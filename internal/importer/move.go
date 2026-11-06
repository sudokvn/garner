package importer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sudokvn/garner/internal/importer/guard"
	"github.com/sudokvn/garner/internal/match"
	"github.com/sudokvn/garner/internal/release"
	"github.com/sudokvn/garner/internal/store"
)

// move moves the files into the library, then asks Jellyfin to scan them.
func (s *Service) move(ctx context.Context, d *store.Download) {
	files, notes, ok := s.moveFiles(ctx, d)
	if !ok {
		return
	}
	if len(files) == 0 {
		s.save(ctx, d, store.Import{State: store.ImportDone, Detail: "the library kept its own files, which are as good or better", Files: files, Notes: notes})
		return
	}
	if s.jellyfin == nil {
		s.save(ctx, d, store.Import{State: store.ImportDone, Detail: "moved into the library; Jellyfin is not configured, so nothing confirmed it", Files: files, Notes: notes})
		return
	}
	var paths []string
	for _, f := range files {
		if p, ok := s.opts.JellyfinPaths.Remote(f.Target); ok {
			paths = append(paths, p)
		}
	}
	if len(paths) < len(files) {
		s.save(ctx, d, store.Import{State: store.ImportUnverified, Detail: "moved into the library, but jellyfin.paths does not cover the library folder",
			Recovery: "Add the library folder to jellyfin.paths in garner.toml, restart garner and retry the import.", Files: files, Notes: notes})
		return
	}
	if err := s.jellyfin.Created(ctx, paths); err != nil {
		s.log.Warn("could not ask Jellyfin to scan; waiting for its own scan", "err", err)
	}
	s.save(ctx, d, store.Import{State: store.ImportScanning, Detail: detailWaiting, Files: files, Notes: notes})
	s.verify(ctx, d)
}

// moveFiles renames the files into the library and records them. A rename
// moves the file on the share without copying it; a rename that would
// cross filesystems fails instead of copying. Then it removes the pack's
// files whose episodes kept the library's own file. It reports false when
// the move failed, or when another change to the library's files runs:
// then the move stays due, and UnlockFiles wakes the loop for it.
//
// Another version of an episode can have the very name a file of the
// import gets, as two releases of one group can. Whether the library keeps
// it is decided before the move, as recordFiles decides it for a file
// elsewhere: a rename onto it would replace it before that.
func (s *Service) moveFiles(ctx context.Context, d *store.Download) ([]store.ImportFile, []string, bool) {
	if !s.files.TryLock() {
		return nil, nil, false
	}
	defer s.files.Unlock()
	files := d.Import.Files
	c, err := s.context(ctx, d.EpisodeID)
	var have map[int64]string
	if err == nil {
		have, err = s.store.SeriesFileTiers(ctx, c.data.Series.ID)
	}
	if err != nil {
		s.fail(ctx, d, files, "could not compare with the library's files: "+err.Error(), "Retry the import.")
		return nil, nil, false
	}
	var moved, left []store.ImportFile
	var kept []int64
	// SABnzbd's folder is looked up only when a file is still in it: after a
	// restart, it may be gone with every file moved already.
	job := sync.OnceValues(func() (guard.Job, error) { return s.guard.Job(d.Storage, d.Title) })
	claim := func(path string) (guard.File, error) {
		j, err := job()
		if err != nil {
			return guard.File{}, err
		}
		return s.guard.InJob(j, path)
	}
	for _, f := range files {
		root := s.rootOf(f.Target)
		if root == "" {
			s.fail(ctx, d, files, f.Target+" is outside the library folders", "Check library.tv and library.anime in garner.toml, then retry the import.")
			return nil, nil, false
		}
		if st, err := os.Stat(root); err != nil || !st.IsDir() {
			s.fail(ctx, d, files, fmt.Sprintf("the library folder %s is missing", root),
				"Mount the media share and check library.tv and library.anime in garner.toml, then retry the import. garner never creates the library folders themselves.")
			return nil, nil, false
		}
		if _, err := os.Lstat(f.Target); err == nil {
			other, err := s.store.FileOfAnother(ctx, f.Target, d.ID)
			if err != nil {
				s.fail(ctx, d, files, "could not check who imported "+f.Target+": "+err.Error(), "Retry the import.")
				return nil, nil, false
			}
			if other && keepsGained(c, d, have, f.Episodes) {
				left = append(left, f)
				kept = append(kept, f.Episodes...)
				continue
			}
		}
		if _, err := os.Stat(f.Source); errors.Is(err, fs.ErrNotExist) {
			// Moved already, before a restart.
			if st, err := os.Stat(f.Target); err == nil && st.Size() == f.Size {
				moved = append(moved, f)
				continue
			}
			s.fail(ctx, d, files, f.Source+" is gone and not in the library", "Check SABnzbd's completed folder; download the episode again if the file was deleted.")
			return nil, nil, false
		}
		if _, err := os.Lstat(f.Target); err == nil {
			ours, err := s.store.FileInUse(ctx, f.Target)
			if err != nil || !ours {
				s.fail(ctx, d, files, f.Target+" already exists and was not imported by garner",
					"Move or delete that file, then retry the import. garner never overwrites files it did not import.")
				return nil, nil, false
			}
		}
		source, err := claim(f.Source)
		if err != nil {
			s.fail(ctx, d, files, fmt.Sprintf("garner may not move %s: %v", filepath.Base(f.Source), err), guardFix)
			return nil, nil, false
		}
		if err := s.guard.Mkdir(filepath.Dir(f.Target)); err != nil {
			fix := permissionFix
			if refused(err) {
				fix = guardFix
			}
			s.fail(ctx, d, files, fmt.Sprintf("cannot create %s: %v", filepath.Dir(f.Target), unwrapPath(err)), fix)
			return nil, nil, false
		}
		if err := s.guard.Move(ctx, source, f.Target); err != nil {
			switch {
			case refused(err):
				s.fail(ctx, d, files, fmt.Sprintf("garner may not move %s: %v", filepath.Base(f.Source), err), guardFix)
			case errors.Is(err, syscall.EXDEV):
				s.fail(ctx, d, files, "the download and the library are on different filesystems, so the file would have to be copied",
					"Keep SABnzbd's completed folder and the library on the same share and dataset, then retry the import. garner moves files; it does not copy them.")
			case errors.Is(err, fs.ErrPermission):
				s.fail(ctx, d, files, fmt.Sprintf("garner may not move %s", filepath.Base(f.Source)), permissionFix)
			default:
				s.fail(ctx, d, files, fmt.Sprintf("moving %s failed: %v", filepath.Base(f.Source), unwrapPath(err)), "Check both folders, then retry the import.")
			}
			return nil, nil, false
		}
		s.log.Info("imported file", "download", d.ID, "target", f.Target)
		moved = append(moved, f)
	}
	recorded, keptNotes, err := s.recordFiles(ctx, d, moved, kept)
	if err != nil {
		s.fail(ctx, d, files, "the files are in the library, but recording them failed: "+err.Error(), "Retry the import.")
		return nil, nil, false
	}
	s.removeKept(ctx, d, job)
	for _, f := range left {
		claimed, err := claim(f.Source)
		if err == nil {
			err = s.guard.Remove(claimed)
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			s.log.Warn("could not remove a file the library did not take", "download", d.ID, "file", f.Source, "err", err)
		}
	}
	files = recorded
	notes := append(append(slices.Clone(d.Import.Notes), keptNotes...), s.tidy(ctx, d, files)...)
	if notes == nil {
		notes = []string{}
	}
	return files, notes, true
}

const permissionFix = "On the NAS, give the account garner uses write access to SABnzbd's completed folder and the library (UID 1000 in this setup), then retry the import."

// guardFix is the recovery step when the guard refused to change a file
// that garner does not own.
const guardFix = "Check sabnzbd.paths, jellyfin.paths, library.tv and library.anime in garner.toml, then retry the import. garner changes only files and folders it owns."

func refused(err error) bool {
	var r *guard.Refusal
	return errors.As(err, &r)
}

// removeKept deletes the videos left in SABnzbd's folder after a move
// whose episodes the library keeps its own files of, as keepBetter decides.
// It works them out from the folder instead of remembering check's list,
// so a retry or a restart after a failed move removes them as well. Videos
// it cannot assign stay for tidy to report.
func (s *Service) removeKept(ctx context.Context, d *store.Download, job func() (guard.Job, error)) {
	j, err := job()
	if err != nil {
		return // gone already, or not garner's to change
	}
	videos, err := videoFiles(j.Dir())
	if err != nil || len(videos) == 0 {
		return
	}
	c, err := s.context(ctx, d.EpisodeID)
	if err != nil {
		return
	}
	have, err := s.store.SeriesFileTiers(ctx, c.data.Series.ID)
	if err != nil {
		return
	}
	for _, f := range videos {
		name := strings.TrimSuffix(filepath.Base(f.Source), filepath.Ext(f.Source))
		m := c.show.Match(release.Parse(name), time.Time{})
		if m.Kind != match.Episodes || !libraryKeeps(c, d, have, m.Episodes) {
			continue
		}
		claimed, err := s.guard.InJob(j, f.Source)
		if err == nil {
			err = s.guard.Remove(claimed)
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			s.log.Warn("could not remove a file it does not import", "download", d.ID, "file", f.Source, "err", err)
		}
	}
}

// recordFiles records each episode's file and removes the version garner
// imported or took over before: one version per episode. A record and the
// plan to remove the version it replaces change in one step, so a crash
// before the removal leaves the plan for finishChanges. A file whose
// episodes the library keeps its own files of, as keepsGained decides, is
// not recorded but removed; the plan may be older than the library's
// files, as after a retry or a restart. It returns the files it recorded
// and a note naming the episodes that kept their file, these and kept,
// those moveFiles left alone.
func (s *Service) recordFiles(ctx context.Context, d *store.Download, files []store.ImportFile, kept []int64) ([]store.ImportFile, []string, error) {
	c, err := s.context(ctx, d.EpisodeID)
	if err != nil {
		return nil, nil, err
	}
	have, err := s.store.SeriesFileTiers(ctx, c.data.Series.ID)
	if err != nil {
		return nil, nil, err
	}
	// The previous versions are claimed while their records still point at
	// them.
	claims := map[string]guard.File{}
	var records []store.EpisodeFile
	var recorded []store.ImportFile
	var dropped []string
	for _, f := range files {
		// A file recorded already is the import's own, as when garner
		// ended after recording it and before saving that it had; one it
		// cannot check is recorded too.
		own, err := s.store.FileInUse(ctx, f.Target)
		if err == nil && !own && keepsGained(c, d, have, f.Episodes) {
			dropped = append(dropped, f.Target)
			kept = append(kept, f.Episodes...)
			continue
		}
		recorded = append(recorded, f)
		for _, ep := range f.Episodes {
			prev, found, err := s.store.EpisodeFile(ctx, ep)
			if err == nil && found && prev.Path != f.Target {
				if c, err := s.guard.Recorded(ctx, prev.Path); err != nil {
					s.log.Warn("left the previous version in place", "path", prev.Path, "err", err)
				} else {
					claims[c.Path()] = c
				}
			}
			records = append(records, store.EpisodeFile{EpisodeID: ep, Path: f.Target, Size: f.Size, Tier: d.Tier})
		}
	}
	planned, err := s.store.RecordFiles(ctx, records, d.ID, s.Now(), append(slices.Sorted(maps.Keys(claims)), dropped...))
	if err != nil {
		return nil, nil, err
	}
	for _, p := range planned {
		claim, ok := claims[p]
		if !ok { // a dropped file of this import, which the plan now covers
			if claim, err = s.guard.Planned(ctx, p); err != nil {
				s.log.Warn("could not claim a file the import does not keep", "path", p, "err", err)
				continue
			}
		}
		if err := s.removeVersion(ctx, claim); err != nil {
			s.log.Warn("could not remove the previous version yet; garner tries again later", "path", p, "err", err)
		}
	}
	if len(kept) == 0 {
		return recorded, nil, nil
	}
	s.log.Info("kept the existing files", "download", d.ID, "episodes", c.show.Describe(kept))
	return recorded, []string{"kept the existing file of " + c.show.Describe(kept) + ": the library gained it after the download started, and it is as good or better"}, nil
}

// removeVersion removes a version of an episode's file that no record uses
// any more, with its subtitle files, and drops the plan to remove it. When
// the removal fails, as in a folder that refuses writes or one that is
// away because the share is not mounted, the plan stays, and
// finishChanges tries again in later rounds without saying so.
func (s *Service) removeVersion(ctx context.Context, c guard.File) error {
	switch err := s.guard.Remove(c); {
	case err == nil:
		s.guard.RemoveSubtitles(c)
		s.log.Info("removed the previous version", "path", c.Path())
	case !errors.Is(err, fs.ErrNotExist):
		return err
	case !dirExists(filepath.Dir(c.Path())):
		return err // not gone: its folder is away
	}
	if err := s.store.DropFileChange(ctx, c.Path()); err != nil {
		s.log.Warn("could not drop the plan to remove a file", "path", c.Path(), "err", err)
	}
	return nil
}

// finishChanges finishes the changes to files that a crash or a failed
// removal left, as their plans in the database tell: a file that moved
// gets its record moved too, one that did not keeps its record, and a
// version an upgrade replaced goes. A change waits while the folder it
// happens in is missing, as when the share is not mounted. It runs only
// while no other change to the library's files runs, so every plan it
// finds is one left that way.
func (s *Service) finishChanges(ctx context.Context) {
	changes, err := s.store.FileChanges(ctx)
	if err != nil {
		s.log.Error("list planned changes to files", "err", err)
		return
	}
	if len(changes) == 0 || !s.files.TryLock() {
		return
	}
	defer s.files.Unlock()
	for _, c := range changes {
		if c.NewPath != "" {
			if !dirExists(filepath.Dir(c.NewPath)) {
				continue
			}
			if _, err := os.Lstat(c.NewPath); err != nil {
				// It did not move, so its record is right.
				if err := s.store.DropFileChange(ctx, c.Path); err != nil {
					s.log.Warn("could not drop the plan to move a file", "path", c.Path, "err", err)
				}
			} else if err := s.store.FinishMove(ctx, c.Path, c.NewPath); err != nil {
				s.log.Warn("could not record an earlier move", "from", c.Path, "to", c.NewPath, "err", err)
			} else {
				s.log.Info("recorded an earlier move", "from", c.Path, "to", c.NewPath)
			}
			continue
		}
		if !dirExists(filepath.Dir(c.Path)) {
			continue
		}
		used, err := s.store.FileInUse(ctx, c.Path)
		if err != nil {
			s.log.Warn("could not check a file planned for removal", "path", c.Path, "err", err)
			continue
		}
		if used {
			// Recorded again meanwhile: it stays.
			if err := s.store.DropFileChange(ctx, c.Path); err != nil {
				s.log.Warn("could not drop the plan to remove a file", "path", c.Path, "err", err)
			}
			continue
		}
		claim, err := s.guard.Planned(ctx, c.Path)
		if err != nil {
			s.log.Warn("could not claim a file planned for removal", "path", c.Path, "err", err)
			continue
		}
		s.removeVersion(ctx, claim) // a failure was told the first time
	}
}

func dirExists(dir string) bool {
	st, err := os.Stat(dir)
	return err == nil && st.IsDir()
}

func (s *Service) rootOf(target string) string {
	for _, root := range []string{s.opts.Library.TV, s.opts.Library.Anime} {
		if root != "" && strings.HasPrefix(target, filepath.Clean(root)+string(filepath.Separator)) {
			return filepath.Clean(root)
		}
	}
	return ""
}
