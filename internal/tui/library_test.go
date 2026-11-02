package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/trucke/garner/internal/api"
)

// TestLibraryTabs offers only the categories garner has shows of: All, TV
// and Anime. tab and shift+tab go round them, and 1 to 3 jump to one.
func TestLibraryTabs(t *testing.T) {
	sess := &session{}
	a := &app{sess: sess, library: newLibrary(sess)}
	a.screen = a.library
	a.library.loaded = true
	sess.shows = []api.Show{
		{Series: api.Series{ID: 1, Name: "Harbor Precinct", Type: api.TypeTV}},
		{Series: api.Series{ID: 2, Name: "Lanterns and Tides", Type: api.TypeAnime}},
	}
	tabs := ansi.Strip(a.library.tabs())
	for _, want := range []string{"All 2", "TV 1", "Anime 1"} {
		if !strings.Contains(tabs, want) {
			t.Errorf("tabs %q lack %q", tabs, want)
		}
	}
	if strings.Contains(tabs, "Movies") {
		t.Errorf("tabs %q offer Movies, which garner does not have", tabs)
	}

	press := func(code rune, mod tea.KeyMod) {
		t.Helper()
		text := string(code)
		if code == tea.KeyTab {
			text = ""
		}
		a.update(tea.KeyPressMsg{Code: code, Mod: mod, Text: text})
	}
	var seen []category
	for range 4 {
		press(tea.KeyTab, 0)
		seen = append(seen, a.library.cat)
	}
	if want := []category{catTV, catAnime, catAll, catTV}; !slices.Equal(seen, want) {
		t.Errorf("tab went through %v, want %v", seen, want)
	}
	press(tea.KeyTab, tea.ModShift) // from TV
	press(tea.KeyTab, tea.ModShift)
	if a.library.cat != catAnime {
		t.Errorf("shift+tab twice from TV ended on %v, want Anime", a.library.cat)
	}
	press('2', 0)
	press('4', 0)
	if a.library.cat != catTV {
		t.Errorf("after 2 and 4 the category is %v, want TV: 4 names no category", a.library.cat)
	}
}
