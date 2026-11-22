package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/sudokvn/garner/internal/api"
)

// TestStripWithoutColour tells the six steps apart by shape when the
// terminal shows no colour, so a row still says where a download stands
// or where it broke. A running download that is not the episode's first
// names its attempt.
func TestStripWithoutColour(t *testing.T) {
	colourless = true
	t.Cleanup(func() { colourless = false })
	now := time.Now()
	text := func(parts []part) string {
		var b strings.Builder
		for _, p := range parts {
			b.WriteString(p.text)
		}
		return b.String()
	}
	downloading := &api.Download{State: api.Downloading, JobID: "SABnzbd_nzo_1", Progress: 42, Created: now}
	rejected := &api.Download{State: api.Completed, JobID: "SABnzbd_nzo_2", Created: now,
		Import: api.Import{State: api.ImportRejected, Detail: "the file's audio is English"}}
	unconfirmed := &api.Download{State: api.Completed, JobID: "SABnzbd_nzo_3", Created: now,
		Import: api.Import{State: api.ImportUnverified, Files: []api.ImportFile{{Target: "/library/x.mkv"}}}}
	for _, c := range []struct {
		state    api.EpisodeState
		d        *api.Download
		attempts int
		want     string
	}{
		{api.EpisodeDownloading, downloading, 2, "■▸□□□□  42% · attempt 2"},
		{api.EpisodeFailed, rejected, 1, "■■■✗□□  rejected · the file's audio is English"},
		{api.EpisodeAttention, unconfirmed, 1, "■■■■■▸  needs you"},
	} {
		if got := text(stripParts("", c.state, c.d, "", nil, c.attempts, now)); !strings.HasPrefix(got, c.want) {
			t.Errorf("%s: %q, want it to start with %q", c.state, got, c.want)
		}
	}
	// A missing episode has no download to show.
	if got := text(stripParts("", api.EpisodeNotSearched, nil, "", nil, 0, now)); got != "missing" {
		t.Errorf("missing: %q", got)
	}

	// On the Show screen, the row of the running download.
	data := api.SeriesData{Series: api.Series{ID: 42, Name: "Harbor Precinct", DefaultOrder: "official", Type: api.TypeTV}}
	data.Episodes = []api.Episode{{ID: 4201, Aired: "2026-01-01", Numbers: map[string]api.Position{"official": {Season: 1, Number: 1}}}}
	show := newShow(&session{}, nil, 42, "Harbor Precinct", "")
	show.setDetail(api.SeriesDetail{SeriesData: data, States: []api.EpisodeStatus{
		{EpisodeID: 4201, State: api.EpisodeDownloading, Attempts: 2, Download: downloading}}})
	if view := ansi.Strip(show.view(120, 36)); !strings.Contains(view, "■▸□□□□  42% · attempt 2") {
		t.Errorf("Show screen lacks the strip:\n%s", view)
	}
}
