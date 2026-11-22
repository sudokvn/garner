package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sudokvn/garner/internal/api"
	"github.com/sudokvn/garner/internal/client"
	"github.com/sudokvn/garner/internal/connect"
)

// TestPalette checks that the palette lists a screen's actions and runs
// them through the screen's own keys, so deleting a show from it still
// asks first, and that typing finds shows and profiles.
func TestPalette(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/profiles" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode([]api.Profile{{ID: 3, Name: "Original language", Shows: []string{"Harbor Precinct"}}})
	}))
	defer srv.Close()
	cl, err := client.New(connect.Code{URL: srv.URL, Token: "test"})
	if err != nil {
		t.Fatal(err)
	}
	sess := &session{client: cl, shows: []api.Show{
		{Series: api.Series{ID: 7, Name: "Harbor Precinct"}}, {Series: api.Series{ID: 8, Name: "Glass Orchard"}},
	}}
	library := newLibrary(sess)
	library.loaded = true
	a := &app{sess: sess, library: library, screen: library, width: 120, height: 36}

	// Every key a palette entry hands on must arrive as that key.
	show := newShow(sess, library, 7, "Harbor Precinct", "")
	for _, s := range []screen{library, show, newEpisode(sess, show, 101), newProfiles(sess, library), newActivity(sess, library)} {
		for _, e := range screenActions(s) {
			if got := keyPress(e.key).String(); got != e.key {
				t.Errorf("%T: the entry %q hands on %q, not %q", s, e.label, got, e.key)
			}
		}
	}

	cmd := sendKeys(a, ":")
	if a.palette == nil || cmd == nil {
		t.Fatal(": did not open the palette")
	}
	a.update(cmd())
	view := ansi.Strip(a.View().Content)
	for _, want := range []string{"Go to or do", "On this screen", "> Open the show", "Delete the show", "Anywhere", "Next episode that needs you", "About garner"} {
		if !strings.Contains(view, want) {
			t.Errorf("the palette lacks %q:\n%s", want, view)
		}
	}
	for _, moving := range []string{"Next category", "Jump to a category"} {
		if strings.Contains(view, moving) {
			t.Errorf("the palette lists the moving key %q", moving)
		}
	}

	size := len(a.paletteLines(a.width, a.height))
	typed := func(s string) []paletteEntry {
		a.update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
		for _, r := range s {
			a.update(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
		return a.paletteEntries()
	}
	if got := typed("orch"); len(got) == 0 || got[0].label != "Glass Orchard" {
		t.Errorf("typing orch found %v first, want the show Glass Orchard", got)
	}
	if got := len(a.paletteLines(a.width, a.height)); got != size {
		t.Errorf("typing changed the palette from %d to %d lines; its field would move", size, got)
	}
	if got := typed("original"); len(got) == 0 || got[0].label != "Original language" || got[0].note != "profile · 1 show" {
		t.Errorf("typing original found %+v first, want the profile", got)
	}

	typed("delete")
	sendKeys(a, "enter")
	if a.palette != nil || a.screen != screen(library) || !library.asking || library.choice != 0 {
		t.Fatalf("choosing delete did not ask its question on the cancel choice: palette %v, asking %v", a.palette != nil, library.asking)
	}
	sendKeys(a, "esc")
	if library.asking {
		t.Error("esc did not close the delete question")
	}

	sendKeys(a, ":")
	sendKeys(a, "esc")
	if a.palette != nil || a.screen != screen(library) {
		t.Error("esc did not close the palette")
	}

	// help() lists keys that need a row; chosen elsewhere, the palette
	// says that they do nothing rather than closing without a word.
	episode := newEpisode(sess, show, 101)
	episode.detail = &api.EpisodeDetail{}
	a.screen = episode
	sendKeys(a, ":")
	typed("cancel the download")
	sendKeys(a, "enter")
	if !strings.Contains(ansi.Strip(episode.flash.text), "Cancel the download does not apply here") || episode.asking {
		t.Errorf("cancelling without a download said %q", ansi.Strip(episode.flash.text))
	}
}
