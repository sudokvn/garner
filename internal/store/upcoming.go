package store

import (
	"cmp"
	"context"
	"database/sql"
	"slices"
	"time"

	"github.com/sudokvn/garner/internal/api"
	"github.com/sudokvn/garner/internal/store/query"
)

// The days Upcoming covers: aired episodes that are missing from this many
// days back, and every episode up to this many days ahead.
const (
	upcomingPast  = 2
	upcomingAhead = 14
)

// Upcoming lists the regular episodes of every show that aired from two
// days before now's date to that date and are not in the library, or air
// in the fourteen days after it, by air date. sc is the monitor's schedule,
// nil when the monitor does not run; the episodes it gets carry when it
// searches them next.
func (s *Store) Upcoming(ctx context.Context, now time.Time, sc *Schedule) ([]api.UpcomingEpisode, error) {
	today := now.Format(time.DateOnly)
	rows, err := s.q.ListEpisodesAiring(ctx, query.ListEpisodesAiringParams{
		First: sql.NullString{String: now.AddDate(0, 0, -upcomingPast).Format(time.DateOnly), Valid: true},
		Last:  sql.NullString{String: now.AddDate(0, 0, upcomingAhead).Format(time.DateOnly), Valid: true},
	})
	if err != nil {
		return nil, err
	}
	out := []api.UpcomingEpisode{}
	if len(rows) == 0 {
		return out, nil
	}
	names := make(map[int64]query.ListEpisodesAiringRow, len(rows))
	for _, r := range rows {
		names[r.ID] = r
	}
	f, err := s.loadFacts(ctx, 0)
	if err != nil {
		return nil, err
	}
	series := make(map[int64]Series, len(f.series))
	for _, sr := range f.series {
		series[sr.ID] = sr
	}
	for _, e := range f.episodes {
		n, listed := names[e.ID]
		sr, ok := series[e.SeriesID]
		if !listed || !ok || !e.Season.Valid || e.Season.Int64 < 1 {
			continue
		}
		aired := e.Aired.String
		_, hasFile := f.files[e.ID]
		past := aired <= today
		if past && hasFile {
			continue
		}
		d := f.downloads[e.ID]
		search, searched := f.searches[e.ID]
		state := episodeState(hasFile, d, search, searched, past)
		up := api.UpcomingEpisode{
			EpisodeID: e.ID, SeriesID: sr.ID, SeriesName: sr.Name, SeriesNameEng: sr.NameEng,
			Season: int(e.Season.Int64), Number: int(e.Number.Int64), Name: n.Name.String, NameEng: n.NameEng.String,
			Aired: aired, Monitored: sr.Monitors(aired),
		}
		if sc != nil && up.Monitored {
			own, ran := f.monitored[e.ID]
			up.NextSearch = sc.nextSearch(aired, hasFile, d, search, searched, own, ran, now)
		}
		// Acknowledgment changes only what needs the user's attention, as
		// in the library, not the schedule.
		if state == EpisodeFailed && d != nil && !d.Acknowledged.IsZero() {
			state = episodeState(hasFile, nil, search, searched, past)
		}
		up.State = state
		out = append(out, up)
	}
	slices.SortFunc(out, func(a, b api.UpcomingEpisode) int {
		return cmp.Or(
			cmp.Compare(a.Aired, b.Aired),
			cmp.Compare(cmp.Or(a.SeriesNameEng, a.SeriesName), cmp.Or(b.SeriesNameEng, b.SeriesName)),
			cmp.Compare(a.SeriesID, b.SeriesID),
			cmp.Compare(a.Season, b.Season),
			cmp.Compare(a.Number, b.Number),
		)
	})
	return out, nil
}

// nextSearch is when the monitor searches a monitored episode that airs on
// aired, as Wanted schedules it: an episode that has not aired yet is
// missing from the start of its air date, and one that is due has now.
// own is the monitor's latest search of it, if it ran one. It is nil for
// an episode the monitor leaves alone as it stands, such as one that
// downloads.
func (sc Schedule) nextSearch(aired string, hasFile bool, d *Download, search searchOutcome, searched bool,
	own searchOutcome, ran bool, now time.Time) *time.Time {
	// The state the episode has once it aired, if nothing changes before.
	if !wants(episodeState(hasFile, d, search, searched, true), d) {
		return nil
	}
	t := sc.next(false, aired, own, ran, d)
	if day, err := time.ParseInLocation(time.DateOnly, aired, now.Location()); err == nil && t.Before(day) {
		t = day
	}
	if t.Before(now) {
		t = now // due: the monitor gets to it soon
	}
	return &t
}
