package tui

import (
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/trucke/garner/internal/api"
)

// TestLateAnswers checks that an answer reaches only the screen that asked
// for it: a details page left early must not fill the next one, and a
// library from an ended session must not replace the current one.
func TestLateAnswers(t *testing.T) {
	sess := &session{}
	left := newPreview(sess, nil, api.FoundShow{ID: 1, Name: "First"})
	next := newPreview(sess, nil, api.FoundShow{ID: 2, Name: "Second"})
	next.update(previewMsg{from: left, preview: api.Preview{FoundShow: api.FoundShow{ID: 1}}})
	if next.preview != nil {
		t.Fatal("the details of a page that was left showed on the next one")
	}

	old := newLibrary(sess)
	a := &app{sess: sess, library: newLibrary(sess)}
	a.screen = a.library
	a.library.loading = true
	a.Update(libraryMsg{from: old, shows: []api.Show{{Series: api.Series{ID: 9}}}})
	if !a.library.loading || len(sess.shows) != 0 {
		t.Fatal("a library answer from an ended session replaced the current library")
	}
	a.Update(libraryMsg{from: a.library, shows: []api.Show{{Series: api.Series{ID: 7}}}})
	if a.library.loading || len(sess.shows) != 1 {
		t.Fatal("the current library's own answer was not applied")
	}
}

// TestInitialFolding checks which season opens first: the one with the
// most urgent episode, the newest when nothing needs anything, and a
// single season always.
func TestInitialFolding(t *testing.T) {
	detail := func(states map[int64]api.EpisodeState, positions map[int64][2]int) api.SeriesDetail {
		d := api.SeriesDetail{SeriesData: api.SeriesData{Series: api.Series{ID: 1, DefaultOrder: "official"}}}
		for id, pos := range positions {
			d.Episodes = append(d.Episodes, api.Episode{ID: id, Numbers: map[string]api.Position{"official": {Season: pos[0], Number: pos[1]}}})
			d.States = append(d.States, api.EpisodeStatus{EpisodeID: id, State: states[id]})
		}
		return d
	}
	positions := map[int64][2]int{31: {3, 1}, 21: {2, 1}, 22: {2, 2}, 11: {1, 1}}
	cases := []struct {
		name   string
		states map[int64]api.EpisodeState
		open   int
		cursor rowKey
	}{
		{"most urgent", map[int64]api.EpisodeState{31: api.EpisodeImported, 21: api.EpisodeImported, 22: api.EpisodeFailed, 11: api.EpisodeNotSearched}, 2, rowKey{2, 22}},
		{"nothing needed", map[int64]api.EpisodeState{31: api.EpisodeUpcoming, 21: api.EpisodeImported, 22: api.EpisodeImported, 11: api.EpisodeImported}, 3, rowKey{season: 3}},
	}
	for _, c := range cases {
		s := newShow(&session{}, nil, 1, "Show", "")
		s.setDetail(detail(c.states, positions))
		folded := s.sess.folded[1]
		if folded[c.open] || !folded[1] && c.open != 1 || s.cursor != c.cursor {
			t.Errorf("%s: folded %v, cursor %v; want season %d open, cursor %v", c.name, folded, s.cursor, c.open, c.cursor)
		}
	}
	single := newShow(&session{}, nil, 1, "Show", "")
	single.setDetail(detail(map[int64]api.EpisodeState{11: api.EpisodeImported}, map[int64][2]int{11: {1, 1}}))
	if single.sess.folded[1][1] {
		t.Error("a show's only season started folded")
	}
}

// TestPipelineFailures checks that each kind of failure marks the step it
// happened in, and the steps after it stay pending.
func TestPipelineFailures(t *testing.T) {
	checked := []api.ImportFile{{Source: "a.mkv", Check: &api.FileCheck{Audio: []string{"German"}}}}
	planned := []api.ImportFile{{Source: "a.mkv", Target: "/lib/A.mkv", Check: &api.FileCheck{Audio: []string{"German"}}}}
	for _, tc := range []struct {
		name string
		d    api.Download
		want int // the failed step
	}{
		{"SABnzbd refused it", api.Download{State: api.Failed, Failure: "refused"}, 0},
		{"missing articles", api.Download{State: api.Failed, JobID: "j", Failure: "cannot be completed"}, 1},
		{"file check rejected it", api.Download{State: api.Failed, JobID: "j", Failure: "file check: audio is Japanese",
			Import: api.Import{State: api.ImportRejected, Files: checked}}, 3},
		{"share not mounted", api.Download{State: api.Completed, JobID: "j",
			Import: api.Import{State: api.ImportFailed, Detail: "garner cannot see it"}}, 3},
		{"move failed", api.Download{State: api.Completed, JobID: "j",
			Import: api.Import{State: api.ImportFailed, Detail: "the library folder is missing", Files: planned}}, 4},
	} {
		steps := pipeline(tc.d, time.Now())
		for i, st := range steps {
			switch {
			case i == tc.want && st.glyph != gFailed:
				t.Errorf("%s: step %d (%s) is not marked failed", tc.name, i, st.name)
			case i > tc.want && st.glyph != gWaiting:
				t.Errorf("%s: step %d (%s) after the failure is not pending", tc.name, i, st.name)
			}
		}
	}
	if st := pipeline(api.Download{State: api.Failed, JobID: "j", Failure: "file check: audio is Japanese",
		Import: api.Import{State: api.ImportRejected}}, time.Now())[3]; st.detail[0].text != "audio is Japanese" {
		t.Errorf("rejected detail = %q", st.detail[0].text)
	}
}

// TestKeysWhileTyping sends ? and A to a filter being typed; otherwise they
// open Help and the Activity.
func TestKeysWhileTyping(t *testing.T) {
	sess := &session{}
	a := &app{sess: sess, library: newLibrary(sess)}
	a.screen = a.library
	a.library.typing = true
	for _, k := range []rune{'?', 'A'} {
		a.update(tea.KeyPressMsg{Code: k, Text: string(k)})
	}
	if a.help || a.screen != screen(a.library) || a.library.filter != "?A" {
		t.Fatalf("while typing: help %v, filter %q", a.help, a.library.filter)
	}
	a.library.typing = false
	a.update(tea.KeyPressMsg{Code: '?', Text: "?"})
	if !a.help {
		t.Error("? did not open Help")
	}
	a.update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if a.help {
		t.Error("esc did not close Help")
	}
}

// TestActivityOrder lists what needs the user first, then what runs, then
// what ended; the newest first within each.
func TestActivityOrder(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 10, 16, 12, m, 0, 0, time.UTC) }
	item := func(ep int64, d api.Download) api.ActivityItem {
		d.EpisodeID = ep
		return api.ActivityItem{Download: d}
	}
	s := &activityScreen{items: []api.ActivityItem{
		item(1, api.Download{State: api.Completed, Updated: at(50), Import: api.Import{State: api.ImportDone, Updated: at(55)}}),
		item(2, api.Download{State: api.Downloading, Created: at(10)}),
		item(3, api.Download{State: api.Queued, Created: at(20)}),
		item(4, api.Download{State: api.Completed, Updated: at(1), Import: api.Import{State: api.ImportFailed, Updated: at(2)}}),
		item(5, api.Download{State: api.Cancelled, Updated: at(40)}),
	}}
	var got []int64
	for _, it := range s.rows() {
		got = append(got, it.Download.EpisodeID)
	}
	if want := []int64{4, 3, 2, 1, 5}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}
