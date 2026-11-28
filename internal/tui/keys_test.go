package tui

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sudokvn/garner/internal/api"
	"github.com/sudokvn/garner/internal/profile"
)

// cursorList is a list with a cursor, opened for TestListKeys.
type cursorList struct {
	press func(tea.KeyPressMsg)
	draw  func() // sets the page, as the program draws after each key
	at    func() int
	rows  int
	page  func() int // rows the list shows at a time
}

// inApp is a list on the app's current screen, drawn at 120 by 36 unless
// the app has a size already.
func inApp(a *app, at func() int, rows int, page func() int) cursorList {
	if a.width == 0 {
		a.width, a.height = 120, 36
	}
	return cursorList{
		press: func(m tea.KeyPressMsg) { a.update(m) },
		draw:  func() { a.View() },
		at:    at, rows: rows, page: page,
	}
}

// listKeyPress is the key press whose String is key.
func listKeyPress(t *testing.T, key string) tea.KeyPressMsg {
	t.Helper()
	m := tea.KeyPressMsg{Code: []rune(key)[0], Text: key}
	switch key {
	case "up":
		m = tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		m = tea.KeyPressMsg{Code: tea.KeyDown}
	case "home":
		m = tea.KeyPressMsg{Code: tea.KeyHome}
	case "end":
		m = tea.KeyPressMsg{Code: tea.KeyEnd}
	case "pgup":
		m = tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		m = tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "ctrl+d", "ctrl+u":
		m = tea.KeyPressMsg{Code: rune(key[5]), Mod: tea.ModCtrl}
	}
	if m.String() != key {
		t.Fatalf("the key press for %q reads %q", key, m.String())
	}
	return m
}

// Fixtures for TestListKeys: lists longer than two pages.
func manyShows() []api.Show {
	var shows []api.Show
	for i := range 80 {
		typ := api.TypeTV
		if i%2 == 1 {
			typ = api.TypeAnime
		}
		shows = append(shows, api.Show{Series: api.Series{ID: int64(i + 1), Name: fmt.Sprintf("Harbor Precinct %02d", i+1), Type: typ}})
	}
	return shows
}

func manyEpisodes(id int64, name string) api.SeriesDetail {
	data := api.SeriesData{Series: api.Series{ID: id, Name: name, DefaultOrder: "official", Type: api.TypeTV}}
	for n := 1; n <= 60; n++ {
		data.Episodes = append(data.Episodes, api.Episode{ID: int64(100 + n), Aired: "2026-01-01",
			Numbers: map[string]api.Position{"official": {Season: 1, Number: n}}})
	}
	return api.SeriesDetail{SeriesData: data}
}

func libraryList(t *testing.T, cat category) (*app, *libraryScreen) {
	t.Helper()
	sess := &session{shows: manyShows()}
	a := &app{sess: sess, library: newLibrary(sess)}
	a.screen = a.library
	a.library.loaded, a.library.cat = true, cat
	return a, a.library
}

func libraryTab(cat category) func(t *testing.T) cursorList {
	return func(t *testing.T) cursorList {
		a, s := libraryList(t, cat)
		rows, _ := s.rows()
		return inApp(a, func() int {
			rows, _ := s.rows()
			return slices.IndexFunc(rows, func(sh api.Show) bool { return sh.ID == s.selected })
		}, len(rows), func() int { return s.page })
	}
}

// The keys a list moves with.
var (
	allListKeys = []string{"j", "k", "down", "up", "g", "G", "home", "end", "ctrl+d", "ctrl+u", "pgdown", "pgup"}
	// While text is typed, letters are text.
	typedListKeys = []string{"down", "up", "home", "end", "pgdown", "pgup"}
)

// TestListKeys walks every list with a cursor and checks that the keys
// move it the same way everywhere: j k and the arrows a row, gg and home to
// the top, a single g nowhere, G and end to the bottom, ctrl+d u and
// pgdown pgup half the rows the list shows. A screen with a list that is
// not walked here fails the test.
func TestListKeys(t *testing.T) {
	lists := []struct {
		name   string
		screen string   // the screen's type
		keys   []string // the keys that move it; all when nil
		open   func(t *testing.T) cursorList
	}{
		{name: "Library, All", screen: "libraryScreen", open: libraryTab(catAll)},
		{name: "Library, TV", screen: "libraryScreen", open: libraryTab(catTV)},
		{name: "Library, Anime", screen: "libraryScreen", open: libraryTab(catAnime)},
		{name: "Library, Upcoming", screen: "libraryScreen", open: func(t *testing.T) cursorList {
			a, s := libraryList(t, catUpcoming)
			s.upcoming = &api.Upcoming{Today: "2026-11-22"}
			for i := range 60 {
				s.upcoming.Episodes = append(s.upcoming.Episodes, api.UpcomingEpisode{EpisodeID: int64(i + 1), SeriesID: 1,
					SeriesName: "Harbor Precinct 01", Season: 1, Number: i + 1, Aired: "2026-11-22", State: api.EpisodeUpcoming})
			}
			return inApp(a, func() int {
				rows := upcomingRows(s.upcoming)
				at := s.upCursor(rows)
				eps := 0
				for _, r := range rows[:at] {
					if r.ep != nil {
						eps++
					}
				}
				return eps
			}, 60, func() int { return s.page })
		}},
		{name: "Library, filter", screen: "libraryScreen", keys: typedListKeys, open: func(t *testing.T) cursorList {
			l := libraryTab(catAll)(t)
			l.press(listKeyPress(t, "/"))
			return l
		}},
		{name: "Show", screen: "showScreen", open: func(t *testing.T) cursorList {
			sess := &session{}
			s := newShow(sess, nil, 1, "Harbor Precinct", "")
			s.setDetail(manyEpisodes(1, "Harbor Precinct"))
			a := &app{sess: sess, screen: s}
			return inApp(a, func() int { return slices.Index(s.rows(), s.cursor) }, len(s.rows()), func() int { return s.page })
		}},
		{name: "Releases", screen: "releasesScreen", open: func(t *testing.T) cursorList {
			sess := &session{}
			s := &releasesScreen{sess: sess, back: &showScreen{name: "Harbor Precinct"}, search: &api.Search{}}
			for i := range 60 {
				s.search.Candidates = append(s.search.Candidates, api.Candidate{ID: int64(i + 1), Rank: i + 1, Indexer: "One",
					Title: fmt.Sprintf("Harbor.Precinct.S01E01.German.DL.1080p.WEB-GRP%02d", i), Size: 725e6, Verdict: api.Accept, Tier: "German 1080p"})
			}
			s.selected = 1
			a := &app{sess: sess, screen: s}
			return inApp(a, func() int {
				return slices.IndexFunc(s.candidates(), func(c api.Candidate) bool { return c.ID == s.selected })
			}, 60, func() int { return s.page })
		}},
		{name: "Activity", screen: "activityScreen", open: func(t *testing.T) cursorList {
			sess := &session{}
			s := newActivity(sess, nil)
			var items []api.ActivityItem
			now := time.Now()
			for i := range 60 {
				at := now.Add(-time.Duration(i) * time.Minute)
				items = append(items, api.ActivityItem{Download: api.Download{ID: int64(i + 1), EpisodeID: int64(i + 1), State: api.Completed,
					Import: api.Import{State: api.ImportDone}, Created: at, Updated: at},
					SeriesID: 1, SeriesName: "Harbor Precinct", Season: 1, Number: i + 1})
			}
			s.update(activityMsg{from: s, items: items})
			a := &app{sess: sess, screen: s}
			return inApp(a, func() int {
				return slices.IndexFunc(s.rows(), func(it api.ActivityItem) bool { return it.Download.EpisodeID == s.selected })
			}, 60, func() int { return s.page })
		}},
		{name: "Profiles", screen: "profilesScreen", open: func(t *testing.T) cursorList {
			sess := &session{}
			s := newProfiles(sess, nil)
			var stored []api.Profile
			for i := range 60 {
				p := german
				p.Name = fmt.Sprintf("German %02d", i+1)
				stored = append(stored, api.Profile{ID: int64(i + 1), Name: p.Name, Text: profile.Encode(p)})
			}
			s.set(stored)
			a := &app{sess: sess, screen: s}
			return inApp(a, func() int {
				return slices.IndexFunc(s.rows, func(r profileRow) bool { return r.ID == s.selected })
			}, 60, func() int { return s.page })
		}},
		{name: "Profiles, use for new shows", screen: "profilesScreen", open: func(t *testing.T) cursorList {
			_, cl := newRecorder(t)
			a, p := profilesApp(cl, 1)
			sendKeys(a, "u")
			return inApp(a, func() int { return p.choice }, 3, func() int { return 3 })
		}},
		// ctrl+d u and pgdown pgup scroll the page to the unsaved changes,
		// which may reach below a short terminal.
		{name: "Profile editor", screen: "editorScreen", keys: []string{"j", "k", "down", "up", "g", "G", "home", "end"},
			open: func(t *testing.T) cursorList {
				_, cl := newRecorder(t)
				a, ed := editorApp(cl, 1)
				return inApp(a, func() int { return ed.cursor }, len(ed.rows()), func() int { return len(ed.rows()) })
			}},
		{name: "Show details, profile and type", screen: "showDetailsScreen", open: func(t *testing.T) cursorList {
			sess := &session{}
			show := newShow(sess, nil, 1, "Harbor Precinct", "")
			d := manyEpisodes(1, "Harbor Precinct")
			for i := range 10 {
				d.Profiles = append(d.Profiles, api.ProfileInfo{ID: int64(i + 1), Name: fmt.Sprintf("German %02d", i+1)})
			}
			show.setDetail(d)
			det := newShowDetails(sess, show, nil)
			a := &app{sess: sess, screen: det}
			sendKeys(a, "p")
			return inApp(a, func() int { return det.choice }, 12, func() int { return 12 })
		}},
		{name: "Show details, monitor", screen: "showDetailsScreen", open: func(t *testing.T) cursorList {
			sess := &session{}
			show := newShow(sess, nil, 1, "Harbor Precinct", "")
			show.setDetail(manyEpisodes(1, "Harbor Precinct"))
			det := newShowDetails(sess, show, nil)
			a := &app{sess: sess, screen: det}
			sendKeys(a, "m")
			return inApp(a, func() int { return det.choice }, len(monitorModes), func() int { return len(monitorModes) })
		}},
		// Home and end move in the search field.
		{name: "Add show", screen: "addScreen", keys: []string{"down", "up", "pgdown", "pgup"}, open: func(t *testing.T) cursorList {
			sess := &session{}
			s := newAddShow(sess, nil)
			s.input.SetValue("harbor")
			s.shown = "harbor"
			for i := range 60 {
				s.found.Results = append(s.found.Results, api.FoundShow{ID: int64(i + 1), Name: fmt.Sprintf("Harbor Precinct %02d", i+1)})
			}
			s.selected = 1
			a := &app{sess: sess, screen: s}
			return inApp(a, func() int {
				return slices.IndexFunc(s.found.Results, func(r api.FoundShow) bool { return r.ID == s.selected })
			}, 60, func() int { return s.rowsShown })
		}},
		{name: "Command palette", screen: "libraryScreen", keys: typedListKeys, open: func(t *testing.T) cursorList {
			a, _ := libraryList(t, catAll)
			// Drawn first, as the program does: the cursor's show then has
			// its actions in the palette.
			a.width, a.height = 120, 36
			a.View()
			a.palette = &palette{}
			return inApp(a, func() int { return a.palette.choice }, len(a.paletteEntries()), func() int { return a.palette.page })
		}},
		{name: "Pick list", open: func(t *testing.T) cursorList {
			p := &pickList{width: 40, labelWidth: 20}
			for i := range 60 {
				p.items = append(p.items, pickItem{label: fmt.Sprintf("German %02d", i+1)})
			}
			return cursorList{
				press: func(m tea.KeyPressMsg) { p.key(m.String()) },
				draw:  func() { p.lines("Pick", 36) },
				at:    func() int { return p.choice },
				rows:  60, page: func() int { return p.page },
			}
		}},
	}

	// What the keys should do, from where the cursor is.
	script := []string{"G", "g", "g", "end", "home", "j", "down", "down", "k", "up",
		"ctrl+d", "pgdown", "ctrl+u", "pgup", "end", "pgup", "ctrl+u", "pgdown", "ctrl+d",
		"G", "g", "j", "g", "home", "end", "k", "up", "g", "g"}
	walked := map[string]bool{}
	for _, l := range lists {
		walked[l.screen] = true
		keys := l.keys
		if keys == nil {
			keys = allListKeys
		}
		list := l.open(t)
		list.draw()
		at, prev := list.at(), ""
		var pressed []string
		for _, key := range script {
			if !slices.Contains(keys, key) {
				continue
			}
			half := max(1, list.page()/2)
			// A g after gg starts a new gg.
			top := key == "g" && prev == "g"
			prev = key
			if top {
				prev = ""
			}
			switch key {
			case "j", "down":
				at++
			case "k", "up":
				at--
			case "ctrl+d", "pgdown":
				at += half
			case "ctrl+u", "pgup":
				at -= half
			case "G", "end":
				at = list.rows - 1
			case "home":
				at = 0
			case "g":
				if top {
					at = 0
				}
			}
			at = min(max(0, at), list.rows-1)
			list.press(listKeyPress(t, key))
			list.draw()
			pressed = append(pressed, key)
			if got := list.at(); got != at {
				t.Errorf("%s: after %s the cursor is on row %d of %d, want %d (half a page is %d)",
					l.name, strings.Join(pressed, " "), got, list.rows, at, half)
				break
			}
		}
	}

	// Every screen is walked or has no list.
	withoutList := map[string]string{
		"aboutScreen":         "scrolls; its source lookup is a pick list",
		"connectScreen":       "takes the connection code",
		"episodeScreen":       "scrolls",
		"importScreen":        "takes a profile's text",
		"importPreviewScreen": "scrolls",
		"previewScreen":       "scrolls; Add show's monitor question is the one on Show details",
	}
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != "update" {
				continue
			}
			star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
			if !ok {
				continue
			}
			typ := star.X.(*ast.Ident).Name
			if _, ok := withoutList[typ]; !ok && !walked[typ] && typ != "app" {
				t.Errorf("%s (%s) is neither walked nor known to have no list", typ, name)
			}
		}
	}
}

// TestOneJobPerLetter guards the rule that a letter means one action
// across the program: on every screen whose help() lists s, s searches
// for releases, and r retries or refreshes the thing in front of the
// user. The palette's label for the key, which may depend on the row,
// must say the same. A screen with a help() not checked here fails.
func TestOneJobPerLetter(t *testing.T) {
	jobs := map[string][]string{
		"s": {"search"},
		"r": {"retry", "refresh"},
	}
	show := newShow(&session{}, nil, 1, "Harbor Precinct", "")
	show.setDetail(manyEpisodes(1, "Harbor Precinct"))
	// The Show screen's s says what it searches on the row: a season or
	// an episode.
	onEpisode := newShow(&session{}, nil, 1, "Harbor Precinct", "")
	onEpisode.setDetail(manyEpisodes(1, "Harbor Precinct"))
	onEpisode.cursor = rowKey{season: 1, episode: 101}
	upcoming := newLibrary(&session{})
	upcoming.cat = catUpcoming
	screens := map[string][]screen{
		"libraryScreen":       {newLibrary(&session{}), upcoming},
		"showScreen":          {show, onEpisode},
		"episodeScreen":       {newEpisode(&session{}, show, 101)},
		"releasesScreen":      {&releasesScreen{back: show}},
		"activityScreen":      {newActivity(&session{}, nil)},
		"profilesScreen":      {newProfiles(&session{}, nil)},
		"editorScreen":        {&editorScreen{}},
		"importPreviewScreen": {&importPreviewScreen{}},
		"aboutScreen":         {&aboutScreen{}},
		"showDetailsScreen":   {newShowDetails(&session{}, show, nil)},
		"previewScreen":       {&previewScreen{}},
	}
	about := func(label string, words []string) bool {
		return slices.ContainsFunc(words, func(w string) bool { return strings.Contains(strings.ToLower(label), w) })
	}
	for typ, list := range screens {
		for _, s := range list {
			name, pairs := s.(keyHelper).help()
			act, _ := s.(actioner)
			for i := 0; i+1 < len(pairs); i += 2 {
				for _, key := range strings.Fields(pairs[i]) {
					words, ruled := jobs[key]
					if !ruled {
						continue
					}
					if !about(pairs[i+1], words) {
						t.Errorf("%s (%s): %s is %q, not about %s", name, typ, key, pairs[i+1], strings.Join(words, " or "))
					}
					if act == nil {
						continue
					}
					if label, _ := act.action(key, pairs[i+1]); !about(label, words) {
						t.Errorf("%s (%s): the palette calls %s %q, not about %s", name, typ, key, label, strings.Join(words, " or "))
					}
				}
			}
		}
	}

	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != "help" {
				continue
			}
			if star, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok {
				if typ := star.X.(*ast.Ident).Name; screens[typ] == nil {
					t.Errorf("%s (%s) lists keys in help() but is not checked here", typ, file)
				}
			}
		}
	}
}
