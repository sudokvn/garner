package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sudokvn/garner/internal/api"
)

// TestPreview shows the show under the cursor in the right half from 160
// columns, and leaves narrower terminals as they were. l and right move
// into the pane, h, left and esc back to the list; tab and 1 to 4 change
// the category from either and leave the keys with the list. enter opens
// the episode under the pane's cursor and s searches it, and esc on its
// releases comes back to the pane as it was.
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
	named := map[string]tea.KeyPressMsg{
		"tab": {Code: tea.KeyTab}, "shift+tab": {Code: tea.KeyTab, Mod: tea.ModShift}, "enter": {Code: tea.KeyEnter},
		"esc": {Code: tea.KeyEscape}, "left": {Code: tea.KeyLeft}, "right": {Code: tea.KeyRight},
	}
	press := func(key string) tea.Cmd {
		t.Helper()
		msg, ok := named[key]
		if !ok {
			msg = tea.KeyPressMsg{Code: []rune(key)[0], Text: key}
		}
		if msg.String() != key {
			t.Fatalf("the key press for %q reads %q", key, msg.String())
		}
		return s.key(msg)
	}

	// Before the first view, the preview loads the first row's show.
	if id, ok := s.previewID(); !ok || id != 1 {
		t.Errorf("before the first view the preview shows show %d, %v; want 1", id, ok)
	}
	narrow := ansi.Strip(s.view(159, 36))
	if strings.Contains(narrow, "Profile") || strings.Contains(narrow, "│") {
		t.Errorf("at 159 columns the preview shows:\n%s", narrow)
	}
	for _, key := range []string{"l", "right", "h", "left"} {
		press(key)
		if s.pv.focused || s.cat != catAll {
			t.Errorf("at 159 columns %s focused the pane %v, category %v; want nothing to happen", key, s.pv.focused, s.cat)
		}
	}
	press("tab")
	if s.pv.focused || s.cat != catTV {
		t.Errorf("at 159 columns tab focused the pane %v, category %v; want the next category", s.pv.focused, s.cat)
	}
	press("1")

	wide := ansi.Strip(s.view(160, 36))
	for _, want := range []string{"Harbor Precinct   TV · Airing", "Profile  German", "Season 1   1 of 2", "S01E02   Low Tide", "German 1080p", "l into the pane", "tab category"} {
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
	press("h")
	if s.pv.focused || s.cat != catTV {
		t.Errorf("at 160 columns tab then h: focused %v, category %v; want the list, TV", s.pv.focused, s.cat)
	}
	press("shift+tab")
	if s.pv.focused || s.cat != catAll {
		t.Errorf("at 160 columns shift+tab: focused %v, category %v; want the list, All", s.pv.focused, s.cat)
	}
	press("l")
	if !s.pv.focused || s.pv.cursor != 102 {
		t.Fatalf("l left the pane unfocused (%v) or off the show's focus episode (%d)", s.pv.focused, s.pv.cursor)
	}
	view := ansi.Strip(s.view(160, 36))
	if !strings.Contains(view, "> ○   S01E02") {
		t.Errorf("the pane's cursor is not on S01E02:\n%s", view)
	}
	for _, want := range []string{"h back to the list", "tab category"} {
		if !strings.Contains(view, want) {
			t.Errorf("the pane's key bar lacks %q:\n%s", want, view)
		}
	}
	// The pane lists the newest episode first; l in it changes nothing.
	press("l")
	press("j")
	if !s.pv.focused || s.pv.cursor != 101 {
		t.Errorf("l then j in the pane: focused %v on %d; want the pane on 101", s.pv.focused, s.pv.cursor)
	}
	press("k")
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
	var searched tea.Msg
	if cmd := press("s"); cmd != nil {
		searched = cmd()
	}
	sw, _ := searched.(switchMsg)
	r, ok := sw.screen.(*releasesScreen)
	if !ok || !r.searching || r.episode.episode.ID != 102 || r.back.id != 1 || r.leave != screen(s) {
		t.Fatalf("s in the pane gave %#v, want a new search of S01E02 leading back to the library", searched)
	}
	if sw, _ = r.key("esc")().(switchMsg); sw.screen != screen(s) || !s.pv.focused || s.pv.cursor != 102 {
		t.Errorf("esc on the releases went to %T, pane focused %v on %d; want the library's pane on 102", sw.screen, s.pv.focused, s.pv.cursor)
	}
	for _, out := range []string{"h", "left", "esc"} {
		press("right")
		if !s.pv.focused {
			t.Fatalf("right did not move into the pane")
		}
		press(out)
		if s.pv.focused || s.cat != catAll {
			t.Errorf("%s in the pane: focused %v, category %v; want the list, All", out, s.pv.focused, s.cat)
		}
	}
	press("l")
	press("tab")
	if s.pv.focused || s.cat != catTV {
		t.Errorf("tab in the pane: focused %v, category %v; want the list, TV", s.pv.focused, s.cat)
	}
	press("l")
	press("shift+tab")
	if s.pv.focused || s.cat != catAll {
		t.Errorf("shift+tab in the pane: focused %v, category %v; want the list, All", s.pv.focused, s.cat)
	}
	press("l")
	press("2")
	if s.pv.focused || s.cat != catTV {
		t.Errorf("2 in the pane: focused %v, category %v; want the list, TV", s.pv.focused, s.cat)
	}
}
