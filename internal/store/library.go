package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/trucke/garner/internal/api"
	"github.com/trucke/garner/internal/store/query"
)

var urgency = map[api.EpisodeState]int{
	EpisodeFailed: 0, EpisodeAttention: 1, EpisodeDownloading: 2, EpisodeProcessing: 3, EpisodeImporting: 4,
	EpisodeWaiting: 5, EpisodeNotSearched: 5, EpisodeUpcoming: 6, EpisodeImported: 7,
}

type searchOutcome struct {
	searched time.Time
	outcome  api.SearchOutcome
	summary  string
}

// Library summarises every show in the catalogue as of now. sc is the
// monitor's schedule, nil when the monitor does not run; missing episodes
// it gets carry its next search.
func (s *Store) Library(ctx context.Context, now time.Time, sc *Schedule) ([]Show, error) {
	shows, _, err := s.summarise(ctx, now, 0, sc)
	return shows, err
}

// ShowStatus summarises one show and lists what garner has of each of its
// episodes, as Library does.
func (s *Store) ShowStatus(ctx context.Context, seriesID int64, now time.Time, sc *Schedule) (Show, []EpisodeStatus, error) {
	shows, states, err := s.summarise(ctx, now, seriesID, sc)
	if err != nil {
		return Show{}, nil, err
	}
	if len(shows) == 0 {
		return Show{}, nil, fmt.Errorf("series %d: %w", seriesID, ErrNotFound)
	}
	return shows[0], states, nil
}

// summarise works out every episode's state. With only set, it covers that
// show alone and also returns the episodes' states.
func (s *Store) summarise(ctx context.Context, now time.Time, only int64, sc *Schedule) ([]Show, []EpisodeStatus, error) {
	f, err := s.loadFacts(ctx, only)
	if err != nil {
		return nil, nil, err
	}
	today := now.Format(time.DateOnly)
	shows := make([]Show, len(f.series))
	index := make(map[int64]int, len(f.series))
	paths := make([]map[string]bool, len(f.series))
	for i, sr := range f.series {
		shows[i] = Show{Series: sr, Counts: map[api.EpisodeState]int{}}
		index[sr.ID] = i
		paths[i] = map[string]bool{}
	}
	var states []EpisodeStatus
	for _, e := range f.episodes {
		i, ok := index[e.SeriesID]
		if !ok {
			continue
		}
		sh := &shows[i]
		aired := e.Aired.Valid && e.Aired.String != "" && e.Aired.String <= today
		regular := e.Season.Valid && e.Season.Int64 > 0
		file, hasFile := f.files[e.ID]
		if hasFile && !paths[i][file.Path] {
			paths[i][file.Path] = true
			sh.Bytes += file.Size
		}
		if regular && aired {
			sh.Aired++
			if hasFile {
				sh.Imported++
			}
		}
		if regular && !aired && e.Aired.Valid && e.Aired.String != "" && (sh.NextAired == "" || e.Aired.String < sh.NextAired) {
			sh.NextAired = e.Aired.String
		}

		d := f.downloads[e.ID]
		search, searched := f.searches[e.ID]
		state := episodeState(hasFile, d, search, searched, aired)
		var next *time.Time
		if sc != nil && regular && wants(state, d) && sh.Monitors(e.Aired.String) {
			own, ran := f.monitored[e.ID]
			t := sc.next(false, e.Aired.String, own, ran, d)
			if t.IsZero() {
				t = now // due: the monitor gets to it soon
			}
			next = &t
		}
		// Acknowledgment changes only what needs the user's attention, not
		// the state used above (or by wanted) to schedule the monitor.
		cleared := state == EpisodeFailed && d != nil && !d.Acknowledged.IsZero()
		if cleared {
			state = episodeState(hasFile, nil, search, searched, aired)
		}
		if !regular && (state == EpisodeUpcoming || state == EpisodeNotSearched) {
			// Specials nobody asked for are not missing.
			state = ""
		}
		if only > 0 {
			st := EpisodeStatus{EpisodeID: e.ID, State: state, Attempts: f.attempts[e.ID], NextRetry: next}
			if hasFile {
				st.Size, st.Tier = file.Size, file.Tier
			}
			if cleared || explains(state, d) {
				st.Download = d
			}
			if searched && !cleared {
				st.Summary = search.summary
			}
			states = append(states, st)
		}
		if state == "" || state == EpisodeImported {
			continue
		}
		if state != EpisodeUpcoming {
			sh.Counts[state]++
		}
		focus := &Focus{EpisodeID: e.ID, Season: int(e.Season.Int64), Number: int(e.Number.Int64), State: state, Aired: e.Aired.String, NextRetry: next}
		if cleared || explains(state, d) {
			focus.Download = d
		}
		if searched && !cleared {
			focus.Summary = search.summary
		}
		if moreUrgent(focus, sh.Focus) {
			sh.Focus = focus
		}
	}
	return shows, states, nil
}

// explains reports whether an episode's latest download explains its
// state; one that waits for the user, cancelled or with a failed import,
// explains why the monitor leaves a missing episode alone.
func explains(state api.EpisodeState, d *Download) bool {
	switch state {
	case EpisodeDownloading, EpisodeProcessing, EpisodeImporting, EpisodeFailed, EpisodeAttention:
		return true
	case EpisodeWaiting, EpisodeNotSearched:
		return leftToYou(d)
	}
	return false
}

// episodeState decides an episode's state from its file, latest download
// and latest search.
func episodeState(hasFile bool, d *Download, s searchOutcome, searched, aired bool) api.EpisodeState {
	var own api.EpisodeState
	if d != nil {
		own = d.EpisodeState()
	}
	switch own {
	case EpisodeProcessing, EpisodeDownloading, EpisodeImporting, EpisodeAttention:
		return own
	}
	if hasFile {
		return EpisodeImported
	}
	if own == EpisodeFailed && (!searched || !s.searched.After(d.Updated)) {
		return EpisodeFailed
	}
	switch {
	case !aired:
		return EpisodeUpcoming
	case searched:
		return EpisodeWaiting
	}
	return EpisodeNotSearched
}

// moreUrgent orders episodes by state, then by position.
func moreUrgent(a, b *Focus) bool {
	if b == nil {
		return true
	}
	if urgency[a.State] != urgency[b.State] {
		return urgency[a.State] < urgency[b.State]
	}
	if a.State == EpisodeUpcoming && a.Aired != b.Aired {
		// Dated episodes come before undated ones.
		return b.Aired == "" || a.Aired != "" && a.Aired < b.Aired
	}
	if a.Season != b.Season {
		return a.Season < b.Season
	}
	return a.Number < b.Number
}

// SeriesFiles lists the files garner imported for a show: the paths as
// garner reaches them and their sizes.
func (s *Store) SeriesFiles(ctx context.Context, seriesID int64) ([]EpisodeFile, error) {
	rows, err := s.q.ListSeriesFiles(ctx, seriesID)
	if err != nil {
		return nil, err
	}
	out := make([]EpisodeFile, len(rows))
	for i, r := range rows {
		out[i] = EpisodeFile{Path: r.Path, Size: r.Size}
	}
	return out, nil
}

// SeriesFileTiers maps the show's episodes that have a file to the file's
// tier, such as "German 1080p"; empty when unknown.
func (s *Store) SeriesFileTiers(ctx context.Context, seriesID int64) (map[int64]string, error) {
	rows, err := s.q.ListSeriesFileTiers(ctx, seriesID)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]string, len(rows))
	for _, r := range rows {
		out[r.EpisodeID] = r.Tier
	}
	return out, nil
}

// BusyDownloads lists a show's downloads that are running or whose files
// are being moved into the library.
func (s *Store) BusyDownloads(ctx context.Context, seriesID int64) ([]Download, error) {
	rows, err := s.q.ListSeriesBusyDownloads(ctx, seriesID)
	if err != nil {
		return nil, err
	}
	return downloadsFromRows(rows)
}

// ErrBusy means a show has a running download or an import that moves
// files, so it cannot be deleted now.
var ErrBusy = errors.New("a download or import of this show is running")

// DeleteSeries removes a show with its episodes, searches, downloads and
// the record of its files, unless a download or import of it is running.
// The check and the removal are one transaction, so nothing starts in
// between. The files themselves are the caller's.
func (s *Store) DeleteSeries(ctx context.Context, seriesID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := s.q.WithTx(tx)
	busy, err := q.CountSeriesBusyDownloads(ctx, seriesID)
	if err != nil {
		return err
	}
	if busy > 0 {
		return ErrBusy
	}
	n, err := q.DeleteSeries(ctx, seriesID)
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("series %d: %w", seriesID, ErrNotFound)
	}
	return tx.Commit()
}

// SeriesChange is what ChangeSeries changes of a show; empty fields stay.
type SeriesChange struct {
	ProfileID int64
	Type      string // TypeTV or TypeAnime
	Monitor   string // MonitorNew, MonitorAll or MonitorOff
}

// ChangeSeries changes a show's profile, type and monitor mode in one
// transaction, so either all of them change or none. A changed profile
// drops the show's search results: they were judged by the old rules, and
// the next search judges by the new ones. So does a changed type: they
// were matched with the old numbering. Episodes with a running download
// keep theirs: it may need a replacement from them. Files in the library
// stay where they are; new ones go to the type's folder. A monitor mode
// chosen anew counts from now.
func (s *Store) ChangeSeries(ctx context.Context, seriesID int64, c SeriesChange, now time.Time) error {
	if c.Type != "" && c.Type != TypeTV && c.Type != TypeAnime {
		return fmt.Errorf("type %q is neither %s nor %s", c.Type, TypeTV, TypeAnime)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := s.q.WithTx(tx)
	row, err := q.GetSeries(ctx, seriesID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("series %d: %w", seriesID, ErrNotFound)
	}
	if err != nil {
		return err
	}
	rejudge := false
	if c.ProfileID != 0 && row.ProfileID.Int64 != c.ProfileID {
		if _, err := q.GetProfile(ctx, c.ProfileID); errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("profile %d: %w", c.ProfileID, ErrNoProfile)
		} else if err != nil {
			return err
		}
		if _, err := q.SetSeriesProfile(ctx, query.SetSeriesProfileParams{ProfileID: sql.NullInt64{Int64: c.ProfileID, Valid: true}, ID: seriesID}); err != nil {
			return err
		}
		rejudge = true
	}
	if c.Type != "" && row.Type.String != c.Type {
		if _, err := q.SetSeriesType(ctx, query.SetSeriesTypeParams{Type: nullString(c.Type), ID: seriesID}); err != nil {
			return err
		}
		rejudge = true
	}
	if c.Monitor != "" {
		if _, err := q.SetSeriesMonitor(ctx, query.SetSeriesMonitorParams{Monitor: c.Monitor, Since: now.Unix(), ID: seriesID}); err != nil {
			return err
		}
	}
	if rejudge {
		if err := clearJudgements(ctx, q, seriesID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// clearJudgements drops a show's search results, so the next search judges
// releases by its current type and profile, and the monitor searches it
// anew.
func clearJudgements(ctx context.Context, q *query.Queries, seriesID int64) error {
	if err := q.ClearSeriesSearches(ctx, seriesID); err != nil {
		return err
	}
	if err := q.ClearSeriesSeasonSearches(ctx, seriesID); err != nil {
		return err
	}
	return q.ClearSeriesMonitorSearches(ctx, seriesID)
}
