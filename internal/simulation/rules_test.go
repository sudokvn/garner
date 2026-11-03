package simulation

import (
	"cmp"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/trucke/garner/internal/api"
	"github.com/trucke/garner/internal/selection"
	"github.com/trucke/garner/internal/store"
	"github.com/trucke/garner/internal/testprofiles"
)

// The rules S1 to S6 from the package comment, and the snapshots they
// compare before and after every step.

// snapshot is what the rules compare before and after a step.
type snapshot struct {
	downloads map[int64]store.Download
	records   map[int64]string  // the recorded file, by episode
	tiers     map[int64]string  // the recorded file's tier, by episode
	library   map[string]string // where each video in the libraries is, by video
	dupes     []string
	// planned are the files garner planned to remove, as the rest of an
	// upgrade that replaced them.
	planned map[string]bool
	jobs    map[string]bool // SABnzbd's job folders that exist, by job
}

// libraries are the folders whose videos the rules follow: garner's and
// Jellyfin's libraries, wherever they are plugged in.
var libraries = []string{"library", "media-old", "unplugged"}

func (s *sim) snapshot() (snapshot, error) {
	sn := snapshot{downloads: map[int64]store.Download{}, records: map[int64]string{}, tiers: map[int64]string{}, library: map[string]string{}, jobs: map[string]bool{},
		planned: map[string]bool{}}
	changes, err := s.st.FileChanges(s.ctx)
	if err != nil {
		return sn, err
	}
	for _, c := range changes {
		if c.NewPath == "" {
			sn.planned[c.Path] = true
		}
	}
	for _, j := range s.w.sab.jobs {
		if _, err := os.Stat(j.dir); j.dir != "" && !j.lost && err == nil {
			sn.jobs[j.id] = true
		}
	}
	all, err := s.st.Downloads(s.ctx, 100000)
	if err != nil {
		return sn, err
	}
	for _, d := range all {
		sn.downloads[d.ID] = d
	}
	for _, sh := range s.w.shows {
		for _, e := range sh.episodes {
			f, found, err := s.st.EpisodeFile(s.ctx, e.id)
			if err != nil {
				return sn, err
			}
			if found {
				sn.records[e.id], sn.tiers[e.id] = f.Path, f.Tier
			}
		}
	}
	for _, top := range libraries {
		err = filepath.WalkDir(filepath.Join(s.w.root, top), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if v, ok := s.video(p, d); ok {
				p = s.w.plugged(p)
				if other, dup := sn.library[v.ID]; dup {
					sn.dupes = append(sn.dupes, fmt.Sprintf("%s at %s and %s", v.ID, other, p))
				}
				sn.library[v.ID] = p
			}
			return nil
		})
		if err != nil {
			return sn, err
		}
	}
	return sn, nil
}

// plugged is where a file under the unplugged folder belongs.
func (w *world) plugged(p string) string {
	if rel, ok := strings.CutPrefix(w.rel(p), "unplugged/"); ok {
		return filepath.Join(w.root, rel)
	}
	return p
}

// digest sums a folder's names and contents.
func (s *sim) digest(dir string) string {
	h := sha256.New()
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		fmt.Fprintln(h, s.w.rel(p), err)
		if err == nil && !d.IsDir() {
			data, _ := os.ReadFile(p)
			h.Write(data)
		}
		return nil
	})
	return fmt.Sprintf("%x", h.Sum(nil))
}

// foreignStays is S1: garner never moves, renames or deletes a file no one
// gave it.
func (s *sim) foreignStays(_, _ snapshot) (string, string) {
	for _, p := range sortedKeys(s.w.foreign) {
		f := s.w.foreign[p]
		info, err := os.Stat(s.w.present(p))
		if err == nil && info.Size() == f.size && info.ModTime().Equal(f.mod) {
			continue
		}
		data, err := os.ReadFile(s.w.present(p))
		if err != nil || string(data) != f.content {
			return "S1", fmt.Sprintf("%s changed or went: %v", p, err)
		}
	}
	return "", ""
}

// filesStay is S2 and S3: a video leaves the libraries only when a newer
// file of its episodes replaced it, or when the user deleted its show with
// its files; garner copies nothing and overwrites nothing else.
func (s *sim) filesStay(before, after snapshot) (string, string) {
	if len(after.dupes) > 0 {
		return "S3", "a video is in the library twice: " + after.dupes[0]
	}
	for _, id := range sortedKeys(before.library) {
		if _, ok := after.library[id]; ok {
			continue
		}
		path := before.library[id]
		var episodes []int64
		for _, e := range sortedKeys64(before.records) {
			if before.records[e] == path {
				episodes = append(episodes, e)
			}
		}
		if len(episodes) == 0 && (before.planned[path] || inFlight(before, path)) {
			continue // the rest of an upgrade that replaced it, or an import's own file
		}
		if len(episodes) == 0 {
			return "S2", fmt.Sprintf("video %s at %s left the library, and no episode recorded it", id, path)
		}
		if ep := s.w.episode(episodes[0]); ep != nil && s.deleting[ep.show.id] {
			continue
		}
		for _, e := range episodes {
			p, ok := after.records[e]
			// Deleting a show with its files kept drops its records, so a
			// replacement in the same step shows only on disk: a video of
			// the episode that arrived in the step. Its tier went with the
			// record, so quality is not compared here.
			if ep := s.w.episode(e); !ok && ep != nil && s.deleted[ep.show.id] {
				if !s.arrived(before, after, e) {
					return "S2", fmt.Sprintf("video %s of episode %d left the library from %s while its show was deleted with its files kept, and no new video of the episode replaced it", id, e, path)
				}
				continue
			}
			if v, read := readVideo(s.w.present(p)); !ok || !read || v.ID == id {
				return "S2", fmt.Sprintf("video %s of episode %d left the library from %s, and no other file replaced it", id, e, path)
			}
			if msg := s.worse(before, after, e); msg != "" {
				return "S2", msg
			}
		}
	}
	return "", ""
}

// arrived reports whether a video of episode e came into the libraries in
// the step. One that was there before, such as the user's own copy, does
// not replace anything.
func (s *sim) arrived(before, after snapshot, e int64) bool {
	for _, id := range sortedKeys(after.library) {
		if _, old := before.library[id]; old {
			continue
		}
		if v, ok := readVideo(s.w.present(after.library[id])); ok && slices.Contains(v.Episodes, e) {
			return true
		}
	}
	return false
}

// inFlight reports whether path is where an import that has not ended
// moves a file: until it records the file, it is the import's to keep or
// remove.
func inFlight(sn snapshot, path string) bool {
	for _, id := range sortedKeys64(sn.downloads) {
		d := sn.downloads[id]
		if d.Import.State != store.ImportMoving && d.Import.State != store.ImportFailed {
			continue
		}
		if slices.ContainsFunc(d.Import.Files, func(f store.ImportFile) bool { return f.Target == path }) {
			return true
		}
	}
	return false
}

// worse says how episode e's file was replaced by one of a worse tier,
// unless the user chose a release over that file, knowing it, and its
// import moved its file in this step: a download of the episode alone
// that started while the file, or one as good, was there. Other imports
// in the step may have replaced that release's file again, with a better
// one. A season pack is for the season's missing episodes, never chosen
// over a file.
func (s *sim) worse(before, after snapshot, e int64) string {
	ep := s.w.episode(e)
	if ep == nil {
		return ""
	}
	profile := selection.FromProfile(testprofiles.German, "")
	if ep.show.profile == store.TypeAnime {
		profile = selection.FromProfile(testprofiles.GermanEnglish, "")
	}
	rank := func(tier string) int { return selection.TierRank(profile, tier) }
	old, now := before.tiers[e], after.tiers[e]
	if rank(now) <= rank(old) {
		return ""
	}
	moved := []api.ImportState{store.ImportScanning, store.ImportDone, store.ImportUnverified}
	for _, id := range sortedKeys64(after.downloads) {
		d := after.downloads[id]
		if d.EpisodeID != e || d.SearchSeason > 0 || d.Replaces == "" || rank(old) < rank(d.Replaces) {
			continue
		}
		b, ok := before.downloads[id]
		if slices.Contains(moved, d.Import.State) && !(ok && sameDownload(b, d) && slices.Contains(moved, b.Import.State)) {
			return ""
		}
	}
	return fmt.Sprintf("episode %d's file %s (%s) was replaced by a worse one, %s (%s)", e, before.records[e], cmp.Or(old, "tier unknown"), after.records[e], cmp.Or(now, "tier unknown"))
}

// recordsMatch is S4: every recorded file exists.
func (s *sim) recordsMatch(_, after snapshot) (string, string) {
	for _, e := range sortedKeys64(after.records) {
		if _, err := os.Stat(s.w.present(after.records[e])); err != nil {
			return "S4", fmt.Sprintf("episode %d records %s, which is gone", e, after.records[e])
		}
	}
	return "", ""
}

// The share's top folders; nothing comes or goes there.
var top = []string{"elsewhere", "garner", "library", "media-old", "unplugged", "usenet"}

// outsideStays is S5: nothing outside the libraries, SABnzbd's folders and
// garner's data changes, the library roots stay, and an unplugged root
// stays away until it is plugged in again.
func (s *sim) outsideStays(before, after snapshot) (string, string) {
	if s.digest(filepath.Join(s.w.root, "elsewhere")) != s.sentinel {
		return "S5", "the folder elsewhere changed"
	}
	entries, _ := os.ReadDir(s.w.root)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, top) {
		return "S5", fmt.Sprintf("the share's top holds %v", names)
	}
	for _, root := range roots {
		_, err := os.Stat(filepath.Join(s.w.root, root))
		if exists := err == nil; exists != (root != s.w.unplugged) {
			return "S5", fmt.Sprintf("library root %s exists %v while unplugged is %q", root, exists, s.w.unplugged)
		}
	}
	// A job folder goes when someone removes the job in SABnzbd by hand,
	// when garner is done with its download (imported or rejected), when
	// garner cancelled the download and SABnzbd deleted its files, or with
	// its show or episode.
	done := []api.ImportState{store.ImportScanning, store.ImportDone, store.ImportUnverified, store.ImportRejected}
	for _, j := range s.w.sab.jobs {
		if !before.jobs[j.id] || after.jobs[j.id] {
			continue
		}
		j.lost = true
		d, ok := downloadOfJob(after, j.id)
		switch {
		case j.vanished:
		case !ok && (!j.posting.show.added || s.deleted[j.posting.show.id] || len(s.removed) > 0):
		case ok && j.gone && d.State == store.Cancelled:
		case ok && !j.gone && slices.Contains(done, d.Import.State):
		default:
			return "S5", fmt.Sprintf("SABnzbd's folder %s of %s went, but garner was not done with its download (%s, import %q)", j.dir, j.id, d.State, d.Import.State)
		}
	}
	return "", ""
}

func downloadOfJob(sn snapshot, jobID string) (store.Download, bool) {
	for _, id := range sortedKeys64(sn.downloads) {
		if d := sn.downloads[id]; d.JobID == jobID {
			return d, true
		}
	}
	return store.Download{}, false
}

// The transitions downloads and imports may take in a step, step by step.
var (
	downloadNext = map[api.DownloadState][]api.DownloadState{
		store.Submitting:  {store.Queued, store.Downloading, store.Processing, store.Completed, store.Failed, store.Cancelled},
		store.Queued:      {store.Downloading, store.Processing, store.Completed, store.Failed, store.Cancelled},
		store.Downloading: {store.Queued, store.Processing, store.Completed, store.Failed, store.Cancelled},
		store.Processing:  {store.Completed, store.Failed, store.Cancelled},
		store.Completed:   {store.Failed}, // the file check rejected the release
	}
	importNext = map[api.ImportState][]api.ImportState{
		"":                   {store.ImportPending},
		store.ImportPending:  {store.ImportMoving, store.ImportRejected, store.ImportFailed},
		store.ImportMoving:   {store.ImportScanning, store.ImportDone, store.ImportUnverified, store.ImportFailed},
		store.ImportScanning: {store.ImportDone, store.ImportUnverified},
	}
	// retryNext are the transitions only Retry makes.
	retryNext = map[api.ImportState][]api.ImportState{
		store.ImportFailed:     {store.ImportPending, store.ImportMoving},
		store.ImportUnverified: {store.ImportScanning},
	}
)

// statesMove is S6: downloads and imports change state only along the
// allowed transitions, and only the downloads of a deleted show or a
// dropped episode disappear.
func (s *sim) statesMove(before, after snapshot) (string, string) {
	for _, id := range sortedKeys64(before.downloads) {
		b := before.downloads[id]
		a, ok := after.downloads[id]
		ok = ok && sameDownload(a, b)
		if !ok {
			e := s.w.episode(b.EpisodeID)
			if !s.removed[b.EpisodeID] && e != nil && e.show.added && !s.deleted[e.show.id] {
				return "S6", fmt.Sprintf("download %d disappeared, and its show is still there", id)
			}
			continue
		}
		if !reachable(downloadNext, b.State, a.State) {
			return "S6", fmt.Sprintf("download %d went from %s to %s", id, b.State, a.State)
		}
		graph := importNext
		if s.retrying[id] {
			graph = merge(importNext, retryNext)
		}
		if !reachable(graph, b.Import.State, a.Import.State) {
			return "S6", fmt.Sprintf("the import of download %d went from %q to %q", id, b.Import.State, a.Import.State)
		}
	}
	// Every download starts as submitting; steps inside this one may have
	// moved it on already.
	for _, id := range sortedKeys64(after.downloads) {
		a := after.downloads[id]
		if b, ok := before.downloads[id]; (!ok || !sameDownload(a, b)) && !reachable(downloadNext, store.Submitting, a.State) {
			return "S6", fmt.Sprintf("download %d started as %s", id, a.State)
		}
	}
	return "", ""
}

// sameDownload tells a download from a later one that got its number: the
// database reuses the highest number after a show's downloads went.
func sameDownload(a, b store.Download) bool {
	return a.Created.Equal(b.Created) && a.EpisodeID == b.EpisodeID && a.Title == b.Title
}

func reachable[S comparable](graph map[S][]S, from, to S) bool {
	seen := map[S]bool{from: true}
	for queue := []S{from}; len(queue) > 0; queue = queue[1:] {
		if queue[0] == to {
			return true
		}
		for _, next := range graph[queue[0]] {
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	return false
}

func merge[S comparable](a, b map[S][]S) map[S][]S {
	out := map[S][]S{}
	for k, v := range a {
		out[k] = slices.Clone(v)
	}
	for k, v := range b {
		out[k] = append(out[k], v...)
	}
	return out
}
