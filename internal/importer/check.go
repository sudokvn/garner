package importer

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/trucke/garner/internal/inspect"
	"github.com/trucke/garner/internal/match"
	"github.com/trucke/garner/internal/release"
	"github.com/trucke/garner/internal/selection"
	"github.com/trucke/garner/internal/store"
)

var videoExtensions = map[string]bool{
	".mkv": true, ".mp4": true, ".m4v": true, ".avi": true, ".ts": true, ".m2ts": true, ".webm": true, ".wmv": true,
}

var sampleName = regexp.MustCompile(`(?i)(^|[^a-z])sample([^a-z]|$)`)

// check finds the video files, assigns them to episodes and checks that
// they hold what the release promised.
func (s *Service) check(ctx context.Context, d *store.Download) {
	if d.Storage == "" {
		s.fail(ctx, d, nil, "SABnzbd did not say where it put the files", "Check the job in SABnzbd's history, then retry the import.")
		return
	}
	local, ok := s.opts.SABnzbdPaths.Local(d.Storage)
	if !ok {
		s.fail(ctx, d, nil, fmt.Sprintf("SABnzbd put the files in %s, which no sabnzbd.paths entry covers", d.Storage),
			"Add the folder to sabnzbd.paths in garner.toml, restart garner and retry the import.")
		return
	}
	files, err := videoFiles(local)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		s.fail(ctx, d, nil, fmt.Sprintf("garner cannot see %s", local),
			"Check that the media share is mounted and that sabnzbd.paths points at it, then retry the import.")
		return
	case errors.Is(err, fs.ErrPermission):
		s.fail(ctx, d, nil, fmt.Sprintf("garner may not read %s", local),
			"Give the account garner uses read access to SABnzbd's completed folder, then retry the import.")
		return
	case err != nil:
		s.fail(ctx, d, nil, err.Error(), "Check the folder, then retry the import.")
		return
	case len(files) == 0:
		s.reject(ctx, d, nil, "the download holds no video file")
		return
	}

	c, err := s.context(ctx, d.EpisodeID)
	if err != nil {
		s.fail(ctx, d, nil, err.Error(), "Retry the import; if this persists, refresh the show.")
		return
	}
	if len(files) == 1 {
		files[0].Episodes = c.releaseEpisodes(d)
	} else {
		var matched []store.ImportFile
		for _, f := range files {
			name := strings.TrimSuffix(filepath.Base(f.Source), filepath.Ext(f.Source))
			if m := c.show.Match(release.Parse(name), time.Time{}); m.Kind == match.Episodes {
				f.Episodes = m.Episodes
				matched = append(matched, f)
			} else {
				s.log.Info("leaving a file it cannot assign", "download", d.ID, "file", f.Source, "reason", m.Reason)
			}
		}
		files = matched
	}
	if !slices.ContainsFunc(files, func(f store.ImportFile) bool { return slices.Contains(f.Episodes, d.EpisodeID) }) {
		s.reject(ctx, d, files, "the download does not hold "+c.show.Label(d.EpisodeID))
		return
	}
	files, notes := s.keepBetter(ctx, d, c, files)

	var problems []string
	for i := range files {
		f := &files[i]
		if fh, err := os.Open(f.Source); err != nil {
			s.fail(ctx, d, files, fmt.Sprintf("garner may not read %s: %v", filepath.Base(f.Source), unwrapPath(err)),
				"Give the account garner uses read access to SABnzbd's completed folder, then retry the import.")
			return
		} else {
			fh.Close()
		}
		p, err := s.probe.Probe(ctx, f.Source)
		if errors.Is(err, inspect.ErrUnreadable) {
			problems = append(problems, fmt.Sprintf("%s: %v", filepath.Base(f.Source), err))
			continue
		}
		if err != nil {
			s.fail(ctx, d, files, err.Error(), "Check that ffprobe runs (the ffprobe setting in garner.toml), then retry the import.")
			return
		}
		res := inspect.Check(p, c.expect(d, f.Episodes))
		f.Check = &res
		for _, pr := range res.Problems {
			if len(files) > 1 {
				pr = filepath.Base(f.Source) + ": " + pr
			}
			problems = append(problems, pr)
		}
		f.Target, err = c.target(s.opts.Library, f.Episodes, releaseTags(d.Title, d.Tier, p), filepath.Ext(f.Source))
		if err != nil {
			s.fail(ctx, d, files, err.Error(), "Set library.tv and library.anime in garner.toml, restart garner and retry the import.")
			return
		}
	}
	if len(problems) > 0 {
		s.reject(ctx, d, files, "file check: "+strings.Join(problems, "; "))
		return
	}
	s.save(ctx, d, store.Import{State: store.ImportMoving, Detail: "the file check passed", Files: files, Notes: notes})
	s.move(ctx, d)
}

// keepBetter drops a pack's files for episodes, other than the one it was
// chosen for, whose file came from a release of the same tier or a better
// one: one version per episode, the better one. It returns the files to
// import and a note naming the episodes that kept their file; removeKept
// deletes the dropped files after the move.
func (s *Service) keepBetter(ctx context.Context, d *store.Download, c showContext, files []store.ImportFile) (keep []store.ImportFile, notes []string) {
	if len(files) < 2 {
		return files, nil
	}
	have, err := s.store.SeriesFileTiers(ctx, c.data.Series.ID)
	if err != nil {
		s.log.Warn("could not compare with the files in the library; importing every file", "download", d.ID, "err", err)
		return files, nil
	}
	var kept []int64
	for _, f := range files {
		if !libraryKeeps(c, d, have, f.Episodes) {
			keep = append(keep, f)
			continue
		}
		kept = append(kept, f.Episodes...)
	}
	if len(kept) == 0 {
		return keep, nil
	}
	s.log.Info("kept the existing files", "download", d.ID, "episodes", c.show.Describe(kept))
	return keep, []string{"kept the existing file of " + c.show.Describe(kept) + ": as good as the pack's or better"}
}

// libraryKeeps reports whether the library keeps its own files of a
// pack's episodes: none of them is the episode the download is for, and
// each has a file from a release as good as the pack's or better. See
// keepsGained for the episode the download is for.
func libraryKeeps(c showContext, d *store.Download, have map[int64]string, episodes []int64) bool {
	if len(episodes) == 0 || slices.Contains(episodes, d.EpisodeID) {
		return false
	}
	pack := selection.TierRank(c.profile, d.Tier)
	for _, ep := range episodes {
		if tier, found := have[ep]; !found || selection.TierRank(c.profile, tier) > pack {
			return false
		}
	}
	return true
}

// keepsGained reports whether the library keeps its own files of the
// episodes of a file the import moved, rather than recording the import's,
// when it records them. Next to libraryKeeps, it keeps the file of the
// episode the download is for when that is better than the download's and
// better than the file the download was to replace: one the library gained
// after the download started, as by a take-over or another import. A
// worse release the user chose on purpose still replaces the file it was
// chosen over. A season pack is for the season's missing episodes; the
// episode it is for is only the first one missing when the season was
// searched, so the library keeps its file as it does the others'.
func keepsGained(c showContext, d *store.Download, have map[int64]string, episodes []int64) bool {
	if libraryKeeps(c, d, have, episodes) {
		return true
	}
	if !slices.Contains(episodes, d.EpisodeID) {
		return false
	}
	rank := func(tier string) int { return selection.TierRank(c.profile, tier) }
	for _, ep := range episodes {
		tier, found := have[ep]
		switch {
		case !found:
			return false
		case ep != d.EpisodeID || d.SearchSeason > 0:
			if rank(tier) > rank(d.Tier) {
				return false
			}
		case rank(tier) >= rank(d.Tier) || d.Replaces != "" && rank(tier) >= rank(d.Replaces):
			return false
		}
	}
	return true
}

// releaseEpisodes are the episodes the release name claims, or the wanted
// episode when the name says nothing usable.
func (c showContext) releaseEpisodes(d *store.Download) []int64 {
	if m := c.show.Match(release.Parse(d.Title), time.Time{}); m.Kind == match.Episodes {
		return m.Episodes
	}
	return []int64{d.EpisodeID}
}

// expect states what one file must hold: the tier's language, or any of
// the profile's for a last resort; for a release chosen by hand, a missing
// language is only noted.
func (c showContext) expect(d *store.Download, episodes []int64) inspect.Expect {
	e := inspect.Expect{Strict: !d.Manual}
	if f := strings.Fields(d.Tier); len(f) == 2 {
		if l, ok := release.LanguageNamed(f[0]); ok {
			e.Languages = []release.Language{l}
		}
		if r, err := strconv.Atoi(strings.TrimSuffix(f[1], "p")); err == nil {
			e.Resolution = release.Resolution(r)
		}
	}
	if len(e.Languages) == 0 {
		for _, t := range c.profile.Tiers {
			if !slices.Contains(e.Languages, t.Language) {
				e.Languages = append(e.Languages, t.Language)
			}
		}
	}
	for _, id := range episodes {
		e.Runtime += time.Duration(cmp.Or(c.episodes[id].Runtime, c.data.Series.AverageRuntime)) * time.Minute
	}
	return e
}

// videoFiles lists the video files of a download folder, without samples
// and extras far smaller than the main file.
func videoFiles(dir string) ([]store.ImportFile, error) {
	st, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	var files []store.ImportFile
	// A sample's file name or folder name says "sample".
	add := func(p string, size int64) {
		rel := filepath.Base(p)
		if st.IsDir() {
			rel, _ = filepath.Rel(dir, p)
		}
		if videoExtensions[strings.ToLower(filepath.Ext(p))] && !sampleName.MatchString(rel) {
			files = append(files, store.ImportFile{Source: p, Size: size})
		}
	}
	if !st.IsDir() {
		add(dir, st.Size())
	} else if err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		add(p, info.Size())
		return nil
	}); err != nil {
		return nil, err
	}
	var largest int64
	for _, f := range files {
		largest = max(largest, f.Size)
	}
	files = slices.DeleteFunc(files, func(f store.ImportFile) bool { return f.Size < largest/10 })
	slices.SortFunc(files, func(a, b store.ImportFile) int { return strings.Compare(a.Source, b.Source) })
	return files, nil
}
