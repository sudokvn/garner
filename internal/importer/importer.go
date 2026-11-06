// Package importer moves finished downloads into the library. It first
// checks that the files hold what the release promised, then moves them on
// the share without copying, and confirms that Jellyfin identified them as
// the intended episodes.
package importer

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/sudokvn/garner/internal/config"
	"github.com/sudokvn/garner/internal/importer/guard"
	"github.com/sudokvn/garner/internal/inspect"
	"github.com/sudokvn/garner/internal/jellyfin"
	"github.com/sudokvn/garner/internal/match"
	"github.com/sudokvn/garner/internal/release"
	"github.com/sudokvn/garner/internal/selection"
	"github.com/sudokvn/garner/internal/store"
)

const (
	pollInterval = 10 * time.Second
	// fullScanAfter is how long garner waits for Jellyfin to act on its
	// hint before asking for a scan of all libraries. Jellyfin delays its
	// own reaction to file changes by 60 seconds.
	fullScanAfter = 90 * time.Second
	// scanTimeout is how long garner waits for Jellyfin to list a moved
	// file without progress before it reports the import as unconfirmed.
	scanTimeout = 3 * time.Minute

	detailWaiting  = "moved into the library; waiting for Jellyfin"
	detailFullScan = "moved into the library; Jellyfin did not pick the files up, so garner asked it to scan all libraries"
)

var ErrNotRetryable = errors.New("only failed or unconfirmed imports can be retried")

// ErrBusy refuses a change to the library's files while another one runs.
var ErrBusy = errors.New("garner is changing files in the library; try again in a moment")

// Prober reads what a file holds.
type Prober interface {
	Probe(ctx context.Context, file string) (inspect.Probe, error)
}

type Options struct {
	// SABnzbdPaths translates SABnzbd's folders to garner's view.
	SABnzbdPaths config.PathMap
	// JellyfinPaths translates Jellyfin's folders to garner's view.
	JellyfinPaths config.PathMap
	Library       config.Library
	// Rules returns a show's reviewed streaming-service order rules.
	Rules func(seriesID int64) map[release.Service]string
	// Profile applies a show's stored profile to the show.
	Profile func(store.Series) selection.Profile
	// Rename moves files for the guard; nil means os.Rename.
	Rename func(oldpath, newpath string) error
	// Before runs before each change to a file or folder; see
	// guard.Options.Before.
	Before func(op, path string)
}

// Service imports completed downloads.
type Service struct {
	store    *store.Store
	probe    Prober
	jellyfin *jellyfin.Client // nil: imports are not confirmed
	opts     Options
	// guard makes every change to files and folders.
	guard *guard.Guard
	log   *slog.Logger
	// Now tells the time: time.Now, or the simulation's clock.
	Now  func() time.Time
	wake chan struct{}
	// files lets one change to the library's files and their records run at
	// a time: a take-over, a rename, an import's move or deleting a show's
	// files. Each would otherwise act on records another one is changing.
	// Nothing waits for it: a user's request is refused with ErrBusy, and
	// the import loop leaves its move for the next round.
	files    sync.Mutex
	OnChange func(store.Download)
	// OnRejected marks a download's release bad, so another one is tried.
	OnRejected func(ctx context.Context, d store.Download, reason string)
}

// New returns an importer. jf may be nil.
func New(st *store.Store, probe Prober, jf *jellyfin.Client, opts Options, log *slog.Logger) *Service {
	if opts.Rules == nil {
		opts.Rules = func(int64) map[release.Service]string { return nil }
	}
	g := guard.New(guard.Options{
		Library: opts.Library, SABnzbdPaths: opts.SABnzbdPaths,
		Recorded: st.FileInUse, Planned: st.PlannedRemoval, LibraryFolders: jellyfinFolders(jf, opts.JellyfinPaths),
		Rename: opts.Rename, Before: opts.Before,
	})
	return &Service{store: st, probe: probe, jellyfin: jf, opts: opts, guard: g, log: log, Now: time.Now, wake: make(chan struct{}, 1)}
}

// jellyfinFolders lists Jellyfin's library folders as garner reaches them;
// nil without Jellyfin. A folder no jellyfin.paths entry covers is out of
// garner's reach, so it is left out.
func jellyfinFolders(jf *jellyfin.Client, paths config.PathMap) func(context.Context) ([]string, error) {
	if jf == nil {
		return nil
	}
	return func(ctx context.Context) ([]string, error) {
		remote, err := jf.LibraryFolders(ctx)
		if err != nil {
			return nil, err
		}
		var local []string
		for _, r := range remote {
			if l, ok := paths.Local(r); ok {
				local = append(local, l)
			}
		}
		return local, nil
	}
}

// LockFiles reserves the library's files for one change, such as deleting
// a show's files; it fails with ErrBusy while another change runs.
// UnlockFiles ends the change.
func (s *Service) LockFiles() error {
	if !s.files.TryLock() {
		return ErrBusy
	}
	return nil
}

// UnlockFiles ends a change LockFiles began and wakes the import loop for
// a move it left meanwhile.
func (s *Service) UnlockFiles() {
	s.files.Unlock()
	s.Wake()
}

// Wake makes Run look for work now, such as after a download completed.
func (s *Service) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Run imports completed downloads until ctx is cancelled.
func (s *Service) Run(ctx context.Context) {
	for {
		s.Work(ctx)
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-time.After(pollInterval):
		}
	}
}

func (s *Service) Work(ctx context.Context) {
	s.finishChanges(ctx)
	due, err := s.store.DueImports(ctx)
	if err != nil {
		s.log.Error("list due imports", "err", err)
		return
	}
	for _, d := range due {
		if ctx.Err() != nil {
			return
		}
		switch d.Import.State {
		case store.ImportPending:
			s.check(ctx, &d)
		case store.ImportMoving:
			s.move(ctx, &d)
		case store.ImportScanning:
			s.verify(ctx, &d)
		}
	}
}

// Retry repeats the step that failed, after the problem was fixed.
func (s *Service) Retry(ctx context.Context, downloadID int64) (store.Download, error) {
	d, err := s.store.GetDownload(ctx, downloadID)
	if err != nil {
		return d, err
	}
	im := d.Import
	switch {
	case im.State == store.ImportFailed && im.Planned():
		im.State = store.ImportMoving
	case im.State == store.ImportFailed:
		im.State = store.ImportPending
	case im.State == store.ImportUnverified:
		im.State = store.ImportScanning
	default:
		return d, ErrNotRetryable
	}
	im.Detail, im.Recovery = "retrying", ""
	if im.State == store.ImportScanning {
		im.Detail = detailWaiting
	}
	if err := s.save(ctx, &d, im); err != nil {
		return d, err
	}
	s.Wake()
	return d, nil
}

// save stores an import's progress, keeping earlier notes unless im brings
// its own. Callers in the loop may ignore the error: it is logged, and the
// step runs again on the next round.
func (s *Service) save(ctx context.Context, d *store.Download, im store.Import) error {
	if im.Notes == nil {
		im.Notes = d.Import.Notes
	}
	if err := s.store.UpdateImport(ctx, d.ID, im, s.Now()); err != nil {
		s.log.Error("save import", "download", d.ID, "err", err)
		return err
	}
	im.Updated = s.Now()
	d.Import = im
	s.log.Info("import", "download", d.ID, "state", im.State, "detail", im.Detail)
	if s.OnChange != nil {
		s.OnChange(*d)
	}
	return nil
}

func (s *Service) fail(ctx context.Context, d *store.Download, files []store.ImportFile, detail, recovery string) {
	s.save(ctx, d, store.Import{State: store.ImportFailed, Detail: detail, Recovery: recovery, Files: files})
}

// reject marks the release bad, removes its files from SABnzbd's folder and
// lets the download service try the next release.
func (s *Service) reject(ctx context.Context, d *store.Download, files []store.ImportFile, reason string) {
	s.save(ctx, d, store.Import{State: store.ImportRejected, Detail: reason, Files: files})
	s.discard(ctx, d)
	if s.OnRejected != nil {
		s.OnRejected(ctx, *d, reason)
	}
}

// showContext is a show prepared for one import.
type showContext struct {
	data     store.SeriesData
	show     *match.Show
	profile  selection.Profile
	episodes map[int64]store.Episode
}

func (s *Service) context(ctx context.Context, episodeID int64) (showContext, error) {
	seriesID, err := s.store.SeriesOfEpisode(ctx, episodeID)
	if err != nil {
		return showContext{}, err
	}
	return s.seriesContext(ctx, seriesID)
}

func (s *Service) seriesContext(ctx context.Context, seriesID int64) (showContext, error) {
	data, err := s.store.LoadSeries(ctx, seriesID)
	if err != nil {
		return showContext{}, err
	}
	c := showContext{
		data: data, profile: s.opts.Profile(data.Series), episodes: map[int64]store.Episode{},
		show: match.NewShow(data, match.Options{Anime: data.Series.Type == store.TypeAnime, ServiceOrders: s.opts.Rules(seriesID)}),
	}
	for _, e := range data.Episodes {
		c.episodes[e.ID] = e
	}
	return c, nil
}

// unwrapPath drops the path from an *fs.PathError; the message names it.
func unwrapPath(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Err
	}
	return err
}
