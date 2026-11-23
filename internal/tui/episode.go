package tui

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sudokvn/garner/internal/api"
	"github.com/sudokvn/garner/internal/client"
)

// Episode follows one episode through garner: its release, the six steps
// from SABnzbd to Jellyfin, and what to fix or what was noted.

type (
	episodeMsg struct {
		from   *episodeScreen
		detail api.EpisodeDetail
		err    error
	}
	cancelledMsg struct {
		download api.Download
		err      error
	}
	retriedMsg struct {
		download api.Download
		err      error
	}
)

type episodeScreen struct {
	sess   *session
	show   *showScreen
	back   screen // where esc goes: the show, or the activity
	id     int64  // TVDB episode ID
	detail *api.EpisodeDetail
	err    error
	// A reload asked for while one runs follows it.
	loading, dirty bool
	asking         bool // the cancel question
	choice         int
	busy           string
	flash          flash
	scroll         scroller
}

func newEpisode(sess *session, show *showScreen, id int64) *episodeScreen {
	return &episodeScreen{sess: sess, show: show, back: show, id: id}
}

// init loads the episode, and its show unless the show screen has already.
func (s *episodeScreen) init() tea.Cmd {
	if s.show.detail == nil {
		return tea.Batch(s.reload(), s.show.reload())
	}
	return s.reload()
}

func (s *episodeScreen) reload() tea.Cmd {
	if s.loading {
		s.dirty = true
		return nil
	}
	s.loading = true
	cl, id := s.sess.client, s.id
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		d, err := cl.Episode(ctx, id)
		return episodeMsg{s, d, err}
	}
}

func (s *episodeScreen) update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case episodeMsg:
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
			s.err, s.detail = nil, &msg.detail
		}
		var cmd tea.Cmd
		if s.dirty {
			s.dirty = false
			cmd = s.reload()
		}
		return s, tea.Batch(cmd, s.redrawWhenSlow())
	case cancelledMsg:
		s.busy = ""
		switch {
		case errors.Is(msg.err, client.ErrUnauthorized):
			return s, func() tea.Msg { return disconnectedMsg{errRevoked} }
		case msg.err != nil:
			s.flash.stay(gFailed + " " + sRed.Render("Could not cancel the download: "+clean(msg.err.Error())))
			return s, nil
		}
		if f, ok := s.back.(flasher); ok {
			f.flashed().info(gDone + " " + sNormal.Render("Cancelled the download of "+s.label()))
		}
		return s, func() tea.Msg { return switchMsg{s.back} }
	case retriedMsg:
		s.busy = ""
		switch {
		case errors.Is(msg.err, client.ErrUnauthorized):
			return s, func() tea.Msg { return disconnectedMsg{errRevoked} }
		case msg.err != nil:
			s.flash.stay(gFailed + " " + sRed.Render("Could not retry the import: "+clean(msg.err.Error())))
			return s, nil
		}
		s.flash.info(gWorking + " " + sNormal.Render("Retrying the import"))
		return s, s.apply(msg.download)
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
		return s, tea.Batch(s.apply(msg.download), s.show.reload())
	case eventMsg:
		var cmd tea.Cmd
		switch msg.Type {
		case client.EventConnected:
			cmd = s.reload()
		case api.EventDownloadUpdated:
			var d api.Download
			if json.Unmarshal(msg.Data, &d) == nil && d.EpisodeID == s.id {
				cmd = s.apply(d)
			}
		}
		// The show screen keeps the episode states current.
		_, showCmd := s.show.update(msg)
		return s, tea.Batch(cmd, showCmd)
	case seriesMsg:
		// Answers the show screen asked for.
		_, cmd := s.show.update(msg)
		return s, cmd
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

// apply takes a changed download from an event. Progress changes in place;
// a new attempt or a new step loads the episode again, for its file.
func (s *episodeScreen) apply(d api.Download) tea.Cmd {
	if s.detail == nil {
		return s.reload()
	}
	list := s.detail.Downloads
	i := slices.IndexFunc(list, func(x api.Download) bool { return x.ID == d.ID })
	if i < 0 || list[i].State != d.State || list[i].Import.State != d.Import.State {
		if i >= 0 {
			list[i] = d
		}
		return tea.Batch(s.reload(), s.redrawWhenSlow())
	}
	list[i] = d
	return nil
}

func (s *episodeScreen) redrawWhenSlow() tea.Cmd {
	if d, ok := s.latest(); ok {
		return redrawWhenSlow(&d)
	}
	return nil
}

func (s *episodeScreen) key(key string) tea.Cmd {
	if s.scroll.key(key) {
		return nil
	}
	switch key {
	case "esc":
		return func() tea.Msg { return switchMsg{s.back} }
	case "x":
		if _, ok := s.cancellable(); ok {
			s.asking, s.choice = true, 0 // the safe choice: keep going
		}
	case "c":
		if d, ok := s.latest(); ok && d.CanAcknowledge() {
			s.busy = "Clearing the failure…"
			return acknowledgeFailure(s.sess.client, d.ID)
		}
	case "enter":
		return s.releases(false)
	case "s":
		return s.releases(true)
	case "r":
		return s.retryImport()
	}
	return nil
}

// answer handles the cancel question.
func (s *episodeScreen) answer(key string) tea.Cmd {
	switch key {
	case "esc":
		s.asking = false
	case "h", "left", "shift+tab":
		s.choice = 0
	case "l", "right", "tab":
		s.choice = 1
	case "enter":
		s.asking = false
		d, ok := s.cancellable()
		switch {
		case s.choice != 1:
			return nil
		case !ok:
			s.flash.info(gWaiting + " " + sNormal.Render("Too late to cancel: SABnzbd has finished downloading it."))
			return nil
		}
		s.busy = "Cancelling the download…"
		cl := s.sess.client
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			d, err := cl.CancelDownload(ctx, d.ID)
			return cancelledMsg{d, err}
		}
	}
	return nil
}

// retryable reports whether r retries the latest download's import: it
// failed, or Jellyfin did not confirm the file.
func (s *episodeScreen) retryable() (api.Download, bool) {
	d, ok := s.latest()
	return d, ok && (d.Import.State == api.ImportFailed || d.Import.State == api.ImportUnverified)
}

func (s *episodeScreen) retryImport() tea.Cmd {
	d, ok := s.retryable()
	if !ok {
		return nil
	}
	s.busy = "Retrying the import…"
	cl := s.sess.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		d, err := cl.RetryImport(ctx, d.ID)
		return retriedMsg{d, err}
	}
}

// searched reports whether the episode has a last search to show: the
// show reports one, or a download was chosen from it. A cleared failure
// hides the show's report, not the search.
func (s *episodeScreen) searched() bool {
	row, ok := s.row()
	return ok && s.detail != nil && (row.status.Summary != "" || len(s.detail.Downloads) > 0)
}

// releases opens the episode's releases: with fresh, a new search, as s on
// its row of the Show screen does; otherwise the last search's, as it
// stands. esc there comes back here, not to the show.
func (s *episodeScreen) releases(fresh bool) tea.Cmd {
	row, ok := s.row()
	if !ok || !fresh && !s.searched() {
		return nil
	}
	next := newReleases(s.sess, s.show, row, fresh)
	next.leave, next.stored = s, !fresh
	return func() tea.Msg { return switchMsg{next} }
}

func (s *episodeScreen) latest() (api.Download, bool) {
	if s.detail == nil || len(s.detail.Downloads) == 0 {
		return api.Download{}, false
	}
	return s.detail.Downloads[0], true
}

// cancellable is the download x cancels: one SABnzbd queues or downloads.
func (s *episodeScreen) cancellable() (api.Download, bool) {
	d, ok := s.latest()
	return d, ok && (d.State == api.Queued || d.State == api.Downloading)
}

// row is the episode as the show screen has it.
func (s *episodeScreen) row() (episodeRow, bool) {
	for _, se := range s.show.seasons() {
		for _, e := range se.episodes {
			if e.episode.ID == s.id {
				return e, true
			}
		}
	}
	return episodeRow{}, false
}

func (s *episodeScreen) label() string {
	if row, ok := s.row(); ok {
		return episodeLabel(row)
	}
	return "the episode"
}

func (s *episodeScreen) view(width, height int) string {
	now := time.Now()
	row, _ := s.row()
	var b strings.Builder
	b.WriteString(header(width, s.sess, "garner", "Library", clean(s.show.name), s.label()) + "\n\n")
	b.WriteString(factLines(width, episodeFacts(row)...) + "\n")
	switch {
	case s.detail == nil && s.err != nil:
		b.WriteString("\n   " + gFailed + " " + sRed.Render(clean(s.err.Error())))
	case s.detail == nil:
		b.WriteString("\n   " + sMuted.Render("Loading…"))
	default:
		// As wide as the design at most, so times stay near their steps.
		w := min(width, designWidth)
		if d, ok := s.latest(); ok {
			b.WriteString("   " + s.releaseFacts(d, w-6) + "\n\n")
			steps := pipeline(d, now)
			markCaveat(steps, d, s.best())
			for _, st := range steps {
				b.WriteString(st.render(w) + "\n")
			}
			// Why no other release started after this one failed.
			if d.State == api.Failed && d.Stopped != "" {
				b.WriteString("\n")
				for _, l := range wrapAfter(clean(d.Stopped), " ", w-6) {
					b.WriteString("   " + sMuted.Render(strings.TrimRight(l, " ")) + "\n")
				}
			}
		}
		for _, f := range s.frames(row, max(40, w-6), now) {
			b.WriteString("\n" + indent(f, 3) + "\n")
		}
	}
	page := s.scroll.layout(width, height, b.String(), s.bottom())
	if s.asking {
		return overlay(page, s.cancelQuestion(), width, height)
	}
	return page
}

// releaseFacts is the release line: name, tier, size, indexer and which
// attempt this is.
func (s *episodeScreen) releaseFacts(d api.Download, w int) string {
	meta := []string{}
	switch d.Verdict {
	case api.Accept:
		meta = append(meta, sMuted.Render(clean(d.Tier)))
	case api.Fallback:
		meta = append(meta, sAmber.Render("last resort"))
	case api.Reject:
		if !d.Manual {
			meta = append(meta, sRed.Render("rejected"))
		}
	}
	if d.Manual {
		meta = append(meta, sAmber.Render("chosen by hand"))
	}
	if d.Replaces != "" {
		meta = append(meta, sMuted.Render("replaces "+clean(d.Replaces)))
	}
	if sz := size(d.Size); sz != "" {
		meta = append(meta, sMuted.Render(sz))
	}
	meta = append(meta, sMuted.Render(clean(d.Indexer)))
	if !d.Acknowledged.IsZero() {
		meta = append(meta, sSubtle.Render("cleared "+when(d.Acknowledged, time.Now())))
	}
	tail := "      " + strings.Join(meta, sMuted.Render(" · "))
	// The attempt counts every download of the episode, as Earlier
	// attempts and the Show screen do.
	if attempt := len(s.detail.Downloads); attempt > 1 {
		tail += "      " + fact("Attempt", fmt.Sprint(attempt))
	}
	// Narrow: the name keeps the line, the rest moves under it.
	room := w - len("Release  ")
	if ansi.StringWidth(clean(d.Title))+lipgloss.Width(tail) <= room || room-lipgloss.Width(tail) >= 40 {
		return fact("Release", cutMiddle(clean(d.Title), max(20, room-lipgloss.Width(tail)))) + tail
	}
	return fact("Release", cutMiddle(clean(d.Title), room)) + "\n   " + strings.Repeat(" ", len("Release  ")) +
		strings.TrimPrefix(tail, "      ")
}

// frames are the panes under the pipeline, most important first.
func (s *episodeScreen) frames(row episodeRow, w int, now time.Time) []string {
	var out []string
	d, ok := s.latest()
	if ok {
		if details := downloadDetails(d, w); details != "" {
			out = append(out, details)
		}
	}
	if ok && d.Active() && len(d.Reasons) > 0 {
		out = append(out, frame("why this release", reasonLines(d.Verdict, d.Reasons, d.Decisive, w-8), w))
	}
	if ok && !d.Active() {
		if notes := s.notes(d); len(notes) > 0 {
			out = append(out, frame("notes", notes, w))
		}
	}
	if earlier := s.earlier(w-6, now); len(earlier) > 0 {
		out = append(out, frame("earlier attempts", earlier, w))
	}
	if f := s.detail.File; f != nil && (row.status.State == api.EpisodeImported || row.status.State == api.EpisodeAttention) {
		dir, name := filepath.Split(clean(f.Path))
		var lines []string
		for _, l := range wrapPath(dir, w-6) {
			lines = append(lines, sMuted.Render(l))
		}
		for _, l := range wrapAfter(name, ".", w-6) {
			lines = append(lines, sNormal.Render(l))
		}
		out = append(out, frame("file", append(lines, sMuted.Render(size(f.Size))), w))
	}
	if ov := s.detail.Overview; ov != "" {
		out = append(out, frame(overviewLabel(s.detail.OverviewLanguage), []string{sNormal.Render(clean(ov))}, w))
	}
	return out
}

// best is the show's first preference, such as "Japanese 1080p"; empty
// until the show is loaded.
func (s *episodeScreen) best() string {
	if s.show.detail == nil {
		return ""
	}
	return s.show.detail.Best
}

// notes are the file check's notes on the episode's file, amber when the
// check passed without the language the release was for, then the
// import's. The sentences come from the server.
func (s *episodeScreen) notes(d api.Download) []string {
	var lines []string
	if c := fileCheck(d); c != nil {
		style := sNormal
		if _, ok := missingLanguage(d, c, s.best()); ok {
			style = sAmber
		}
		for _, n := range c.Notes {
			lines = append(lines, style.Render(clean(n)))
		}
	}
	for _, n := range d.Import.Notes {
		lines = append(lines, sNormal.Render(clean(n)))
	}
	return lines
}

// earlier lists every download before the latest, newest first, in two
// lines each within w cells: the attempt, its release, size and start,
// then what came of it.
func (s *episodeScreen) earlier(w int, now time.Time) []string {
	if s.detail == nil || len(s.detail.Downloads) < 2 {
		return nil
	}
	all := s.detail.Downloads
	labelWidth := len("Attempt ") + len(fmt.Sprint(len(all)-1))
	var lines []string
	for i, e := range all[1:] {
		if i > 0 {
			lines = append(lines, "")
		}
		label := sSubtle.Render(pad(fmt.Sprintf("Attempt %d", len(all)-1-i), labelWidth)) + "   "
		// The size tells uploads of the same name apart.
		meta := when(e.Created, now)
		if sz := size(e.Size); sz != "" {
			meta = sz + " · " + meta
		}
		room := w - lipgloss.Width(label)
		name := cutMiddle(clean(e.Title), max(20, room-len(meta)-3))
		lines = append(lines, pad(label+sNormal.Render(name), w-ansi.StringWidth(meta))+sSubtle.Render(meta),
			strings.Repeat(" ", lipgloss.Width(label))+renderParts(attemptOutcome(e), room, func(s lipgloss.Style) lipgloss.Style { return s }))
	}
	return lines
}

// attemptOutcome says what came of an earlier download. A rejected file
// shows what its release claimed and what the file check found.
func attemptOutcome(d api.Download) []part {
	sep := part{" · ", sMuted}
	switch {
	case d.State == api.Cancelled:
		return []part{{"cancelled", sSubtle}}
	case d.Import.State == api.ImportRejected:
		parts := []part{{"rejected after download", sMuted}, sep}
		found := foundProblems(fileCheck(d))
		if found == "" {
			return append(parts, part{rejection(d), sRed})
		}
		if d.Tier != "" {
			parts = append(parts, part{"claimed ", sMuted}, part{clean(d.Tier), sNormal}, part{"   ", sMuted})
		}
		return append(parts, part{"found ", sMuted}, part{found, sRed})
	case d.Import.State == api.ImportFailed:
		return []part{{"import failed", sMuted}, sep, {clean(d.Import.Detail), sRed}}
	case d.State == api.Failed:
		return []part{{"download failed", sMuted}, sep, {clean(cmp.Or(d.FailureSummary, d.Failure)), sRed}}
	case d.Import.State == api.ImportUnverified:
		return []part{{"needs you", sAmber}, sep, {clean(d.Import.Detail), sMuted}}
	case d.Import.State == api.ImportDone:
		parts := []part{{"imported", sMuted}}
		if tier := cmp.Or(foundTier(fileCheck(d)), clean(d.Tier)); tier != "" {
			parts = append(parts, sep, part{tier, sMuted})
		}
		return parts
	}
	label, style := attemptLabel(&d)
	return []part{{label, style}}
}

// rejection is the server's reason for rejecting a download, without the
// step it names, as the pipeline shows it.
func rejection(d api.Download) string {
	return clean(strings.TrimPrefix(cmp.Or(d.Failure, d.Import.Detail), "file check: "))
}

func (s *episodeScreen) cancelQuestion() []string {
	return []string{
		gFailed + " " + sBold.Render("Cancel the download of "+s.label()+"?"),
		"",
		choice("Keep going", s.choice == 0) + "   " + choice("Cancel download", s.choice == 1),
		"",
		keyBar("h l", "choose", "enter", "ok", "esc", "back"),
	}
}

func (s *episodeScreen) bottom() string {
	switch {
	case s.busy != "":
		return sMuted.Render(s.busy)
	case s.flash.text != "":
		return s.flash.text
	}
	// Every key keeps its place, greyed out where it does not apply.
	return joinKeys(
		keySlot("x", "cancel", can(s, "x")),
		keySlot("enter", "releases", can(s, "enter")),
		keySlot("s", "new search", can(s, "s")),
		keySlot("r", "retry import", can(s, "r")),
		keySlot("c", "clear failure", can(s, "c")),
		keyBar("A", "activity", "?", "help", "esc", "back"),
	)
}

func (s *episodeScreen) flashed() *flash { return &s.flash }

// action says which keys run on the episode now: s once the show has the
// episode, as on its row there, enter when it has a last search, r when
// its import can be retried.
func (s *episodeScreen) action(key, label string) (string, bool) {
	switch key {
	case "x":
		_, ok := s.cancellable()
		return label, ok
	case "enter":
		return label, s.searched()
	case "s":
		_, ok := s.row()
		return label, ok
	case "r":
		_, ok := s.retryable()
		return label, ok
	case "c":
		d, ok := s.latest()
		return label, ok && d.CanAcknowledge()
	}
	return label, true
}

func (s *episodeScreen) captures() bool { return s.asking || s.busy != "" }

func (s *episodeScreen) help() (string, []string) {
	return "Episode", []string{
		"enter", "the last search's releases, without searching", "s", "search the episode again",
		"r", "retry the import", "x", "cancel the download",
		"c", "clear the latest download failure",
	}
}

// step is one row of the pipeline.
type step struct {
	glyph  string
	name   string
	style  lipgloss.Style // the name's
	detail []part
	at     time.Time
}

const stepNameWidth = len("Moving into the library")

// render draws the step: glyph, name, detail, and its time at the right.
func (st step) render(width int) string {
	at := ""
	if !st.at.IsZero() {
		at = when(st.at, time.Now())
	}
	left := "   " + st.glyph + "   " + st.style.Render(pad(st.name, stepNameWidth)) + "   "
	room := width - 3 - lipgloss.Width(left) - len(at) - 3
	line := left + renderParts(st.detail, room, func(s lipgloss.Style) lipgloss.Style { return s })
	if at == "" {
		return line
	}
	return pad(line, width-3-len(at)) + sSubtle.Render(at)
}

// pipeline lays a download out as the six steps from SABnzbd to Jellyfin.
// Times show where garner records them: when it was sent, when SABnzbd
// finished, and when the import ended or failed.
func pipeline(d api.Download, now time.Time) []step {
	// Each step's name while pending, running and done.
	names := [6][3]string{
		{"Send to SABnzbd", "Sending to SABnzbd", "Sent to SABnzbd"},
		{"Download", "Downloading", "Downloaded"},
		{"Processing", "Processing", "Processed"},
		{"File check", "File check", "File check"},
		{"Move into the library", "Moving into the library", "Moved into the library"},
		{"Jellyfin confirms", "Jellyfin confirms", "Jellyfin confirms"},
	}
	steps := make([]step, len(names))
	for i := range steps {
		steps[i] = step{glyph: gWaiting, name: names[i][0], style: sSubtle}
	}
	run := func(i int, glyph string, detail ...part) {
		steps[i] = step{glyph: glyph, name: names[i][1], style: sNormal, detail: detail}
	}
	done := func(i int, at time.Time, detail ...part) {
		steps[i] = step{glyph: gDone, name: names[i][2], style: sNormal, detail: detail, at: at}
	}
	fail := func(i int, why string, at time.Time) {
		steps[i] = step{glyph: gFailed, name: names[i][0], style: sNormal, detail: []part{{clean(why), sRed}}, at: at}
	}
	downloading := sAmber.Render("◐")
	im := d.Import

	switch {
	case d.State == api.Submitting:
		run(0, downloading, part{clean(d.Detail), sMuted})
		return steps
	case d.State == api.Failed && d.JobID == "":
		fail(0, d.Failure, d.Updated)
		return steps
	}
	done(0, d.Created)

	switch {
	case d.State == api.Queued, d.State == api.Downloading:
		run(1, downloading, pipelineParts(api.EpisodeDownloading, &d, "", nil, 0, now)...)
		steps[2].detail = []part{{"repair and unpack", sSubtle}}
		return steps
	case d.State == api.Cancelled:
		steps[1] = step{glyph: sSubtle.Render("✗"), name: names[1][0], style: sNormal, detail: []part{{"cancelled", sMuted}}, at: d.Updated}
		return steps
	case d.State == api.Failed && im.State == "":
		// SABnzbd's failure, in the download or its repair and unpack.
		fail(1, cmp.Or(d.FailureSummary, d.Failure), d.Updated)
		return steps
	}
	done(1, time.Time{}, part{size(d.Size), sMuted})

	if d.State == api.Processing {
		run(2, gWorking, part{clean(cmp.Or(d.Detail, "processing")), sMuted})
		return steps
	}
	var finished time.Time
	if d.State == api.Completed {
		finished = d.Updated
	}
	done(2, finished, part{stageNames(d.Stages), sMuted})

	switch {
	case im.State == "", im.State == api.ImportPending:
		run(3, gWorking, part{"checking the file", sMuted})
		return steps
	case im.State == api.ImportRejected:
		// The row names the step; the reason need not.
		fail(3, strings.TrimPrefix(cmp.Or(d.Failure, im.Detail), "file check: "), d.Updated)
		return steps
	case im.State == api.ImportFailed && !im.Planned():
		fail(3, im.Detail, im.Updated)
		return steps
	}
	done(3, time.Time{}, checkParts(im.Files)...)

	switch im.State {
	case api.ImportMoving:
		run(4, gWorking, part{"moving the file", sMuted})
		return steps
	case api.ImportFailed:
		fail(4, im.Detail, im.Updated)
		return steps
	}
	target := ""
	if len(im.Files) > 0 {
		target = filepath.Base(clean(im.Files[0].Target))
	}
	done(4, time.Time{}, part{target, sMuted})

	switch im.State {
	case api.ImportScanning:
		wait := "waiting for Jellyfin to list it"
		if now.Sub(d.Updated) >= importSlowAfter {
			wait += " · since " + when(d.Updated, now)
		}
		run(5, gWorking, part{wait, sMuted})
	case api.ImportUnverified:
		steps[5] = step{glyph: sAmber.Render("▲"), name: names[5][2], style: sNormal,
			detail: []part{{clean(im.Detail), sAmber}}, at: im.Updated}
	default:
		done(5, im.Updated, part{clean(im.Detail), sMuted})
	}
	return steps
}

// stageNames lists SABnzbd's post-processing steps, such as "repair ·
// unpack".
func stageNames(stages []api.Stage) string {
	var names []string
	for _, st := range stages {
		switch st.Name {
		case "Source", "Download", "Servers":
			continue // where it came from, not what was done to it
		}
		names = append(names, strings.ToLower(clean(st.Name)))
	}
	return strings.Join(names, " · ")
}

// checkParts sums up the file check: "German, Japanese audio · 1080p · 24
// min".
func checkParts(files []api.ImportFile) []part {
	if len(files) == 0 || files[0].Check == nil {
		return nil
	}
	res := files[0].Check
	var facts []string
	var audio []string
	for _, a := range res.Audio {
		if a = clean(a); !slices.Contains(audio, a) {
			audio = append(audio, a)
		}
	}
	if len(audio) > 0 {
		facts = append(facts, strings.Join(audio, ", ")+" audio")
	}
	var w, h int
	if _, err := fmt.Sscanf(res.Resolution, "%dx%d", &w, &h); err == nil {
		facts = append(facts, fmt.Sprintf("%dp", api.ResolutionClass(w, h)))
	}
	if d, err := time.ParseDuration(res.Duration); err == nil {
		facts = append(facts, fmt.Sprintf("%d min", int(d.Round(time.Minute).Minutes())))
	}
	if len(files) > 1 {
		facts = append(facts, plural(len(files), "file"))
	}
	return []part{{strings.Join(facts, " · "), sMuted}}
}

// fileCheck is what the file check found in the download's file of its
// episode, or in its first checked file; nil before the check.
func fileCheck(d api.Download) *api.FileCheck {
	var first *api.FileCheck
	for _, f := range d.Import.Files {
		switch {
		case f.Check == nil:
		case slices.Contains(f.Episodes, d.EpisodeID):
			return f.Check
		case first == nil:
			first = f.Check
		}
	}
	return first
}

// foundAudio names the audio languages the check found, each once.
// Tracks without a language tag are "unknown", unless others are named.
func foundAudio(c *api.FileCheck) []string {
	var named []string
	unknown := false
	for _, a := range c.Audio {
		switch a = clean(a); {
		case a == "unknown":
			unknown = true
		case !slices.Contains(named, a):
			named = append(named, a)
		}
	}
	if len(named) == 0 && unknown {
		return []string{"unknown"}
	}
	return named
}

// foundResolution names the video's resolution as releases do, such as
// "1080p"; empty when the check found no video.
func foundResolution(c *api.FileCheck) string {
	var w, h int
	if _, err := fmt.Sscanf(c.Resolution, "%dx%d", &w, &h); err != nil {
		return ""
	}
	return fmt.Sprintf("%dp", api.ResolutionClass(w, h))
}

// foundTier writes what the check found as a preference reads, such as
// "English 1080p"; empty without a check.
func foundTier(c *api.FileCheck) string {
	if c == nil {
		return ""
	}
	audio := strings.Join(foundAudio(c), ", ")
	if audio == "unknown" || audio == "" {
		audio = "unknown audio"
	}
	return strings.TrimSpace(audio + " " + foundResolution(c))
}

// splitTier splits a preference such as "Japanese 1080p" into its
// language and resolution.
func splitTier(tier string) (language, resolution string, ok bool) {
	i := strings.LastIndex(tier, " ")
	if i <= 0 {
		return "", "", false
	}
	return tier[:i], tier[i+1:], true
}

// meets reports whether what the check found meets a preference such as
// "Japanese 1080p": audio in its language at its resolution.
func meets(c *api.FileCheck, tier string) bool {
	language, resolution, ok := splitTier(tier)
	return ok && slices.Contains(c.Audio, language) && foundResolution(c) == resolution
}

// missingLanguage reports the language a passed check did not find: the
// language of the preference the release met, or for a release without
// one, such as a rejected one chosen by hand, of the profile's first
// preference. The file was kept, and the check's notes say why. A last
// resort may have any of the profile's languages, so it has none missing.
func missingLanguage(d api.Download, c *api.FileCheck, best string) (string, bool) {
	if c == nil || len(c.Problems) > 0 || d.Verdict == api.Fallback {
		return "", false
	}
	language, _, ok := splitTier(cmp.Or(d.Tier, best))
	if !ok || slices.Contains(c.Audio, language) {
		return "", false
	}
	return language, true
}

// markCaveat turns the passed file check amber when the file lacks the
// language its release was for, and says so before the other facts.
func markCaveat(steps []step, d api.Download, best string) {
	c := fileCheck(d)
	language, ok := missingLanguage(d, c, best)
	if !ok || len(steps) < 4 || steps[3].glyph != gDone {
		return
	}
	audio := foundAudio(c)
	text := strings.Join(audio, ", ") + " audio"
	// An untagged track may be in the language: no claim then.
	if !slices.Contains(c.Audio, "unknown") {
		text += ", no " + language + " track"
	}
	facts := []part{{text, sAmber}}
	if res := foundResolution(c); res != "" {
		facts = append(facts, part{" · " + res, sMuted})
	}
	if dur, err := time.ParseDuration(c.Duration); err == nil {
		facts = append(facts, part{fmt.Sprintf(" · %d min", int(dur.Round(time.Minute).Minutes())), sMuted})
	}
	steps[3].glyph, steps[3].detail = sAmber.Render("✓"), facts
}

// foundProblems names what the file check found that its problems are
// about, such as "1280x720" or "English audio"; empty for problems the
// facts do not show, such as an unreadable file.
func foundProblems(c *api.FileCheck) string {
	if c == nil {
		return ""
	}
	var found []string
	add := func(s string) {
		if s != "" && !slices.Contains(found, s) {
			found = append(found, s)
		}
	}
	for i := range c.Problems {
		switch c.Kind(i) {
		case api.ProblemResolution:
			add(clean(c.Resolution))
		case api.ProblemAudioLanguage:
			add(strings.Join(foundAudio(c), ", ") + " audio")
		case api.ProblemNoAudioTrack:
			add("no audio track")
		case api.ProblemNoVideoTrack:
			add("no video track")
		case api.ProblemLength:
			if dur, err := time.ParseDuration(c.Duration); err == nil {
				add(fmt.Sprintf("%d min", int(dur.Round(time.Minute).Minutes())))
			}
		default:
			return "" // the server's sentence says it better
		}
	}
	return strings.Join(found, " · ")
}
