package tui

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sudokvn/garner/internal/api"
	"github.com/sudokvn/garner/internal/client"
)

// The Library's preview: on a terminal previewWidth cells wide or more,
// the right half shows the show under the cursor, its facts and its
// newest season's episodes, with the rejected attempts of a download in
// flight under its episode. tab moves into it and back.

const previewWidth = 160

// libraryPreviewMsg answers the load of one show for the preview.
type libraryPreviewMsg struct {
	from   *libraryScreen
	gen    int // which load, so an answer to a replaced one is dropped
	id     int64
	detail api.SeriesDetail
	err    error
}

type preview struct {
	details map[int64]*api.SeriesDetail // shows loaded so far
	errs    map[int64]error
	// fresh holds the shows loaded since the server last reported a
	// change; the others show until they load again.
	fresh map[int64]bool
	asked int64 // the show being loaded, 0 when none
	gen   int   // counts the loads asked for
	again bool  // a change arrived while it loaded; load it once more
	// failed is a show whose load just failed. It loads again after the
	// cursor left it or the server reported a change, not at once.
	failed int64
	// focused is whether the pane has the keys; cursor is its episode.
	focused bool
	cursor  int64
	offset  int
}

// previewID is the show the preview shows: the one under the cursor, or
// the show of the upcoming episode under it. Before the library is first
// drawn, the cursor is on the first row.
func (s *libraryScreen) previewID() (int64, bool) {
	if s.cat == catUpcoming {
		e, ok := s.cursorUpcoming()
		return e.SeriesID, ok
	}
	if sh, ok := s.cursorShow(); ok {
		return sh.ID, true
	}
	if rows, _ := s.rows(); len(rows) > 0 {
		return rows[0].ID, true
	}
	return 0, false
}

// wantPreview loads the show under the cursor unless it is loaded and
// current. One load runs at a time, so moving fast through the library
// asks for the show the cursor rests on, not every show it passed. It
// loads only while the preview shows; a resize that makes room for it
// loads it.
func (s *libraryScreen) wantPreview() tea.Cmd {
	id, ok := s.previewID()
	if s.pv.failed != id {
		s.pv.failed = 0
	}
	if !ok || !s.wide() || s.pv.asked != 0 || s.pv.fresh[id] || s.pv.failed == id || s.sess.client == nil {
		return nil
	}
	s.pv.gen++
	s.pv.asked = id
	cl, gen := s.sess.client, s.pv.gen
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		d, err := cl.Series(ctx, id)
		return libraryPreviewMsg{s, gen, id, d, err}
	}
}

// previewLoaded stores a show the preview asked for.
func (s *libraryScreen) previewLoaded(msg libraryPreviewMsg) tea.Cmd {
	if msg.gen != s.pv.gen || msg.id != s.pv.asked {
		return nil
	}
	s.pv.asked = 0
	if errors.Is(msg.err, client.ErrUnauthorized) {
		return func() tea.Msg { return disconnectedMsg{errRevoked} }
	}
	if s.pv.details == nil {
		s.pv.details, s.pv.errs, s.pv.fresh = map[int64]*api.SeriesDetail{}, map[int64]error{}, map[int64]bool{}
	}
	if s.pv.fresh == nil {
		s.pv.fresh = map[int64]bool{}
	}
	switch {
	case msg.err != nil:
		s.pv.errs[msg.id] = msg.err
		// A change reported while it loaded still loads it once more.
		if !s.pv.again {
			s.pv.failed = msg.id
		}
	default:
		delete(s.pv.errs, msg.id)
		s.pv.details[msg.id] = &msg.detail
		s.pv.fresh[msg.id] = !s.pv.again
	}
	s.pv.again = false
	return s.wantPreview()
}

// previewStale marks every loaded show for loading again, after the
// server reported a change.
func (s *libraryScreen) previewStale() tea.Cmd {
	s.pv.fresh, s.pv.failed = nil, 0
	s.pv.again = s.pv.asked != 0
	return s.wantPreview()
}

// previewDownload takes a changed download: its progress changes the
// shown show in place, anything else loads the show again.
func (s *libraryScreen) previewDownload(d api.Download) tea.Cmd {
	cur, _ := s.previewID()
	// A show loading for the first time may hold the download too.
	if s.pv.asked != 0 && s.pv.details[s.pv.asked] == nil {
		s.pv.again = true
	}
	for id, det := range s.pv.details {
		if !slices.ContainsFunc(det.Episodes, func(e api.Episode) bool { return e.ID == d.EpisodeID }) {
			continue
		}
		i := slices.IndexFunc(det.States, func(st api.EpisodeStatus) bool { return st.EpisodeID == d.EpisodeID })
		if s.pv.asked == id {
			// The load that runs may answer with older progress.
			s.pv.again = true
		}
		if id == cur && i >= 0 {
			if old := det.States[i].Download; old != nil && old.ID == d.ID && old.State == d.State && old.Import.State == d.Import.State {
				det.States[i].Download = &d
				continue
			}
		}
		delete(s.pv.fresh, id)
	}
	return s.wantPreview()
}

// previewShown is the loaded show the preview shows and its newest
// season.
func (s *libraryScreen) previewShown() (*api.SeriesDetail, season, bool) {
	id, ok := s.previewID()
	d := s.pv.details[id]
	if !ok || d == nil {
		return nil, season{}, false
	}
	seasons := (&showScreen{detail: d}).seasons()
	if len(seasons) == 0 {
		return d, season{}, false
	}
	return d, seasons[0], true
}

// wide reports whether the last drawn library had room for the preview.
func (s *libraryScreen) wide() bool {
	return s.width >= previewWidth && s.loaded && len(s.sess.shows) > 0
}

// focusPreview moves the keys into the pane, onto the episode the show's
// state comes from when it is in that season. It reports false when the
// pane has no episode to take the keys.
func (s *libraryScreen) focusPreview() bool {
	_, se, ok := s.previewShown()
	if !ok || len(se.episodes) == 0 {
		return false
	}
	s.pv.focused, s.pv.offset = true, 0
	s.pv.cursor = se.episodes[0].episode.ID
	if sh, ok := s.cursorShow(); ok && s.cat != catUpcoming && sh.Focus != nil {
		if slices.ContainsFunc(se.episodes, func(e episodeRow) bool { return e.episode.ID == sh.Focus.EpisodeID }) {
			s.pv.cursor = sh.Focus.EpisodeID
		}
	}
	if e, ok := s.cursorUpcoming(); ok && s.cat == catUpcoming {
		if slices.ContainsFunc(se.episodes, func(r episodeRow) bool { return r.episode.ID == e.EpisodeID }) {
			s.pv.cursor = e.EpisodeID
		}
	}
	return true
}

// previewKey handles a key while the pane has the keys.
func (s *libraryScreen) previewKey(key string) tea.Cmd {
	_, se, ok := s.previewShown()
	if !ok || !s.wide() {
		s.pv.focused = false
		return nil
	}
	i := max(0, slices.IndexFunc(se.episodes, func(e episodeRow) bool { return e.episode.ID == s.pv.cursor }))
	move := func(n int) {
		s.pv.cursor = se.episodes[min(len(se.episodes)-1, max(0, i+n))].episode.ID
	}
	if s.keys.key(key, max(1, s.page-5), move) {
		return nil
	}
	switch key {
	case "tab", "shift+tab", "esc":
		s.pv.focused = false
	case "1", "2", "3", "4":
		s.pv.focused = false
		s.cat = category(key[0] - '1')
	case "enter":
		return s.openPreviewEpisode(se.episodes[i])
	case "q":
		return tea.Quit
	}
	return nil
}

// openPreviewEpisode opens an episode of the pane as the Show screen's
// enter would, with the Show screen behind it.
func (s *libraryScreen) openPreviewEpisode(e episodeRow) tea.Cmd {
	d, _, ok := s.previewShown()
	if !ok {
		return nil
	}
	show := newShow(s.sess, s, d.Series.ID, cmp.Or(d.Series.NameEng, d.Series.Name), "")
	show.setDetail(*d)
	if folded := s.sess.folded[d.Series.ID]; folded != nil {
		folded[e.pos.Season] = false
	}
	show.cursor = rowKey{e.pos.Season, e.episode.ID}
	if cmd := show.open(e); cmd != nil {
		return cmd
	}
	// An episode not aired yet has nothing to open; its show says so.
	return func() tea.Msg { return switchMsg{show} }
}

// previewLines draws the pane, width cells wide and at most height rows.
func (s *libraryScreen) previewLines(width, height int, now time.Time) []string {
	id, ok := s.previewID()
	if !ok {
		return nil
	}
	d := s.pv.details[id]
	if d == nil {
		if err := s.pv.errs[id]; err != nil {
			return []string{"   " + gFailed + " " + sRed.Render(ansi.Truncate(clean(err.Error()), width-5, "…"))}
		}
		return []string{"   " + sMuted.Render("Loading…")}
	}
	typ := "TV"
	if d.Series.Type == api.TypeAnime {
		typ = "Anime"
	}
	facts := []string{typ}
	if d.Series.Year > 0 {
		facts = append(facts, strconv.Itoa(d.Series.Year))
	}
	if st := status(d.Series.Status); st != "" {
		facts = append(facts, st)
	}
	if d.Summary.NextAired != "" {
		facts = append(facts, "next episode "+day(d.Summary.NextAired, now))
	}
	name := clean(cmp.Or(d.Series.NameEng, d.Series.Name))
	lines := []string{
		"   " + sBold.Render(name) + "   " + sMuted.Render(strings.Join(facts, " · ")),
		"   " + fact("Profile", clean(d.Profile)) + "   " + sMuted.Render(clean(d.Rules)),
	}
	_, se, ok := s.previewShown()
	if !ok {
		return cut(lines, width)
	}
	title := "Season " + strconv.Itoa(se.number)
	if se.number == 0 {
		title = "Specials"
	}
	lines = append(lines, "", "   "+sBold.Render(title)+"   "+sMuted.Render(seasonCounts(se)))
	lines = cut(lines, width)

	// The episodes: as many as fit, each with its rejected attempts,
	// around the cursor when the pane has the keys; a row each says what
	// is above or below when there is room for them. The episode under
	// the cursor shows even in a short pane.
	eps := se.episodes
	cur := -1
	if s.pv.focused {
		cur = slices.IndexFunc(eps, func(e episodeRow) bool { return e.episode.ID == s.pv.cursor })
	}
	if cur >= 0 && len(lines) >= height {
		lines = lines[:max(0, height-1)]
	}
	room := max(1, height-len(lines))
	heights := make([]int, len(eps))
	total := 0
	for i, e := range eps {
		heights[i] = 1 + len(previewRejected(e.status))
		total += heights[i]
	}
	// span counts the rows of the episodes from a up to b.
	span := func(a, b int) int {
		n := 0
		for _, h := range heights[a:b] {
			n += h
		}
		return n
	}
	avail, marks := total, false
	if total > room {
		avail, marks = room, room >= 3
		if marks {
			avail -= 2 // a row each for ↑ and ↓
		}
	}
	first := 0
	if cur >= 0 {
		first = min(s.pv.offset, cur)
		for first < cur && span(first, cur+1) > avail {
			first++
		}
	}
	last := first
	for last < len(eps) && (last == first || span(first, last+1) <= avail) {
		last++
	}
	for first > 0 && span(first-1, last) <= avail {
		first--
	}
	s.pv.offset = first
	labelW, titleW := len("S00E00"), 0
	for _, e := range eps {
		labelW = max(labelW, ansi.StringWidth(episodeLabel(e)))
		titleW = max(titleW, ansi.StringWidth(clean(cmp.Or(e.episode.NameEng, e.episode.Name))))
	}
	// Margin 3, glyph 1, gaps 3 each; the state keeps 36 cells, room for
	// the strip, the progress and the attempt.
	titleW = max(10, min(titleW, width-3-1-3-labelW-3-3-36))
	if marks && first > 0 {
		lines = append(lines, "   "+sSubtle.Render("↑ "+strconv.Itoa(first)+" more"))
	}
	for i := first; i < last; i++ {
		lines = append(lines, previewRow(eps[i], d, labelW, titleW, width, now, i == cur))
		for _, r := range previewRejected(eps[i].status) {
			lines = append(lines, previewRejectedRow(r, width))
		}
	}
	if marks && last < len(eps) {
		lines = append(lines, "   "+sSubtle.Render("↓ "+strconv.Itoa(len(eps)-last)+" more"))
	}
	return lines
}

func previewRow(e episodeRow, d *api.SeriesDetail, labelW, titleW, width int, now time.Time, selected bool) string {
	cur := cursor(selected)
	bg := cur.bg
	gap := bg(lipgloss.NewStyle()).Render("   ")
	main := sNormal
	if e.status.State == api.EpisodeImported || e.status.State == "" {
		main = sSubtle
	}
	glyph, glyphStyle := episodeGlyph(e.status.State)
	line := cur.margin() + bg(glyphStyle).Render(glyph) + gap +
		bg(main).Render(fit(episodeLabel(e), labelW, false)) + gap +
		bg(main).Render(fit(clean(cmp.Or(e.episode.NameEng, e.episode.Name)), titleW, false)) + gap
	line += renderParts(episodeParts(e, d, now), max(0, width-lipgloss.Width(line)), bg)
	return cur.fill(line, width)
}

// previewRejected lists the rejected attempts the pane hangs under an
// episode: those of the run of a download in flight.
func previewRejected(st api.EpisodeStatus) []api.Download {
	switch st.State {
	case api.EpisodeDownloading, api.EpisodeProcessing, api.EpisodeImporting:
		if st.Download != nil {
			return st.Rejected
		}
	}
	return nil
}

// previewRejectedRow hangs a rejected attempt under its episode, its text
// under the episode labels, as Activity does.
func previewRejectedRow(d api.Download, width int) string {
	lead := "     " + sSubtle.Render("└ ")
	return lead + renderParts(rejectedParts(d), max(0, width-lipgloss.Width(lead)), func(s lipgloss.Style) lipgloss.Style { return s })
}

// cut keeps lines within width cells.
func cut(lines []string, width int) []string {
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, width, "…")
	}
	return lines
}

// besidePane puts the pane right of a table drawn tableWidth wide: the
// table's lines after its first skip, the column names, then a rule in
// the border colour between them. The rule under the column names spans
// the full width.
func (s *libraryScreen) besidePane(body string, skip, tableWidth, width int, now time.Time) string {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if len(lines) < skip {
		return body
	}
	if skip > 0 {
		lines[skip-1] = "   " + sBorder.Render(strings.Repeat("─", max(0, width-6)))
	}
	pane := s.previewLines(width-tableWidth-2, s.page, now)
	rows := lines[skip:]
	out := slices.Clone(lines[:skip])
	for i := 0; i < max(len(rows), len(pane)) && i < s.page; i++ {
		left, right := "", ""
		if i < len(rows) {
			left = ansi.Truncate(rows[i], tableWidth, "")
		}
		if i < len(pane) {
			right = pane[i]
		}
		out = append(out, pad(left, tableWidth)+sBorder.Render("│")+" "+right)
	}
	return strings.Join(out, "\n")
}
