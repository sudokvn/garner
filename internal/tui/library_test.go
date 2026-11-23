package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sudokvn/garner/internal/api"
	"github.com/sudokvn/garner/internal/client"
	"github.com/sudokvn/garner/internal/connect"
)

// TestLibraryTabs offers only the categories garner has shows of: All, TV
// and Anime, then Upcoming. tab and shift+tab go round them at every
// width, also where the preview shows, and 1 to 4 jump to one.
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
	for _, width := range []int{120, previewWidth} {
		a.width, a.height, a.library.cat = width, 36, catAll
		a.View()
		if a.library.wide() != (width >= previewWidth) {
			t.Fatalf("at %d columns the preview shows: %v", width, a.library.wide())
		}
		var seen []category
		for range 5 {
			press(tea.KeyTab, 0)
			a.View()
			seen = append(seen, a.library.cat)
		}
		if want := []category{catTV, catAnime, catUpcoming, catAll, catTV}; !slices.Equal(seen, want) {
			t.Errorf("at %d columns tab went through %v, want %v", width, seen, want)
		}
		press(tea.KeyTab, tea.ModShift) // from TV
		press(tea.KeyTab, tea.ModShift)
		if a.library.cat != catUpcoming {
			t.Errorf("at %d columns shift+tab twice from TV ended on %v, want Upcoming", width, a.library.cat)
		}
	}
	press('2', 0)
	press('5', 0)
	if a.library.cat != catTV {
		t.Errorf("after 2 and 5 the category is %v, want TV: 5 names no category", a.library.cat)
	}
}

// TestUpcoming gives every day from two days ago to a week ahead a row,
// empty days too, then lists later episodes under Later. The last column
// says what garner will do, and enter opens the episode's show. s searches
// an aired episode now and esc comes back to the tab; on an episode that
// has not aired, s does nothing.
func TestUpcoming(t *testing.T) {
	sess := &session{}
	s := newLibrary(sess)
	s.loaded, s.cat = true, catUpcoming
	past := time.Date(2026, 11, 21, 18, 0, 0, 0, time.Local)
	airs := time.Date(2026, 11, 26, 0, 0, 0, 0, time.Local)
	s.upcoming = &api.Upcoming{Today: "2026-11-22", Episodes: []api.UpcomingEpisode{
		{EpisodeID: 1, SeriesID: 10, SeriesName: "Lanterns and Tides", Season: 1, Number: 11, Name: "The Night Market",
			Aired: "2026-11-21", State: api.EpisodeWaiting, Monitored: true, NextSearch: &past},
		{EpisodeID: 2, SeriesID: 11, SeriesName: "Night Ferry", Season: 2, Number: 9, Name: "The Long Pier",
			Aired: "2026-11-26", State: api.EpisodeUpcoming, Monitored: true, NextSearch: &airs},
		{EpisodeID: 3, SeriesID: 12, SeriesName: "Salt and Signal", Season: 1, Number: 11,
			Aired: "2026-11-26", State: api.EpisodeUpcoming},
		{EpisodeID: 4, SeriesID: 10, SeriesName: "Lanterns and Tides", Season: 1, Number: 12,
			Aired: "2026-12-03", State: api.EpisodeUpcoming, Monitored: true, NextSearch: &airs},
	}}
	view := ansi.Strip(s.view(120, 36))
	want := []string{"Fri 20", "Sat 21", "Sun 22 · today", "Mon 23", "Tue 24", "Wed 25", "Thu 26", "Fri 27", "Sat 28", "Thu 3 Dec"}
	at := 0
	for _, line := range strings.Split(view, "\n") {
		if at < len(want) && strings.Contains(line, want[at]) {
			at++
		}
	}
	if at < len(want) {
		t.Errorf("the days stop before %q:\n%s", want[at], view)
	}
	for _, w := range []string{"Upcoming 4", "missing · next search soon", "searches the day it airs", "not monitored", "no title yet", "Later"} {
		if !strings.Contains(view, w) {
			t.Errorf("Upcoming lacks %q:\n%s", w, view)
		}
	}
	// The cursor starts on the first episode from today, Night Ferry.
	cmd := s.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter opened nothing")
	}
	sw, ok := cmd().(switchMsg)
	if show, isShow := sw.screen.(*showScreen); !ok || !isShow || show.id != 11 {
		t.Errorf("enter opened %#v, want the show Night Ferry", sw.screen)
	}

	if cmd := s.key(tea.KeyPressMsg{Code: 's', Text: "s"}); cmd != nil {
		t.Errorf("s on an episode that has not aired gave %#v", cmd())
	}
	s.key(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if view := ansi.Strip(s.view(120, 36)); !strings.Contains(view, "s search now") {
		t.Errorf("the key bar lacks s search now:\n%s", view)
	}
	cmd = s.key(tea.KeyPressMsg{Code: 's', Text: "s"})
	if cmd == nil {
		t.Fatal("s on an aired episode searched nothing")
	}
	sw, _ = cmd().(switchMsg)
	rel, ok := sw.screen.(*releasesScreen)
	if !ok || rel.episode.episode.ID != 1 || rel.back.id != 10 || !rel.searching {
		t.Fatalf("s opened %#v, want a new search of Lanterns and Tides S01E11", sw.screen)
	}
	if sw, _ = rel.key("esc")().(switchMsg); sw.screen != s || s.upSelected != 1 {
		t.Errorf("esc went to %#v with the cursor on %d, want the tab on episode 1", sw.screen, s.upSelected)
	}
}

// TestLibraryBeforeUpcoming shows the shows as soon as the library
// answers: a slow schedule must not keep the main list loading.
func TestLibraryBeforeUpcoming(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/series":
			json.NewEncoder(w).Encode([]api.Show{{Series: api.Series{ID: 1, Name: "Harbor Precinct", Type: api.TypeTV}}})
		case "/api/v1/upcoming":
			<-release
			json.NewEncoder(w).Encode(api.Upcoming{Today: "2026-11-22"})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	defer close(release)
	cl, err := client.New(connect.Code{URL: srv.URL, Token: "test"})
	if err != nil {
		t.Fatal(err)
	}
	s := newLibrary(&session{client: cl})

	msgs := make(chan tea.Msg, 4)
	var run func(tea.Cmd)
	run = func(cmd tea.Cmd) {
		go func() {
			msg := cmd()
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, c := range batch {
					if c != nil {
						run(c)
					}
				}
				return
			}
			msgs <- msg
		}()
	}
	run(s.reload())
	select {
	case msg := <-msgs:
		if _, ok := msg.(libraryMsg); !ok {
			t.Fatalf("the first answer is %T, not the library", msg)
		}
		s.update(msg)
	case <-time.After(5 * time.Second):
		t.Fatal("the library waits for the upcoming episodes")
	}
	if view := ansi.Strip(s.view(120, 36)); !strings.Contains(view, "Harbor Precinct") {
		t.Errorf("the library does not list its show while Upcoming loads:\n%s", view)
	}
}
