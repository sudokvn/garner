package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/sudokvn/garner/internal/api"
	"github.com/sudokvn/garner/internal/client"
	"github.com/sudokvn/garner/internal/connect"
	"github.com/sudokvn/garner/internal/profile"
)

// TestNextNeeds walks the queue n opens: failures not cleared first, then
// files Jellyfin did not confirm, wrapping around; with nothing in it, n
// says so and stays.
func TestNextNeeds(t *testing.T) {
	at := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	item := func(ep int64, d api.Download) api.ActivityItem {
		d.ID, d.EpisodeID = ep, ep
		return api.ActivityItem{Download: d, SeriesID: 7, SeriesName: "Harbor Precinct", Season: 1, Number: int(ep - 100)}
	}
	var items []api.ActivityItem
	shows := []api.Show{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/activity":
			json.NewEncoder(w).Encode(items)
		case "/api/v1/series":
			json.NewEncoder(w).Encode(shows)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	cl, err := client.New(connect.Code{URL: srv.URL, Token: "test"})
	if err != nil {
		t.Fatal(err)
	}
	sess := &session{client: cl}
	library := newLibrary(sess)
	library.loaded = true
	a := &app{sess: sess, library: library, screen: library, width: 120, height: 36}
	press := func() {
		t.Helper()
		cmd := sendKeys(a, "n")
		if cmd == nil {
			t.Fatal("n did nothing")
		}
		a.update(cmd())
	}

	press()
	if a.screen != screen(library) || !strings.Contains(ansi.Strip(library.flash.text), "Nothing needs you") {
		t.Fatalf("with nothing in the queue, n left the library or did not say so: %q", library.flash.text)
	}

	items = []api.ActivityItem{
		item(104, api.Download{State: api.Completed, Import: api.Import{State: api.ImportUnverified}}),
		item(103, api.Download{State: api.Failed, Acknowledged: at}),
		item(102, api.Download{State: api.Completed, Import: api.Import{State: api.ImportFailed}}),
		item(101, api.Download{State: api.Failed}),
		item(100, api.Download{State: api.Downloading}),
	}
	// A failure older than the activity's day is still in the library;
	// one the library has not seen cleared yet stays cleared.
	shows = []api.Show{
		{Series: api.Series{ID: 7, Name: "Harbor Precinct"}, Focus: &api.Focus{EpisodeID: 102, State: api.EpisodeFailed}},
		{Series: api.Series{ID: 8, Name: "Glass Orchard"}, Focus: &api.Focus{EpisodeID: 201, State: api.EpisodeFailed}},
		{Series: api.Series{ID: 9, Name: "Copper Valley"}, Focus: &api.Focus{EpisodeID: 103, State: api.EpisodeFailed}},
	}
	want := []int64{102, 101, 201, 104, 102}
	for i, ep := range want {
		press()
		s, ok := a.screen.(*episodeScreen)
		if !ok || s.id != ep {
			t.Fatalf("n number %d opened %T, want episode %d", i+1, a.screen, ep)
		}
		count := []string{"1 of 4", "2 of 4", "3 of 4", "4 of 4", "1 of 4"}[i]
		if bottom := ansi.Strip(a.View().Content); !strings.Contains(bottom, "needs you · "+count) {
			t.Errorf("episode %d: the bottom line does not say %q", ep, count)
		}
	}
	if cmd := sendKeys(a, "esc"); cmd != nil {
		a.update(cmd())
	}
	if a.screen != screen(library) {
		t.Errorf("esc on the queue's episode went to %T, not where n was first pressed", a.screen)
	}
	if strings.Contains(ansi.Strip(a.View().Content), "needs you ·") {
		t.Error("the queue's count stayed after leaving its episode")
	}
}

// TestNeedsLeadsBack checks that the Show behind an episode n opened leads
// back where n was pressed, so a profile editor's unsaved changes are not
// lost on the way back from Other releases.
func TestNeedsLeadsBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/activity":
			json.NewEncoder(w).Encode([]api.ActivityItem{{
				Download: api.Download{ID: 1, EpisodeID: 101, State: api.Failed},
				SeriesID: 7, SeriesName: "Harbor Precinct", Season: 1, Number: 1,
			}})
		case "/api/v1/series":
			json.NewEncoder(w).Encode([]api.Show{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	cl, err := client.New(connect.Code{URL: srv.URL, Token: "test"})
	if err != nil {
		t.Fatal(err)
	}
	sess := &session{client: cl}
	ed := newEditor(sess, nil, 1, profile.Profile{Name: "Harbor"}, 1)
	ed.p.Upgrades = !ed.p.Upgrades
	a := &app{sess: sess, library: newLibrary(sess), screen: ed, width: 120, height: 36}

	a.update(sendKeys(a, "n")())
	ep, ok := a.screen.(*episodeScreen)
	if !ok {
		t.Fatalf("n opened %T, not the episode", a.screen)
	}
	// Other releases, then esc back to the Show.
	a.update(switchMsg{ep.show})
	if cmd := sendKeys(a, "esc"); cmd != nil {
		a.update(cmd())
	}
	if a.screen != screen(ed) || !ed.changed() {
		t.Errorf("esc on the Show went to %T, not back to the editor with its changes", a.screen)
	}
}
