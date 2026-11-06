package importer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/sudokvn/garner/internal/importer/guard"
	"github.com/sudokvn/garner/internal/store"
)

var (
	// ErrMoving refuses to delete a show while garner moves files of it
	// into the library.
	ErrMoving = errors.New("garner is moving files of this show into the library; try again when that has finished")
	// ErrNoLibrary means the show's files cannot be deleted: garner has no
	// library to import into, so no guard to check they are garner's.
	ErrNoLibrary = errors.New("garner has no library to delete the show's files from")
)

// Deletion is the file side of deleting a show. StartDeletion begins it;
// the caller then cancels the show's running downloads and deletes its
// records, and ends it with Finish, or with Abort when it gives up.
type Deletion struct {
	// Busy are the show's running downloads, for the caller to cancel.
	Busy []store.Download
	// Files counts the show's files, each file once, and Bytes their size.
	Files int
	Bytes int64

	series int64
	// imp is set while the deletion holds the library's files.
	imp *Service
	// claimed are the files to delete; nil when they stay.
	claimed []guard.File
}

// StartDeletion begins deleting a show. With deleteFiles, it holds the
// library's files from reading the show's files until they are gone, so
// no take-over, rename or import changes them, and it fails with ErrBusy
// while another change runs. It fails with ErrMoving while garner moves
// files of the show into the library. It reads the show's files before
// the caller cancels anything, so nothing is cancelled when this fails,
// and claims those it deletes while the records still say they are
// garner's. imp is nil when garner has no library to import into; deleting
// files then fails with ErrNoLibrary when the show has any.
func StartDeletion(ctx context.Context, st *store.Store, imp *Service, seriesID int64, deleteFiles bool) (*Deletion, error) {
	d := &Deletion{series: seriesID}
	if deleteFiles && imp != nil {
		if err := imp.LockFiles(); err != nil {
			return nil, err
		}
		d.imp = imp
	}
	if err := d.read(ctx, st, imp, deleteFiles); err != nil {
		d.Abort()
		return nil, err
	}
	return d, nil
}

// read finds the show's running downloads and files, and claims the files
// to delete.
func (d *Deletion) read(ctx context.Context, st *store.Store, imp *Service, deleteFiles bool) error {
	busy, err := st.BusyDownloads(ctx, d.series)
	if err != nil {
		return err
	}
	for _, b := range busy {
		if !b.Active() {
			return ErrMoving
		}
	}
	d.Busy = busy
	files, err := st.SeriesFiles(ctx, d.series)
	if err != nil {
		return err
	}
	// A file holding several episodes is listed once per episode.
	paths := make([]string, 0, len(files))
	seen := make(map[string]bool, len(files))
	for _, f := range files {
		if !seen[f.Path] {
			seen[f.Path] = true
			paths = append(paths, f.Path)
			d.Bytes += f.Size
		}
	}
	d.Files = len(paths)
	if !deleteFiles || len(paths) == 0 {
		return nil
	}
	if imp == nil {
		return ErrNoLibrary
	}
	d.claimed, err = imp.claimFiles(ctx, paths)
	return err
}

// Finish ends a deletion once the caller deleted the show's records. It
// removes the claimed files, their subtitle files and the folders left
// empty, tells Jellyfin which files went and releases the library's
// files. It returns the files it removed and the problems it met.
func (d *Deletion) Finish(ctx context.Context) (removed, problems []string) {
	defer d.Abort()
	if d.claimed == nil {
		return nil, nil
	}
	removed, problems = d.imp.deleteFiles(ctx, d.series, d.claimed)
	if len(problems) > 0 {
		d.imp.log.Error("delete series files", "series", d.series, "problems", problems)
	}
	d.imp.removed(ctx, removed)
	return removed, problems
}

// Abort ends a deletion and leaves the files alone. After Finish it does
// nothing.
func (d *Deletion) Abort() {
	if d.imp != nil {
		d.imp.UnlockFiles()
		d.imp = nil
	}
}

var seasonFolder = regexp.MustCompile(`^Season\.\d+$`)

// claimFiles claims a show's files while the database still records them,
// so deleteFiles may remove them after the records went.
func (s *Service) claimFiles(ctx context.Context, paths []string) ([]guard.File, error) {
	files := make([]guard.File, 0, len(paths))
	for _, p := range paths {
		f, err := s.guard.Recorded(ctx, p)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

// deleteFiles removes a show's claimed files and their subtitle files,
// then the season and show folders that are left empty. Only folders named
// the way garner names them are removed, so a library root or someone
// else's folder never goes. It returns the files it removed and the
// problems it met.
func (s *Service) deleteFiles(ctx context.Context, seriesID int64, files []guard.File) (removed, problems []string) {
	showSuffix := fmt.Sprintf(".[tvdbid-%d]", seriesID)
	emptied := map[string]guard.File{} // by folder: a file that lay in it
	for _, f := range files {
		if err := s.guard.Remove(f); err != nil && !errors.Is(err, fs.ErrNotExist) {
			problems = append(problems, err.Error())
			continue
		} else if err == nil {
			removed = append(removed, f.Path())
		}
		s.guard.RemoveSubtitles(f)
		emptied[filepath.Dir(f.Path())] = f
	}
	folders := s.guard.Folders(ctx)
	for _, season := range slices.Sorted(maps.Keys(emptied)) {
		if !seasonFolder.MatchString(filepath.Base(season)) {
			continue
		}
		stop := season
		if show := filepath.Dir(season); strings.HasSuffix(filepath.Base(show), showSuffix) {
			// Stays while other seasons are left; it goes with the last.
			stop = show
		}
		folders.RemoveEmpty(emptied[season], stop)
	}
	return removed, problems
}

// removed tells Jellyfin that files are gone. When that fails, Jellyfin
// drops them at its next library scan.
func (s *Service) removed(ctx context.Context, paths []string) {
	if s.jellyfin == nil {
		return
	}
	var remote []string
	for _, p := range paths {
		if r, ok := s.opts.JellyfinPaths.Remote(p); ok {
			remote = append(remote, r)
		}
	}
	if len(remote) == 0 {
		return
	}
	// The files are gone: tell Jellyfin even when the caller stopped waiting.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := s.jellyfin.Deleted(ctx, remote); err != nil {
		s.log.Warn("could not tell Jellyfin about deleted files; it drops them at its next scan", "err", err)
	}
}
