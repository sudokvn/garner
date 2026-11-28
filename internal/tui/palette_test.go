package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sudokvn/garner/internal/api"
	"github.com/sudokvn/garner/internal/client"
	"github.com/sudokvn/garner/internal/connect"
	"github.com/sudokvn/garner/internal/profile"
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

	// Help opens About with enter, not with a, which the Library lists
	// just above for adding a show; the palette's entry says so.
	anywhere := a.anywhere()
	if i := slices.IndexFunc(anywhere, func(e paletteEntry) bool { return e.label == "About garner" }); i < 0 || anywhere[i].key != "enter" {
		t.Errorf("the palette's About garner is not on enter in Help: %+v", anywhere)
	}
	sendKeys(a, "?", "a")
	if !a.help || a.screen != screen(library) {
		t.Errorf("a in Help closed it (%v) for %T", !a.help, a.screen)
	}
	if !strings.Contains(ansi.Strip(a.View().Content), "enter about garner") {
		t.Errorf("Help's footer lacks enter about garner:\n%s", ansi.Strip(a.View().Content))
	}
	sendKeys(a, "enter")
	if _, ok := a.screen.(*aboutScreen); a.help || !ok {
		t.Errorf("enter in Help opened %T, Help still open %v; want About", a.screen, a.help)
	}
	a.screen = library

	// The palette leaves out a key that does not apply on the row. One
	// that turns out to do nothing all the same says so rather than
	// closing without a word.
	episode := newEpisode(sess, show, 101)
	episode.detail = &api.EpisodeDetail{}
	a.screen = episode
	sendKeys(a, ":")
	for _, e := range a.paletteEntries() {
		if e.key == "x" {
			t.Errorf("the palette offers %q on an episode without a download", e.label)
		}
	}
	sendKeys(a, "esc")
	a.pressSaying("x", "Cancel the download")
	if !strings.Contains(ansi.Strip(episode.flash.text), "Cancel the download does not apply here") || episode.asking {
		t.Errorf("cancelling without a download said %q", ansi.Strip(episode.flash.text))
	}
}

// TestPaletteOffersWhatRuns walks each screen that answers for its keys
// over rows where they differ: the palette offers a key the key bar
// shows, and leaves out one it greys out or leaves out, so choosing an
// action never meets "does not apply here". A key whose meaning depends
// on the row carries the key bar's label.
func TestPaletteOffersWhatRuns(t *testing.T) {
	// keyState finds key in a key bar: on, greyed out or absent, with its
	// label there.
	keyState := func(bar, key string) (string, string) {
		for _, part := range strings.Split(bar, keyGap) {
			plain := strings.TrimSpace(strings.ReplaceAll(ansi.Strip(part), "\u00a0", " "))
			if k, label, _ := strings.Cut(plain, " "); k == key {
				if strings.Contains(part, sKey.Render(key)) {
					return "on", label
				}
				return "greyed out", label
			}
		}
		return "absent", ""
	}
	check := func(name string, s screen, bar string, keys ...string) {
		t.Helper()
		_, pairs := s.(keyHelper).help()
		for _, key := range keys {
			state, barLabel := keyState(bar, key)
			var offered []string
			for _, e := range screenActions(s) {
				if e.key == key {
					offered = append(offered, e.label)
				}
			}
			switch {
			case state == "on" && len(offered) == 0:
				t.Errorf("%s: the key bar offers %s, the palette does not\n%s", name, key, ansi.Strip(bar))
			case state != "on" && len(offered) > 0:
				t.Errorf("%s: %s is %s in the key bar, the palette offers %q\n%s", name, key, state, offered, ansi.Strip(bar))
			}
			for _, label := range offered {
				known := label == upperFirst(barLabel)
				for i := 0; i+1 < len(pairs); i += 2 {
					known = known || strings.Fields(pairs[i])[0] == key && label == upperFirst(pairs[i+1])
				}
				if !known {
					t.Errorf("%s: the palette calls %s %q, the key bar %q", name, key, label, barLabel)
				}
			}
		}
	}
	lastLine := func(view string) string { lines := strings.Split(view, "\n"); return lines[len(lines)-1] }
	sess := &session{}

	activity := newActivity(sess, nil)
	activity.items = []api.ActivityItem{
		{Download: api.Download{EpisodeID: 1, State: api.Downloading}},
		{Download: api.Download{EpisodeID: 2, State: api.Failed}},
		{Download: api.Download{EpisodeID: 3, State: api.Completed, Import: api.Import{State: api.ImportDone}}},
	}
	for _, id := range []int64{1, 2, 3} {
		activity.selected = id
		check(fmt.Sprintf("Activity, row %d", id), activity, activity.bottom(), "enter", "x", "c")
	}

	data := api.SeriesData{
		Series: api.Series{ID: 42, Name: "Harbor Precinct", DefaultOrder: "official", Type: api.TypeTV},
		Episodes: []api.Episode{
			{ID: 101, Aired: "2026-01-01", Numbers: map[string]api.Position{"official": {Season: 1, Number: 1}}},
			{ID: 102, Aired: "2026-01-08", Numbers: map[string]api.Position{"official": {Season: 1, Number: 2}}},
			{ID: 103, Aired: "2026-01-15", Numbers: map[string]api.Position{"official": {Season: 1, Number: 3}}},
			{ID: 104, Aired: "2099-01-01", Numbers: map[string]api.Position{"official": {Season: 1, Number: 4}}},
		},
	}
	show := newShow(sess, nil, 42, "Harbor Precinct", "")
	show.setDetail(api.SeriesDetail{SeriesData: data, States: []api.EpisodeStatus{
		{EpisodeID: 101, State: api.EpisodeImported}, {EpisodeID: 102, State: api.EpisodeNotSearched},
		{EpisodeID: 103, State: api.EpisodeWaiting, Summary: "No matching release found"}, {EpisodeID: 104, State: api.EpisodeUpcoming},
	}})
	for _, id := range []int64{101, 102, 103, 104} {
		show.cursor = rowKey{season: 1, episode: id}
		check(fmt.Sprintf("Show, episode %d", id), show, show.bottom(), "enter", "s")
	}
	show.cursor = rowKey{season: 1}
	check("Show, season row", show, show.bottom(), "s")

	episode := newEpisode(sess, show, 101)
	episode.detail = &api.EpisodeDetail{}
	episode.id = 102 // never searched
	check("Episode, never searched", episode, episode.bottom(), "x", "enter", "s", "r", "c")
	episode.id = 103 // searched, nothing downloaded
	check("Episode, searched", episode, episode.bottom(), "x", "enter", "s", "r", "c")
	episode.id = 999 // not in the show as loaded
	check("Episode, show without it", episode, episode.bottom(), "x", "enter", "s", "r", "c")
	episode.id = 101
	for name, downloads := range map[string][]api.Download{
		"never downloaded": nil,
		"downloading":      {{State: api.Downloading}},
		"download failed":  {{State: api.Failed}},
		"import failed":    {{State: api.Completed, Import: api.Import{State: api.ImportFailed}}},
		"needs you":        {{State: api.Completed, Import: api.Import{State: api.ImportUnverified}}},
		"importing":        {{State: api.Completed, Import: api.Import{State: api.ImportPending}}},
		"imported":         {{State: api.Completed, Import: api.Import{State: api.ImportDone}}},
	} {
		episode.detail = &api.EpisodeDetail{Downloads: downloads}
		check("Episode, "+name, episode, episode.bottom(), "x", "enter", "s", "r", "c")
	}

	library := newLibrary(sess)
	library.loaded = true
	check("Library, empty", library, library.bottom(120, 0, 0), "enter", "a", "i")
	// The show tabs sort with o; s, which searches, does nothing there.
	library.sess = &session{shows: []api.Show{{Series: api.Series{ID: 42, Name: "Harbor Precinct", Type: api.TypeTV}}}}
	library.selected = 42
	check("Library", library, library.bottom(120, 1, 1), "enter", "i", "s")
	// o is in Help, not the key bar, as d is; the palette offers it.
	if !slices.ContainsFunc(screenActions(library), func(e paletteEntry) bool { return e.key == "o" && e.label == "Sort by the next column" }) {
		t.Errorf("the Library's palette lacks o, Sort by the next column: %v", screenActions(library))
	}
	// With no show under the cursor, the keys that act on one grey out
	// and the palette leaves them out.
	library.filter = "zzz"
	check("Library, filter matches nothing", library, library.bottom(120, 0, 1), "enter", "i", "d")
	library.filter, library.cat = "", catAnime
	check("Library, empty category", library, library.bottom(120, 0, 0), "enter", "i", "d")
	library.cat, library.sess = catAll, sess
	library.cat = catUpcoming
	library.upcoming = &api.Upcoming{Today: "2026-11-22"}
	check("Upcoming, empty", library, library.bottom(120, 0, 0), "enter", "s", "i")
	library.upcoming.Episodes = []api.UpcomingEpisode{
		{EpisodeID: 1, SeriesID: 7, SeriesName: "Harbor Precinct", Season: 1, Number: 1, Aired: "2026-11-21", State: api.EpisodeWaiting},
		{EpisodeID: 2, SeriesID: 7, SeriesName: "Harbor Precinct", Season: 1, Number: 2, Aired: "2026-11-26", State: api.EpisodeUpcoming},
	}
	for _, id := range []int64{1, 2} {
		library.upSelected = id
		check(fmt.Sprintf("Upcoming, episode %d", id), library, library.bottom(120, 0, 0), "enter", "s", "i")
	}

	releases := &releasesScreen{sess: sess, back: show, search: &api.Search{Candidates: []api.Candidate{
		{ID: 1, Rank: 1, Title: "Harbor.Precinct.S01E01.German.DL.1080p.WEB-GRP", Verdict: api.Accept, Tier: "German 1080p"},
	}}, selected: 1}
	check("Releases", releases, releases.bottom(), "enter", "s", "r")
	releases.searching = true
	check("Releases, searching", releases, releases.bottom(), "enter", "s", "r")
	releases.searching, releases.selected = false, 0
	releases.search = &api.Search{Summary: "No matching release found"}
	check("Releases, nothing found", releases, releases.bottom(), "enter", "s")

	noEpisodes := newShow(sess, nil, 43, "Glass Orchard", "")
	noEpisodes.setDetail(api.SeriesDetail{SeriesData: api.SeriesData{Series: api.Series{ID: 43, Name: "Glass Orchard", DefaultOrder: "official"}}})
	check("Show, no episodes", noEpisodes, noEpisodes.bottom(), "enter", "s")

	activity.items = []api.ActivityItem{}
	check("Activity, empty", activity, activity.bottom(), "enter", "x", "c")

	// Profiles edits with enter or e; the palette runs the first.
	profiles := newProfiles(sess, nil)
	profiles.set(nil)
	check("Profiles, none", profiles, lastLine(profiles.view(120, 36)), "d", "x", "u", "e")
	if slices.ContainsFunc(screenActions(profiles), func(e paletteEntry) bool { return e.key == "enter" }) {
		t.Error("the palette offers to edit a profile while none is listed")
	}
	profiles.set([]api.Profile{{ID: 1, Name: "German", Text: profile.Encode(german)}})
	check("Profiles", profiles, lastLine(profiles.view(120, 36)), "d", "x", "u")
	if !slices.ContainsFunc(screenActions(profiles), func(e paletteEntry) bool { return e.key == "enter" }) {
		t.Error("the palette does not offer to edit the profile under the cursor")
	}

	preview := &previewScreen{sess: sess}
	check("Add show, loading", preview, preview.bottom(), "a", "o")
	preview.preview = &api.Preview{}
	check("Add show", preview, preview.bottom(), "a", "o")
	preview.preview.Added = true
	check("Add show, in the library", preview, preview.bottom(), "a", "o")

	_, cl := newRecorder(t)
	_, editor := editorApp(cl, 1)
	editor.shows = 2
	check("Profile editor", editor, lastLine(editor.view(120, 36)), "t")
	editor.p.Description += " and more"
	check("Profile editor, changed", editor, lastLine(editor.view(120, 36)), "t")
}
