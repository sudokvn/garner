package store

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/sudokvn/garner/internal/api"
)

// TestUpcoming checks which episodes the upcoming list holds, and that it
// tells when the monitor searches each only where the monitor gets it: an
// unaired episode from the start of its air date, a missing one by the
// monitor's schedule.
func TestUpcoming(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, t.TempDir(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Date(2026, 11, 22, 15, 0, 0, 0, time.Local)
	ep := func(id int64, aired string, season, number int) Episode {
		return Episode{ID: id, Aired: aired, Name: fmt.Sprintf("Episode %d", id), Numbers: map[string]Position{"official": {Season: season, Number: number}}}
	}
	save := func(id int64, name, mode string, episodes ...Episode) {
		t.Helper()
		if err := st.SaveSeries(ctx, SeriesData{
			Series:   Series{ID: id, Slug: "s", Name: name, OriginalLanguage: "eng", OriginalCountry: "usa", Status: "Continuing", DefaultOrder: "official", TVDBUpdated: "x", Type: "tv", Monitor: mode},
			Episodes: episodes,
		}, false, now.AddDate(0, -1, 0)); err != nil {
			t.Fatal(err)
		}
	}
	save(1, "Harbor Precinct", MonitorAll,
		ep(101, "2026-11-19", 1, 1), // three days ago: too old
		ep(102, "2026-11-20", 1, 2), // missing, searched by the monitor an hour ago
		ep(103, "2026-11-21", 1, 3), // in the library
		ep(104, "2026-11-22", 1, 4), // today, downloading
		ep(105, "2026-11-23", 1, 5), // tomorrow
		ep(106, "2026-12-06", 1, 6), // in fourteen days
		ep(107, "2026-12-07", 1, 7), // in fifteen days: too far
		ep(108, "2026-11-24", 0, 1)) // a special
	save(2, "Alpine Rescue", MonitorOff,
		ep(201, "2026-11-22", 2, 1)) // today, never searched
	if err := st.SaveMonitorSearch(ctx, 102, now.Add(-time.Hour), api.SearchNothing); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSearch(ctx, Search{EpisodeID: 102, Searched: now.Add(-time.Hour), Outcome: api.SearchNothing, Summary: "nothing"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetEpisodeFile(ctx, EpisodeFile{EpisodeID: 103, Path: "/lib/103.mkv", Size: 1}, 0, now); err != nil {
		t.Fatal(err)
	}
	d, err := st.StartDownload(ctx, Download{EpisodeID: 104, Title: "Harbor.Precinct.S01E04.1080p.WEB-GRP", Indexer: "nzb-one", Link: "x"}, now)
	if err != nil {
		t.Fatal(err)
	}
	d.State = Downloading
	if err := st.UpdateDownload(ctx, d, now); err != nil {
		t.Fatal(err)
	}
	sched := Schedule{Recent: 14 * 24 * time.Hour, MissingRecent: 6 * time.Hour, MissingOlder: 24 * time.Hour}

	describe := func(list []api.UpcomingEpisode) string {
		var lines []string
		for _, u := range list {
			next := "-"
			if u.NextSearch != nil {
				next = u.NextSearch.Format("01-02 15:04")
			}
			lines = append(lines, fmt.Sprintf("%d %s %s %s %v %s", u.EpisodeID, u.Aired, u.Name, u.State, u.Monitored, next))
		}
		return strings.Join(lines, "\n")
	}
	got, err := st.Upcoming(ctx, now, &sched)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"102 2026-11-20 Episode 102 waiting true 11-22 20:00",
		"201 2026-11-22 Episode 201 not_searched false -",
		"104 2026-11-22 Episode 104 downloading true -",
		"105 2026-11-23 Episode 105 upcoming true 11-23 00:00",
		"106 2026-12-06 Episode 106 upcoming true 12-06 00:00",
	}, "\n")
	if s := describe(got); s != want {
		t.Errorf("upcoming:\n%s\nwant:\n%s", s, want)
	}
	if got[0].SeriesName != "Harbor Precinct" || got[0].Season != 1 || got[0].Number != 2 {
		t.Errorf("first episode = %+v", got[0])
	}

	// Without the monitor, nothing says when it searches.
	got, err = st.Upcoming(ctx, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range got {
		if u.NextSearch != nil {
			t.Errorf("episode %d is searched at %v without the monitor", u.EpisodeID, u.NextSearch)
		}
	}
}
