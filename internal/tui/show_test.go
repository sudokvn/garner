package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/sudokvn/garner/internal/api"
)

// TestAbsoluteNumbers shows an anime episode's absolute number next to its
// TVDB position where they differ, so it can be found in release names
// that number the whole show, such as E059. TV shows keep their labels.
func TestAbsoluteNumbers(t *testing.T) {
	for _, typ := range []string{api.TypeAnime, api.TypeTV} {
		data := api.SeriesData{Series: api.Series{ID: 42, Name: "Lantern Harbor", DefaultOrder: "official", Type: typ}}
		for s := 1; s <= 2; s++ {
			for n := 1; n <= 58; n++ {
				data.Episodes = append(data.Episodes, api.Episode{ID: int64(100*s + n), Aired: "2026-01-01",
					Numbers: map[string]api.Position{"official": {Season: s, Number: n}}})
			}
		}
		show := newShow(&session{}, nil, 42, "Lantern Harbor", "")
		// The cursor starts on S02E01, which needs a search.
		show.setDetail(api.SeriesDetail{SeriesData: data, States: []api.EpisodeStatus{{EpisodeID: 201, State: api.EpisodeNotSearched}}})
		view := ansi.Strip(show.view(120, 36))
		row, ok := show.cursorEpisode()
		if !ok {
			t.Fatalf("%s: no episode under the cursor", typ)
		}
		facts := ansi.Strip(newReleases(&session{}, show, row, false).view(120, 36))
		// On a narrow terminal the absolute numbers give way, so the rows
		// stay within the width.
		for _, line := range strings.Split(ansi.Strip(show.view(60, 36)), "\n") {
			if ansi.StringWidth(line) > 60 || strings.Contains(line, "S02E01 ·") {
				t.Errorf("%s at 60 columns: %q", typ, line)
			}
		}
		switch typ {
		case api.TypeAnime:
			if !strings.Contains(view, "S02E01 · 59") || !strings.Contains(facts, "Episode  S02E01") || !strings.Contains(facts, "Absolute  59") {
				t.Errorf("anime:\n%s\n%s", view, facts)
			}
		default:
			if strings.Contains(view, "S02E01 ·") || !strings.Contains(view, "S02E01  ") || strings.Contains(facts, "Absolute") {
				t.Errorf("TV:\n%s\n%s", view, facts)
			}
		}
	}
}
