package tui

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sudokvn/garner/internal/api"
	"github.com/sudokvn/garner/internal/client"
)

// n opens the Episode screen of the next episode that needs the user, on
// any screen that is not capturing keys: failed downloads and imports not
// cleared first, then files Jellyfin did not confirm. n again walks on and
// wraps around; the bottom line says where in the queue the user is.

// needItem is an episode that needs the user.
type needItem struct {
	episode, series int64
	show            string
}

// needsQueue orders what needs the user: failures first, then files
// Jellyfin did not confirm, the newest first in each, as the activity
// lists them. The library adds what the activity no longer lists, such as
// a failure older than a day; an episode the activity lists is decided by
// it, as the library may be older.
func needsQueue(items []api.ActivityItem, shows []api.Show) []needItem {
	var failed, unconfirmed []needItem
	seen := map[int64]bool{}
	add := func(state api.EpisodeState, it needItem) {
		if seen[it.episode] {
			return
		}
		seen[it.episode] = true
		switch state {
		case api.EpisodeFailed:
			failed = append(failed, it)
		case api.EpisodeAttention:
			unconfirmed = append(unconfirmed, it)
		}
	}
	for _, it := range items {
		d := it.Download
		state := d.EpisodeState()
		if state == api.EpisodeFailed && !d.Acknowledged.IsZero() {
			state = "" // cleared
		}
		add(state, needItem{d.EpisodeID, it.SeriesID, itemShow(it)})
	}
	for _, sh := range shows {
		// The library counts a cleared failure by what else the episode
		// has, so a failed focus was not cleared.
		if f := sh.Focus; f != nil {
			add(f.State, needItem{f.EpisodeID, sh.ID, showName(sh)})
		}
	}
	return append(failed, unconfirmed...)
}

// needsState is the queue n walks.
type needsState struct {
	queue  []needItem
	at     int
	screen screen // the Episode screen n opened last
	origin screen // where the walk began; esc on the episode goes back there
	asked  screen // the screen n was pressed on, while the activity loads
	asks   int    // counts lookups, so only the latest answer opens anything
	// loading is whether a lookup runs; n pressed meanwhile waits for its
	// answer instead of asking again.
	loading bool
}

// needsMsg answers n with the activity.
type needsMsg struct {
	sess  *session
	ask   int // which lookup it answers
	items []api.ActivityItem
	err   error
	// shows is the library as it is now, unless showsErr. The one the
	// app holds is not used in its place: it may still list a failure
	// cleared a moment ago.
	shows    []api.Show
	showsErr error
}

// nextNeeds loads the activity to find what needs the user.
func (a *app) nextNeeds() tea.Cmd {
	a.needs.asked = a.screen
	if a.needs.loading {
		return nil // the lookup that runs answers this press
	}
	a.needs.loading = true
	a.needs.asks++
	sess, ask := a.sess, a.needs.asks
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		msg := needsMsg{sess: sess, ask: ask}
		done := make(chan struct{})
		go func() {
			defer close(done)
			msg.shows, msg.showsErr = sess.client.Library(ctx)
		}()
		msg.items, msg.err = sess.client.Activity(ctx)
		<-done
		return msg
	}
}

// showNeeds opens the next episode in the queue: the first, or the one
// after the episode n opened last when the user is still on it.
func (a *app) showNeeds(msg needsMsg) tea.Cmd {
	if msg.sess == a.sess && msg.ask == a.needs.asks {
		a.needs.loading = false
	}
	c, capturing := a.screen.(capturer)
	if msg.sess != a.sess || msg.ask != a.needs.asks || a.screen != a.needs.asked ||
		a.help || capturing && c.captures() {
		// The user moved on, started typing or answering a question, or
		// the session ended.
		return nil
	}
	a.needs.asked = nil
	f, _ := a.screen.(flasher)
	switch {
	case errors.Is(msg.err, client.ErrUnauthorized):
		return func() tea.Msg { return disconnectedMsg{errRevoked} }
	case msg.err != nil:
		if f != nil {
			f.flashed().stay(gFailed + " " + sRed.Render("Could not load what needs you: "+clean(msg.err.Error())))
		}
		return nil
	}
	queue := needsQueue(msg.items, msg.shows)
	if len(queue) == 0 {
		if msg.showsErr != nil {
			// Older failures may still need the user.
			if f != nil {
				f.flashed().stay(gFailed + " " + sRed.Render("Could not load what needs you: "+clean(msg.showsErr.Error())))
			}
			return nil
		}
		a.needs = needsState{asks: a.needs.asks}
		if f != nil {
			f.flashed().info(gDone + " " + sNormal.Render("Nothing needs you"))
		}
		return nil
	}
	at := 0
	if walking := a.needs.screen != nil && a.screen == a.needs.screen; walking {
		current := a.needs.queue[a.needs.at].episode
		if i := slices.IndexFunc(queue, func(it needItem) bool { return it.episode == current }); i >= 0 {
			at = (i + 1) % len(queue)
		} else {
			// The episode needs nothing any more: the next one of the old
			// queue still in it takes its place, or else the first.
			old := a.needs.queue
			for k := 1; k < len(old); k++ {
				next := old[(a.needs.at+k)%len(old)].episode
				if i := slices.IndexFunc(queue, func(it needItem) bool { return it.episode == next }); i >= 0 {
					at = i
					break
				}
			}
		}
	} else {
		a.needs.origin = a.screen
	}
	it := queue[at]
	// The Show behind the episode leads back where the walk began too,
	// such as a profile editor with unsaved changes.
	show := newShow(a.sess, a.needs.origin, it.series, it.show, "")
	next := newEpisode(a.sess, show, it.episode)
	next.back = a.needs.origin
	a.needs.queue, a.needs.at, a.needs.screen = queue, at, next
	a.screen = next
	return next.init()
}

// withNeeds adds where the user is in the queue to the bottom line of an
// Episode screen n opened, at the right; the screen's keys give way to it
// by their importance.
func (a *app) withNeeds(page string) string {
	if a.needs.screen == nil || a.screen != a.needs.screen || len(a.needs.queue) == 0 {
		return page
	}
	label := sKey.Render("n") + " " + sMuted.Render("needs you · "+strconv.Itoa(a.needs.at+1)+" of "+strconv.Itoa(len(a.needs.queue)))
	lines := strings.Split(page, "\n")
	last := len(lines) - 1
	room := a.width - 1 - lipgloss.Width(label) - 1 - len(keyGap)
	if room < 10 {
		return page
	}
	bar := fitKeys(strings.TrimPrefix(lines[last], " "), room)
	gap := a.width - 1 - lipgloss.Width(bar) - lipgloss.Width(label) - 1
	lines[last] = " " + bar + strings.Repeat(" ", max(len(keyGap), gap)) + label
	return strings.Join(lines, "\n")
}
