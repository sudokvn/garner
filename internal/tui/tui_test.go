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
