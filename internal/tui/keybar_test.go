package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/sudokvn/garner/internal/api"
)

// TestKeyBarKeepsItsPlace moves the cursor over rows whose keys differ: the
// keys stay in place, so the eye finds them where they were (GRR-82).
func TestKeyBarKeepsItsPlace(t *testing.T) {
	data := api.SeriesData{
		Series: api.Series{ID: 42, Name: "Harbor Precinct", DefaultOrder: "official", Type: api.TypeTV},
		Episodes: []api.Episode{
			{ID: 101, Aired: "2026-01-01", Numbers: map[string]api.Position{"official": {Season: 1, Number: 1}}},
			{ID: 102, Aired: "2026-01-08", Numbers: map[string]api.Position{"official": {Season: 1, Number: 2}}},
			{ID: 103, Aired: "2026-01-15", Numbers: map[string]api.Position{"official": {Season: 1, Number: 3}}},
		},
	}
	show := newShow(&session{}, nil, 42, "Harbor Precinct", "")
	show.setDetail(api.SeriesDetail{SeriesData: data, States: []api.EpisodeStatus{
		{EpisodeID: 101, State: api.EpisodeImported, Size: 1 << 30, Tier: "German 1080p"},
		{EpisodeID: 102, State: api.EpisodeNotSearched},
		{EpisodeID: 103, State: api.EpisodeWaiting},
	}})
	// The column each key starts in, on every kind of row.
	starts := func(bar string) []int {
		var at []int
		for _, key := range []string{"h l ", "tab ", "S ", "i ", "? ", "esc "} {
			c := column(bar, key)
			if c < 0 {
				t.Errorf("show screen: no %q in %q", key, ansi.Strip(bar))
			}
			at = append(at, c)
		}
		return at
	}
	var first []int
	for _, row := range []rowKey{{season: 1, episode: 101}, {season: 1, episode: 102}, {season: 1, episode: 103}, {season: 1}} {
		show.cursor = row
		got := starts(show.bottom())
		if first == nil {
			first = got
			continue
		}
		if !slices.Equal(got, first) {
			t.Errorf("show screen, row %+v: keys start at %v, on S01E01 at %v\n%s", row, got, first, ansi.Strip(show.bottom()))
		}
	}

	// On a season row, enter folds it.
	show.cursor = rowKey{season: 1}
	if bar := ansi.Strip(show.bottom()); !strings.HasPrefix(bar, "enter fold") && !strings.HasPrefix(bar, "enter unfold") {
		t.Errorf("season row: key bar %q, want enter to fold or unfold", bar)
	}

	activity := newActivity(&session{}, nil)
	activity.items = []api.ActivityItem{
		{Download: api.Download{EpisodeID: 1, State: api.Downloading}},
		{Download: api.Download{EpisodeID: 2, State: api.Failed}},
		{Download: api.Download{EpisodeID: 3, State: api.Completed, Import: api.Import{State: api.ImportDone}}},
	}
	var help int
	for i, id := range []int64{1, 2, 3} {
		activity.selected = id
		at := column(activity.bottom(), "? help")
		if at < 0 {
			t.Errorf("activity, episode %d: no ? help in %q", id, ansi.Strip(activity.bottom()))
		}
		if i == 0 {
			help = at
		} else if at != help {
			t.Errorf("activity, episode %d: ? help starts at %d, on the first row at %d\n%s", id, at, help, ansi.Strip(activity.bottom()))
		}
	}

	// A narrow terminal still leaves out whole keys: the padding is no gap.
	show.cursor = rowKey{season: 1, episode: 102}
	narrow := ansi.Strip(fitKeys(show.bottom(), 60))
	if strings.Contains(narrow, "     ") || !strings.HasSuffix(narrow, "esc back") || !strings.Contains(narrow, "? help") {
		t.Errorf("narrow key bar %q", narrow)
	}
}

// TestBarSeparated keeps the last row of a full screen apart from the key
// bar: a rule runs between them (GRR-82).
func TestBarSeparated(t *testing.T) {
	body := strings.Repeat("row\n", 30)
	lines := strings.Split(screenLayout(80, 10, body, keyBar("?", "help")), "\n")
	if len(lines) != 10 {
		t.Fatalf("%d lines, want 10", len(lines))
	}
	if rule := ansi.Strip(lines[8]); strings.Trim(rule, " ─") != "" || !strings.Contains(rule, "─") {
		t.Errorf("the row above the key bar is %q, want a rule", rule)
	}
	if ansi.Strip(lines[7]) != "row" || !strings.Contains(ansi.Strip(lines[9]), "? help") {
		t.Errorf("rows around the rule: %q, %q", ansi.Strip(lines[7]), ansi.Strip(lines[9]))
	}
}

// column is the screen column where text starts in a rendered line.
func column(line, text string) int {
	plain := ansi.Strip(line)
	i := strings.Index(plain, text)
	if i < 0 {
		return -1
	}
	return ansi.StringWidth(plain[:i])
}
