package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sudokvn/garner/internal/api"
)

// TestPreview shows the show under the cursor in the right half from 160
// columns, and leaves narrower terminals as they were. tab moves into the
// pane and back, the categories stay on 1 to 4, and enter opens the
// episode under the pane's cursor.
func TestPreview(t *testing.T) {
	sess := &session{shows: []api.Show{
		{Series: api.Series{ID: 1, Name: "Harbor Precinct", Type: api.TypeTV}, Aired: 2, Imported: 1,
			Focus: &api.Focus{EpisodeID: 102, Season: 1, Number: 2, State: api.EpisodeNotSearched}},
	}}
	s := newLibrary(sess)
	s.loaded = true
	data := api.SeriesData{Series: api.Series{ID: 1, Name: "Harbor Precinct", Type: api.TypeTV, DefaultOrder: "official", Status: "Continuing"}}
	for n := 1; n <= 2; n++ {
		data.Episodes = append(data.Episodes, api.Episode{ID: int64(100 + n), Name: []string{"", "Pilot", "Low Tide"}[n],
			Aired: "2026-01-01", Numbers: map[string]api.Position{"official": {Season: 1, Number: n}}})
	}
	s.pv.details = map[int64]*api.SeriesDetail{1: {SeriesData: data, Profile: "German", States: []api.EpisodeStatus{
		{EpisodeID: 101, State: api.EpisodeImported, Tier: "German 1080p"}, {EpisodeID: 102, State: api.EpisodeNotSearched}}}}
	s.pv.fresh = map[int64]bool{1: true}
	press := func(key string) tea.Cmd {
		code := []rune(key)[0]
		if key == "tab" {
			return s.key(tea.KeyPressMsg{Code: tea.KeyTab})
		}
		if key == "enter" {
			return s.key(tea.KeyPressMsg{Code: tea.KeyEnter})
		}
		return s.key(tea.KeyPressMsg{Code: code, Text: key})
	}

	// Before the first view, the preview loads the first row's show.
	if id, ok := s.previewID(); !ok || id != 1 {
		t.Errorf("before the first view the preview shows show %d, %v; want 1", id, ok)
	}
	narrow := ansi.Strip(s.view(159, 36))
	if strings.Contains(narrow, "Profile") || strings.Contains(narrow, "│") {
		t.Errorf("at 159 columns the preview shows:\n%s", narrow)
	}
	press("tab")
	if s.pv.focused || s.cat != catTV {
		t.Errorf("at 159 columns tab focused the pane %v, category %v; want the next category", s.pv.focused, s.cat)
	}
	press("1")

	wide := ansi.Strip(s.view(160, 36))
	for _, want := range []string{"Harbor Precinct   TV · Airing", "Profile  German", "Season 1   1 of 2", "S01E02   Low Tide", "German 1080p", "tab into the pane"} {
		if !strings.Contains(wide, want) {
			t.Errorf("at 160 columns the preview lacks %q:\n%s", want, wide)
		}
	}
	for _, line := range strings.Split(wide, "\n") {
		if ansi.StringWidth(line) > 160 {
			t.Errorf("line wider than 160: %q", line)
		}
	}
	press("tab")
	if !s.pv.focused || s.pv.cursor != 102 {
		t.Fatalf("tab left the pane unfocused (%v) or off the show's focus episode (%d)", s.pv.focused, s.pv.cursor)
	}
	if view := ansi.Strip(s.view(160, 36)); !strings.Contains(view, "> ○   S01E02") {
		t.Errorf("the pane's cursor is not on S01E02:\n%s", view)
	}
	// enter on a missing episode opens its releases, with the show behind.
	var opened tea.Msg
	if cmd := press("enter"); cmd != nil {
		opened = cmd()
	}
	if sw, ok := opened.(switchMsg); !ok {
		t.Errorf("enter in the pane gave %#v, want a switch", opened)
	} else if r, ok := sw.screen.(*releasesScreen); !ok || r.back == nil || r.back.back != screen(s) {
		t.Errorf("enter in the pane opened %T, want the releases with the show and the library behind", sw.screen)
	}
	press("tab")
	if s.pv.focused {
		t.Error("tab did not leave the pane")
	}
	press("tab")
	press("2")
	if s.pv.focused || s.cat != catTV {
		t.Errorf("2 in the pane: focused %v, category %v; want the list, TV", s.pv.focused, s.cat)
	}
}
