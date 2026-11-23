package tui

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sudokvn/garner/internal/api"
	"github.com/sudokvn/garner/internal/client"
)

// Activity lists what runs, what waits for the user, and what ended within
// the last day: one row per episode, its latest download, with the
// attempts before it that the file check rejected hanging under it.

type activityMsg struct {
	from  *activityScreen
	items []api.ActivityItem
	err   error
}

// rejectionsMsg brings the rejection counts on their own, so a slow count
// does not hold back the list.
type rejectionsMsg struct {
	from       *activityScreen
	gen        int // the reload that asked
	rejections api.Rejections
	err        error
}

type activityScreen struct {
	sess       *session
	back       screen
	items      []api.ActivityItem
	rejections *api.Rejections
	// Counts reloads, so an older answer does not replace newer counts.
	rejGen int
	err    error
	// A reload asked for while one runs follows it.
	loading, dirty bool
	selected       int64 // episode ID
	offset         int
	page           int // lines on screen, from the last view
	keys           listKeys
	asking         bool // the cancel question
	choice         int
	busy           string
	flash          flash
}

func newActivity(sess *session, back screen) *activityScreen {
	return &activityScreen{sess: sess, back: back}
}

func (s *activityScreen) init() tea.Cmd { return s.reload() }

func (s *activityScreen) reload() tea.Cmd {
	if s.loading {
		s.dirty = true
		return nil
	}
	s.loading = true
	s.rejGen++
	cl, gen := s.sess.client, s.rejGen
	return tea.Batch(func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		items, err := cl.Activity(ctx)
		return activityMsg{from: s, items: items, err: err}
	}, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		r, err := cl.Rejections(ctx)
		return rejectionsMsg{from: s, gen: gen, rejections: r, err: err}
	})
}

func (s *activityScreen) update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case activityMsg:
		if msg.from != s {
			return s, nil
		}
		s.loading = false
		switch {
		case errors.Is(msg.err, client.ErrUnauthorized):
			return s, func() tea.Msg { return disconnectedMsg{errRevoked} }
		case msg.err != nil:
			s.err = msg.err
		default:
			s.err, s.items = nil, msg.items
			if rows := s.rows(); len(rows) > 0 && !slices.ContainsFunc(rows, func(it api.ActivityItem) bool { return it.Download.EpisodeID == s.selected }) {
				s.selected, s.offset = rows[0].Download.EpisodeID, 0
			}
		}
		var cmd tea.Cmd
		if s.dirty {
			s.dirty = false
			cmd = s.reload()
		}
		var waiting []*api.Download
		for i := range s.items {
			waiting = append(waiting, &s.items[i].Download)
		}
		return s, tea.Batch(cmd, redrawWhenSlow(waiting...))
	case rejectionsMsg:
		// Without the counts, the list still shows.
		if msg.from == s && msg.gen == s.rejGen && msg.err == nil {
			s.rejections = &msg.rejections
		}
	case cancelledMsg:
		s.busy = ""
		switch {
		case errors.Is(msg.err, client.ErrUnauthorized):
			return s, func() tea.Msg { return disconnectedMsg{errRevoked} }
		case msg.err != nil:
			s.flash.stay(gFailed + " " + sRed.Render("Could not cancel the download: "+clean(msg.err.Error())))
			return s, nil
		}
		s.flash.info(gDone + " " + sNormal.Render("Cancelled the download"))
		return s, s.reload()
	case acknowledgedMsg:
		s.busy = ""
		switch {
		case errors.Is(msg.err, client.ErrUnauthorized):
			return s, func() tea.Msg { return disconnectedMsg{errRevoked} }
		case msg.err != nil:
			s.flash.stay(gFailed + " " + sRed.Render("Could not clear the failure: "+clean(msg.err.Error())))
			return s, nil
		}
		s.flash.info(gDone + " " + sNormal.Render("Failure cleared"))
		return s, s.apply(msg.download)
	case eventMsg:
		switch msg.Type {
		case client.EventConnected, api.EventSeriesDeleted:
			return s, s.reload()
		case api.EventDownloadUpdated:
			var d api.Download
			if json.Unmarshal(msg.Data, &d) != nil {
				return s, s.reload()
			}
			return s, s.apply(d)
		}
	case tea.KeyPressMsg:
		if s.busy != "" {
			return s, nil
		}
		s.flash.clear()
		if s.asking {
			return s, s.answer(msg.String())
		}
		return s, s.key(msg.String())
	}
	return s, nil
}

// apply takes a changed download from an event: progress changes in
// place; anything else loads the list again, which may add or drop a row.
func (s *activityScreen) apply(d api.Download) tea.Cmd {
	i := slices.IndexFunc(s.items, func(it api.ActivityItem) bool { return it.Download.ID == d.ID })
	if i < 0 || s.items[i].Download.State != d.State || s.items[i].Download.Import.State != d.Import.State {
		return s.reload()
	}
	// An event does not number the download; it keeps its attempt.
	d.Attempt = s.items[i].Download.Attempt
	s.items[i].Download = d
	return nil
}

func (s *activityScreen) key(key string) tea.Cmd {
	if s.keys.key(key, s.page, s.move) {
		return nil
	}
	switch key {
	case "esc":
		return func() tea.Msg { return switchMsg{s.back} }
	case "enter":
		it, ok := s.cursor()
		if !ok {
			return nil
		}
		// The episode screen needs its show; esc on either comes back here.
		show := newShow(s.sess, s, it.SeriesID, itemShow(it), "")
		next := newEpisode(s.sess, show, it.Download.EpisodeID)
		next.back = s
		return func() tea.Msg { return switchMsg{next} }
	case "c":
		if it, ok := s.cursor(); ok && it.Download.CanAcknowledge() {
			s.busy = "Clearing the failure…"
			return acknowledgeFailure(s.sess.client, it.Download.ID)
		}
	case "x":
		if it, ok := s.cursor(); ok && cancellable(it.Download) {
			s.asking, s.choice = true, 0 // the safe choice: keep going
		}
	}
	return nil
}

// answer handles the cancel question.
func (s *activityScreen) answer(key string) tea.Cmd {
	switch key {
	case "esc":
		s.asking = false
	case "h", "left", "shift+tab":
		s.choice = 0
	case "l", "right", "tab":
		s.choice = 1
	case "enter":
		s.asking = false
		it, ok := s.cursor()
		switch {
		case s.choice != 1 || !ok:
			return nil
		case !cancellable(it.Download):
			s.flash.info(gWaiting + " " + sNormal.Render("Too late to cancel: SABnzbd has finished downloading it."))
			return nil
		}
		s.busy = "Cancelling the download…"
		cl, id := s.sess.client, it.Download.ID
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			d, err := cl.CancelDownload(ctx, id)
			return cancelledMsg{d, err}
		}
	}
	return nil
}

// cancellable reports whether x can stop the download: SABnzbd queues or
// downloads it.
func cancellable(d api.Download) bool {
	return d.State == api.Queued || d.State == api.Downloading
}

// The states of ended rows that are no episode state: a cancelled download
// and a failure the user cleared.
const (
	activityCancelled api.EpisodeState = "cancelled"
	activityCleared   api.EpisodeState = "cleared"
)

// activityRank orders the rows: what needs the user first, then what runs,
// furthest along first, then what ended.
var activityRank = map[api.EpisodeState]int{
	api.EpisodeFailed: 0, api.EpisodeAttention: 1, api.EpisodeImporting: 2, api.EpisodeProcessing: 3,
	api.EpisodeDownloading: 4, api.EpisodeImported: 5, activityCancelled: 5, activityCleared: 5,
}

// activityState is the state a row's download puts its episode in.
// Cancelled downloads and cleared failures are ended activity, not new
// download states. A cleared failure remains available on the episode
// screen.
func activityState(d api.Download) api.EpisodeState {
	state := d.EpisodeState()
	switch {
	case d.State == api.Cancelled:
		return activityCancelled
	case state == api.EpisodeFailed && !d.Acknowledged.IsZero():
		return activityCleared
	}
	return state
}

// rows are the items in display order: by state, then the newest first.
func (s *activityScreen) rows() []api.ActivityItem {
	rows := slices.Clone(s.items)
	slices.SortStableFunc(rows, func(a, b api.ActivityItem) int {
		return cmp.Or(cmp.Compare(activityRank[activityState(a.Download)], activityRank[activityState(b.Download)]),
			since(b.Download).Compare(since(a.Download)))
	})
	return rows
}

func (s *activityScreen) cursor() (api.ActivityItem, bool) {
	rows := s.rows()
	i := slices.IndexFunc(rows, func(it api.ActivityItem) bool { return it.Download.EpisodeID == s.selected })
	if i < 0 {
		return api.ActivityItem{}, false
	}
	return rows[i], true
}

func (s *activityScreen) move(n int) {
	rows := s.rows()
	if len(rows) == 0 {
		return
	}
	i := max(0, slices.IndexFunc(rows, func(it api.ActivityItem) bool { return it.Download.EpisodeID == s.selected }))
	s.selected = rows[min(len(rows)-1, max(0, i+n))].Download.EpisodeID
}

// since is when the row's state began: when the download started while it
// runs, otherwise when it last changed.
func since(d api.Download) time.Time {
	if d.Active() {
		return d.Created
	}
	if d.Import.Updated.After(d.Updated) {
		return d.Import.Updated
	}
	return d.Updated
}

func itemShow(it api.ActivityItem) string { return clean(cmp.Or(it.SeriesNameEng, it.SeriesName)) }

func itemEpisode(it api.ActivityItem) string {
	return fmt.Sprintf("S%02dE%02d", it.Season, it.Number)
}

const (
	activityTop        = 6 // rows above the list: header, blank, facts, blank, column names, rule
	activityShowWidth  = 34
	activityEpWidth    = len("Episode")
	activitySinceWidth = len("16 Oct 14:02")
)

func (s *activityScreen) view(width, height int) string {
	var b strings.Builder
	now := time.Now()
	b.WriteString(header(width, s.sess, "garner", "Activity") + "\n\n")
	b.WriteString(s.facts(width, now) + "\n\n")
	// Wide: State takes the room, as on the other tables. Narrow: the show
	// names give way to State, down to a minimum.
	fixed := 3 + 1 + 3 + 3 + activityEpWidth + 3 + 3 + activitySinceWidth + 3
	showWidth := max(16, min(activityShowWidth, width-fixed-24))
	stateWidth := max(10, width-fixed-showWidth)
	head := func(t string, n int, right bool) string { return sSubtle.Render(fit(t, n, right)) }
	b.WriteString("       " + head("Show", showWidth, false) + "   " + head("Episode", activityEpWidth, false) + "   " +
		head("State", stateWidth, false) + "   " + head("Since", activitySinceWidth, true) + "\n")
	b.WriteString("   " + sBorder.Render(strings.Repeat("─", max(0, width-6))) + "\n")

	rows := s.rows()
	switch {
	case s.err != nil:
		b.WriteString("\n   " + gFailed + " " + sRed.Render(clean(s.err.Error())))
	case s.items == nil:
		b.WriteString("\n   " + sMuted.Render("Loading…"))
	case len(rows) == 0:
		b.WriteString("\n   " + sMuted.Render("Nothing running. Finished downloads stay here for a day."))
	default:
		// Lines, for scrolling: each row, then its rejected attempts.
		var lines []string
		cursor := 0 // the cursor row's line
		last := 0   // the last line that belongs to it
		for _, it := range rows {
			selected := it.Download.EpisodeID == s.selected
			if selected {
				cursor = len(lines)
			}
			lines = append(lines, activityRow(it, selected, showWidth, stateWidth, width, now))
			for _, r := range it.Rejected {
				lines = append(lines, rejectedRow(r, width, now))
			}
			if selected {
				last = len(lines) - 1
			}
		}
		page := max(3, height-activityTop-barHeight)
		s.page = page
		// The cursor row shows with its rejected attempts where they fit.
		s.offset = min(max(s.offset, last-page+1), cursor)
		for _, l := range lines[s.offset:min(len(lines), s.offset+page)] {
			b.WriteString(l + "\n")
		}
	}
	page := screenLayout(width, height, b.String(), s.bottom())
	if s.asking {
		if it, ok := s.cursor(); ok {
			return overlay(page, []string{
				gFailed + " " + sBold.Render("Cancel the download of "+itemShow(it)+" "+itemEpisode(it)+"?"),
				"",
				choice("Keep going", s.choice == 0) + "   " + choice("Cancel download", s.choice == 1),
				"",
				keyBar("h l", "choose", "enter", "ok", "esc", "back"),
			}, width, height)
		}
	}
	return page
}

func activityRow(it api.ActivityItem, selected bool, showWidth, stateWidth, width int, now time.Time) string {
	cur := cursor(selected)
	bg := cur.bg
	d := it.Download
	state := activityState(d)
	glyph, glyphStyle := episodeGlyph(state)
	text := sNormal
	var parts []part
	switch state {
	case api.EpisodeImported:
		// Done: nothing to do, so subtle, as on the show screen. What the
		// file check found, or without a check what the release claimed.
		text = sSubtle
		found := cmp.Or(foundTier(fileCheck(d)), clean(d.Tier))
		parts = []part{{strings.TrimPrefix(found+" · "+size(d.Size), " · "), sSubtle}}
		if d.Replaces != "" {
			parts = append(parts, part{" · replaced " + clean(d.Replaces), sSubtle})
		}
	case activityCleared:
		glyph, glyphStyle, text = "✗", sSubtle, sSubtle
		parts = []part{{"cleared · " + clean(cmp.Or(d.FailureSummary, d.Failure)), sSubtle}}
	case activityCancelled:
		glyph, glyphStyle, text = "✗", sSubtle, sSubtle
		parts = []part{{"cancelled", sSubtle}}
	default:
		// A running download that is not the episode's first names its
		// attempt, as the Show screen does.
		parts = statusParts(state, &d, "", nil, d.Attempt, now)
		if (state == api.EpisodeProcessing || state == api.EpisodeImporting) && d.Attempt > 1 {
			parts = append(parts, part{" · attempt " + strconv.Itoa(d.Attempt), sMuted})
		}
	}
	gap := bg(lipgloss.NewStyle()).Render("   ")
	line := cur.margin() + bg(glyphStyle).Render(glyph) + gap +
		bg(text).Render(fit(itemShow(it), showWidth, false)) + gap +
		bg(text).Render(fit(itemEpisode(it), activityEpWidth, false)) + gap +
		renderParts(parts, stateWidth, bg)
	line = cur.fill(line, width-3-activitySinceWidth) + bg(sSubtle).Render(fit(when(since(d), now), activitySinceWidth, true))
	return cur.fill(line, width-3)
}

// rejectedRow hangs an attempt the file check rejected under its episode's
// row, with the server's reason and when it was rejected.
func rejectedRow(d api.Download, width int, now time.Time) string {
	lead := "       " + sSubtle.Render("└ ")
	room := width - 3 - activitySinceWidth - 3 - lipgloss.Width(lead)
	line := lead + renderParts(rejectedParts(d), room, func(s lipgloss.Style) lipgloss.Style { return s })
	return pad(line, width-3-activitySinceWidth) + sSubtle.Render(fit(when(cmp.Or(d.Import.Updated, d.Updated), now), activitySinceWidth, true))
}

// rejectedParts names an attempt the file check rejected, by its number
// when the server sends one, with the server's reason.
func rejectedParts(d api.Download) []part {
	label := "rejected after download"
	if d.Attempt > 0 {
		label = "attempt " + strconv.Itoa(d.Attempt) + " rejected"
	}
	return []part{{label, sMuted}, {" · ", sMuted}, {rejection(d), sMuted}}
}

// facts count what runs, what was imported today and what the file check
// rejected, which the server counts over its current shows.
func (s *activityScreen) facts(width int, now time.Time) string {
	inFlight, today := 0, 0
	for _, it := range s.items {
		d := it.Download
		switch {
		case d.Active():
			inFlight++
		case d.Import.State == api.ImportDone && d.Import.Updated.Local().Format(time.DateOnly) == now.Local().Format(time.DateOnly):
			today++
		}
	}
	parts := []string{fact("In flight", fmt.Sprint(inFlight)), fact("Imported today", fmt.Sprint(today))}
	if r := s.rejections; r != nil {
		counts := "none"
		if r.Total > 0 {
			counts = fmt.Sprintf("%d in the last 7 days · %d since %s", r.LastWeek, r.Total, day(r.Oldest.Local().Format(time.DateOnly), now))
		}
		parts = append(parts, fact("Rejected after download", counts))
	}
	return factLines(width, parts...)
}

func (s *activityScreen) bottom() string {
	switch {
	case s.busy != "":
		return sMuted.Render(s.busy)
	case s.flash.text != "":
		return s.flash.text
	}
	var parts []string
	if len(s.items) > 0 {
		parts = append(parts, keyBar("j k", "move"))
	}
	// enter, x and c keep their place on rows they do not apply to, and
	// grey out when no row is listed.
	parts = append(parts, keySlot("enter", "episode", can(s, "enter")),
		keySlot("x", "cancel", can(s, "x")),
		keySlot("c", "clear failure", can(s, "c")))
	return joinKeys(append(parts, keyBar("?", "help", "esc", "back"))...)
}

func (s *activityScreen) flashed() *flash { return &s.flash }

// action says which keys run on the row under the cursor: x on a download
// SABnzbd still has, c on a failure not cleared.
func (s *activityScreen) action(key, label string) (string, bool) {
	it, ok := s.cursor()
	switch key {
	case "enter":
		return label, ok
	case "x":
		return label, ok && cancellable(it.Download)
	case "c":
		return label, ok && it.Download.CanAcknowledge()
	}
	return label, true
}

func (s *activityScreen) captures() bool { return s.asking || s.busy != "" }

func (s *activityScreen) help() (string, []string) {
	return "Activity", []string{"enter", "open the episode", "x", "cancel the download", "c", "clear the selected download failure"}
}
