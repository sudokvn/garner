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
	"github.com/charmbracelet/x/ansi"

	"github.com/trucke/garner/internal/api"
	"github.com/trucke/garner/internal/client"
)

// Library lists the shows with what garner has of each.

type category int

// The library's tabs. Movies get one once garner manages them.
const (
	catAll category = iota
	catTV
	catAnime
	categories // how many there are
)

type sortKey int

const (
	byState sortKey = iota
	byName
	byEpisodes
	bySize
	byStatus
	sortKeys
)

// libraryMsg answers one library's reload; from tells the app which.
type libraryMsg struct {
	from  *libraryScreen
	shows []api.Show
	err   error
}

type libraryScreen struct {
	sess     *session
	loaded   bool
	loading  bool
	dirty    bool // an event arrived while loading; load again
	cat      category
	sort     sortKey
	reverse  bool // the sort column's order flipped
	filter   string
	typing   bool // the filter prompt has the keys
	selected int64
	offset   int
	page     int // table rows on screen, from the last view
	flash    flash
	lastKey  string
	// The delete question for the show under the cursor, and the show
	// being deleted.
	asking   bool
	choice   int
	deleting string // its name
	busy     string // what runs, such as "Deleting Harbor Precinct…"
}

func newLibrary(sess *session) *libraryScreen {
	return &libraryScreen{sess: sess, page: 20}
}

// init loads the library when returning from another screen. On the
// first start it waits for the event stream: connecting loads it.
func (s *libraryScreen) init() tea.Cmd {
	if !s.loaded {
		return nil
	}
	return s.reload()
}

// showAdded selects a show just added, wherever the filter and tabs were,
// for when the user returns from the show.
func (s *libraryScreen) showAdded(msg addedMsg) tea.Cmd {
	s.cat, s.filter, s.typing, s.selected = catAll, "", false, msg.id
	return s.reload()
}

// showDeleted reports a deleted show.
func (s *libraryScreen) showDeleted(msg deletedMsg) tea.Cmd {
	text := "Deleted " + msg.name
	switch {
	case msg.deleted.Kept > 0:
		text = fmt.Sprintf("Removed %s; its %s, %s, stay in the library", msg.name, plural(msg.deleted.Kept, "file"), size(msg.deleted.Bytes))
	case msg.deleted.Files > 0:
		text += fmt.Sprintf(" and %s, %s", plural(msg.deleted.Files, "file"), size(msg.deleted.Bytes))
	}
	s.flash.info(gDone + " " + sNormal.Render(text))
	if n := len(msg.deleted.Problems); n > 0 {
		s.flash.stay(gFailed + " " + sNormal.Render(text+fmt.Sprintf("; %d files could not be removed: ", n)) +
			sRed.Render(clean(msg.deleted.Problems[0])))
	}
	return s.reload()
}

func (s *libraryScreen) reload() tea.Cmd {
	if s.loading {
		s.dirty = true
		return nil
	}
	s.loading = true
	cl := s.sess.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		shows, err := cl.Library(ctx)
		return libraryMsg{s, shows, err}
	}
}

func (s *libraryScreen) update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case libraryMsg:
		s.loading = false
		switch {
		case errors.Is(msg.err, client.ErrUnauthorized):
			return s, func() tea.Msg { return disconnectedMsg{errRevoked} }
		case msg.err != nil:
			if !s.sess.offline {
				s.flash.stay(sRed.Render(clean(msg.err.Error())))
			}
		default:
			s.sess.shows, s.loaded = msg.shows, true
		}
		if s.dirty {
			s.dirty = false
			return s, s.reload()
		}
		var waiting []*api.Download
		for _, sh := range s.sess.shows {
			if sh.Focus != nil {
				waiting = append(waiting, sh.Focus.Download)
			}
		}
		return s, redrawWhenSlow(waiting...)
	case eventMsg:
		switch msg.Type {
		case client.EventConnected, api.EventSeriesUpdated, api.EventSeriesDeleted, api.EventSearchFinished:
			return s, s.reload()
		case api.EventDownloadUpdated:
			var d api.Download
			if json.Unmarshal(msg.Data, &d) != nil || !s.patch(d) {
				return s, s.reload()
			}
		}
	case deleteResultMsg:
		name := s.deleting
		s.deleting, s.busy = "", ""
		switch {
		case errors.Is(msg.err, client.ErrUnauthorized):
			return s, func() tea.Msg { return disconnectedMsg{errRevoked} }
		case msg.err != nil:
			s.flash.stay(gFailed + " " + sRed.Render("Could not delete "+name+": "+clean(msg.err.Error())))
			return s, nil
		}
		return s, s.showDeleted(deletedMsg{name, msg.deleted})
	case tea.PasteMsg:
		if s.typing {
			s.filter += clean(msg.Content)
		}
	case tea.KeyPressMsg:
		return s, s.key(msg)
	}
	return s, nil
}

// patch updates a show's running download in place when only its progress
// changed. It reports false when the library needs loading again.
func (s *libraryScreen) patch(d api.Download) bool {
	for _, sh := range s.sess.shows {
		if f := sh.Focus; f != nil && f.Download != nil && f.Download.ID == d.ID {
			if f.Download.State != d.State || f.Download.Import.State != d.Import.State || !f.Download.Acknowledged.Equal(d.Acknowledged) {
				return false
			}
			f.Download = &d
			return true
		}
	}
	// A download the summary does not show yet, such as a new one.
	return false
}

func (s *libraryScreen) key(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if s.deleting != "" {
		return nil
	}
	s.flash.clear()
	if s.asking {
		return s.answer(key)
	}
	if s.typing {
		switch key {
		case "enter":
			s.typing = false
		case "esc":
			s.typing, s.filter = false, ""
		case "backspace":
			if r := []rune(s.filter); len(r) > 0 {
				s.filter = string(r[:len(r)-1])
			}
		case "ctrl+u":
			s.filter = ""
		case "up":
			s.move(-1)
		case "down":
			s.move(1)
		default:
			if msg.Text != "" && msg.Mod == 0 || msg.Text != "" && msg.Mod == tea.ModShift {
				s.filter += clean(msg.Text)
			}
		}
		return nil
	}

	prev := s.lastKey
	s.lastKey = key
	switch key {
	case "j", "down":
		s.move(1)
	case "k", "up":
		s.move(-1)
	case "g":
		if prev == "g" {
			s.move(-1 << 30)
			s.lastKey = ""
		}
	case "G":
		s.move(1 << 30)
	case "ctrl+d":
		s.move(max(1, s.page/2))
	case "ctrl+u":
		s.move(-max(1, s.page/2))
	case "tab":
		s.cat = (s.cat + 1) % categories
	case "shift+tab":
		s.cat = (s.cat + categories - 1) % categories
	case "1", "2", "3":
		s.cat = category(key[0] - '1')
	case "s":
		s.sort, s.reverse = (s.sort+1)%sortKeys, false
	case "I":
		s.reverse = !s.reverse
	case "/":
		s.typing = true
	case "esc":
		s.filter = ""
	case "enter":
		if sh, ok := s.cursorShow(); ok {
			next := newShow(s.sess, s, sh.ID, showName(sh), "")
			return func() tea.Msg { return switchMsg{next} }
		}
	case "a":
		next := newAddShow(s.sess, s)
		return func() tea.Msg { return switchMsg{next} }
	case "i":
		if sh, ok := s.cursorShow(); ok {
			next := newShowDetails(s.sess, newShow(s.sess, s, sh.ID, showName(sh), ""), s)
			return func() tea.Msg { return switchMsg{next} }
		}
	case "d":
		if _, ok := s.cursorShow(); ok {
			s.asking, s.choice = true, 0 // the safe choice: cancel
		}
	case "q":
		return tea.Quit
	}
	return nil
}

// answer handles the delete question.
func (s *libraryScreen) answer(key string) tea.Cmd {
	sh, ok := s.cursorShow()
	if !ok {
		s.asking = false
		return nil
	}
	done, a := deleteKey(key, sh, &s.choice)
	if done {
		s.asking = false
	}
	if !a.delete {
		return nil
	}
	s.deleting, s.busy = showName(sh), a.busy(showName(sh))
	return deleteShow(s.sess.client, sh.ID, a.keep)
}

func (s *libraryScreen) cursorShow() (api.Show, bool) {
	rows, _ := s.rows()
	i := slices.IndexFunc(rows, func(sh api.Show) bool { return sh.ID == s.selected })
	if i < 0 {
		return api.Show{}, false
	}
	return rows[i], true
}

// move shifts the cursor by n rows within the visible rows.
func (s *libraryScreen) move(n int) {
	rows, _ := s.rows()
	if len(rows) == 0 {
		return
	}
	i := max(0, slices.IndexFunc(rows, func(sh api.Show) bool { return sh.ID == s.selected }))
	s.selected = rows[min(len(rows)-1, max(0, i+n))].ID
}

// rows are the shows of the current category that match the filter, in
// the chosen order; total counts the category.
func (s *libraryScreen) rows() (rows []api.Show, total int) {
	needle := []rune(strings.ToLower(s.filter))
	for _, sh := range s.sess.shows {
		if !s.cat.holds(sh) {
			continue
		}
		total++
		if len(needle) == 0 || fuzzy(needle, sh.Name) || fuzzy(needle, sh.NameEng) || fuzzy(needle, sh.NameDeu) {
			rows = append(rows, sh)
		}
	}
	slices.SortStableFunc(rows, func(a, b api.Show) int {
		byNames := cmp.Compare(strings.ToLower(showName(a)), strings.ToLower(showName(b)))
		var c int
		switch s.sort {
		case byName:
			c = byNames
		case byState:
			c = cmp.Compare(rank(a), rank(b))
		case byEpisodes:
			c = cmp.Compare(b.Aired-b.Imported, a.Aired-a.Imported)
		case bySize:
			c = cmp.Compare(b.Bytes, a.Bytes)
		case byStatus:
			c = cmp.Compare(status(a.Status), status(b.Status))
		}
		if s.reverse {
			c = -c
		}
		return cmp.Or(c, byNames)
	})
	return rows, total
}

func (c category) holds(sh api.Show) bool {
	switch c {
	case catTV:
		return sh.Type != api.TypeAnime
	case catAnime:
		return sh.Type == api.TypeAnime
	}
	return true
}

// fuzzy reports whether needle's letters appear in s in order.
func fuzzy(needle []rune, s string) bool {
	if s == "" {
		return false
	}
	i := 0
	for _, r := range strings.ToLower(s) {
		if i < len(needle) && r == needle[i] {
			i++
		}
	}
	return i == len(needle)
}

var stateRank = map[api.EpisodeState]int{
	api.EpisodeFailed: 0, api.EpisodeAttention: 1, api.EpisodeDownloading: 2, api.EpisodeProcessing: 3,
	api.EpisodeImporting: 4, api.EpisodeWaiting: 5, api.EpisodeNotSearched: 5, api.EpisodeUpcoming: 6,
}

// rank orders shows for the state sort: needs you first, complete last. A
// state this client does not know, say from a newer server, comes just
// before complete.
func rank(sh api.Show) int {
	if sh.Focus == nil {
		return 9
	}
	if r, ok := stateRank[sh.Focus.State]; ok {
		return r
	}
	return 8
}

func showName(sh api.Show) string { return clean(cmp.Or(sh.NameEng, sh.Name)) }

const libraryTop = 6 // rows above the table: header, blank, tabs, blank, column names, rule

func (s *libraryScreen) view(width, height int) string {
	rows, total := s.rows()
	s.page = max(1, height-libraryTop-barHeight)
	cursor := max(0, slices.IndexFunc(rows, func(sh api.Show) bool { return sh.ID == s.selected }))
	if len(rows) > 0 {
		s.selected = rows[cursor].ID
	}
	s.offset = min(max(s.offset, cursor-s.page+1), cursor)

	var b strings.Builder
	b.WriteString(header(width, s.sess, "garner", "Library") + "\n\n")
	b.WriteString("   " + s.tabs() + "\n\n")
	cols := s.columns(width)
	b.WriteString(cols.head(s.sort, s.reverse) + "\n")
	b.WriteString("   " + sBorder.Render(strings.Repeat("─", max(0, width-6))) + "\n")

	switch {
	case !s.loaded && s.sess.offline:
		b.WriteString("   " + sMuted.Render("garner at "+clean(s.sess.client.Addr())+" does not answer. Trying again."))
	case !s.loaded:
		b.WriteString("   " + sMuted.Render("Loading the library…"))
	case len(s.sess.shows) == 0:
		b.WriteString(s.welcome(width))
	case len(rows) == 0 && s.filter != "":
		b.WriteString("   " + sMuted.Render("No show matches "+strconv.Quote(s.filter)+"."))
	case len(rows) == 0:
		b.WriteString("   " + sMuted.Render("No shows in this category."))
	default:
		end := min(len(rows), s.offset+s.page)
		for i := s.offset; i < end; i++ {
			b.WriteString(cols.row(rows[i], i == cursor) + "\n")
		}
	}
	page := screenLayout(width, height, b.String(), s.bottom(width, len(rows), total))
	if sh, ok := s.cursorShow(); ok && s.asking {
		return overlay(page, deleteQuestion(showName(sh), sh, s.choice), width, height)
	}
	return page
}

// welcome is what an empty library shows: how to add a show, under
// garner's mark when the terminal shows it and the table has room for it.
// Two blank rows above and below the mark are the brand's free space
// (docs/brand).
func (s *libraryScreen) welcome(width int) string {
	msg := sMuted.Render("No shows yet. Press ") + sKey.Render("a") + sMuted.Render(" to add one.")
	if !s.sess.mark || s.page < 2+len(markLines)+2+1 {
		return "   " + msg
	}
	w := min(width, designWidth)
	var b strings.Builder
	b.WriteString("\n\n")
	for _, row := range markRows() {
		b.WriteString(strings.Repeat(" ", max(0, (w-markWidth)/2)) + row + "\n")
	}
	b.WriteString("\n\n" + strings.Repeat(" ", max(0, (w-lipgloss.Width(msg))/2)) + msg)
	return b.String()
}

func (s *libraryScreen) tabs() string {
	counts := [categories]int{}
	for _, sh := range s.sess.shows {
		for c := range categories {
			if c.holds(sh) {
				counts[c]++
			}
		}
	}
	names := [categories]string{"All", "TV", "Anime"}
	parts := make([]string, categories)
	for c := range categories {
		label := sMuted.Render(names[c])
		if c == s.cat {
			label = sBold.Render(names[c])
		}
		parts[c] = label + " " + sMuted.Render(strconv.Itoa(counts[c]))
	}
	return strings.Join(parts, "      ")
}

func (s *libraryScreen) bottom(width, shown, total int) string {
	switch {
	case s.typing:
		left := sNormal.Render("/"+s.filter) + lipgloss.NewStyle().Background(cFg).Render(" ")
		right := sMuted.Render(fmt.Sprintf("%d of %d", shown, total))
		return left + strings.Repeat(" ", max(1, width-2-lipgloss.Width(left)-lipgloss.Width(right))) + right
	case s.busy != "":
		return sMuted.Render(s.busy)
	case s.flash.text != "":
		return s.flash.text
	case s.loaded && len(s.sess.shows) == 0:
		return keyBar("a", "add show", "?", "help", "q", "quit")
	case s.filter != "":
		return keyBar("j k", "move", "enter", "open", "i", "details", "a", "add", "/", "filter",
			"esc", "clear filter", "P", "profiles", "?", "help", "q", "quit")
	}
	// d, s and tab category are in Help; the tabs show at the top.
	return keyBar("j k", "move", "enter", "open", "i", "details", "a", "add", "/", "filter",
		"P", "profiles", "A", "activity", "?", "help", "q", "quit")
}

// columns holds the table's column widths: glyph, show, episodes, size,
// status, then state takes the rest.
type columns struct {
	width                         int
	name, episodes, size, statusW int
	state                         int
}

func (s *libraryScreen) columns(width int) columns {
	c := columns{width: width, name: len("Show") + 2, episodes: len("Episodes") + 2, size: len("Size") + 2, statusW: len("Status") + 2}
	for _, sh := range s.sess.shows {
		c.name = max(c.name, ansi.StringWidth(showName(sh)))
		c.episodes = max(c.episodes, len(episodes(sh)))
		c.size = max(c.size, len(size(sh.Bytes)))
		c.statusW = max(c.statusW, len(status(sh.Status)))
	}
	// Margins and gaps: 3 + glyph 1 + 3 + name + 3 + episodes + 3 + size
	// + 3 + status + 3 + state + 3.
	fixed := 3 + 1 + 3 + 3 + c.episodes + 3 + c.size + 3 + c.statusW + 3 + 3
	// Narrow: the names keep room, State gives way, then Status goes.
	state := 24
	if width-fixed-state < min(c.name, minName) {
		fixed, c.statusW, state = fixed-3-c.statusW, 0, 16
	}
	c.name = max(12, min(c.name, width-fixed-state))
	c.state = max(0, width-fixed-c.name)
	return c
}

func (c columns) head(sort sortKey, reverse bool) string {
	arrow := "▾"
	if reverse {
		arrow = "▴"
	}
	label := func(name string, k sortKey) (string, lipgloss.Style) {
		if k == sort {
			return name + " " + arrow, sNormal
		}
		return name, sSubtle
	}
	glyph := " "
	if sort == byState {
		glyph = sNormal.Render(arrow)
	}
	field := func(name string, k sortKey, w int, right bool) string {
		t, st := label(name, k)
		return st.Render(fit(t, w, right))
	}
	status := ""
	if c.statusW > 0 {
		status = field("Status", byStatus, c.statusW, false) + "   "
	}
	return "   " + glyph + "   " + field("Show", byName, c.name, false) + "   " + field("Episodes", byEpisodes, c.episodes, true) +
		"   " + field("Size", bySize, c.size, true) + "   " + status + sSubtle.Render("State")
}

func (c columns) row(sh api.Show, selected bool) string {
	bg := func(st lipgloss.Style) lipgloss.Style {
		if selected {
			return st.Background(cSelect)
		}
		return st
	}
	gap := bg(lipgloss.NewStyle()).Render("   ")
	main, numbers, statusStyle := sNormal, sNormal, sMuted
	if sh.Focus == nil {
		main, numbers, statusStyle = sSubtle, sSubtle, sSubtle
	}
	glyph, glyphStyle := showGlyph(sh)
	line := gap + bg(glyphStyle).Render(glyph) + gap +
		bg(main).Render(fit(showName(sh), c.name, false)) + gap +
		bg(numbers).Render(fit(episodes(sh), c.episodes, true)) + gap +
		bg(numbers).Render(fit(size(sh.Bytes), c.size, true)) + gap
	if c.statusW > 0 {
		line += bg(statusStyle).Render(fit(status(sh.Status), c.statusW, false)) + gap
	}
	line += renderParts(stateParts(sh, time.Now()), c.state, bg)
	if w := lipgloss.Width(line); w < c.width {
		line += bg(lipgloss.NewStyle()).Render(strings.Repeat(" ", c.width-w))
	}
	return line
}

func showGlyph(sh api.Show) (string, lipgloss.Style) {
	if sh.Focus == nil {
		return "✓", sMint
	}
	switch sh.Focus.State {
	case api.EpisodeFailed:
		return "✗", sRed
	case api.EpisodeAttention:
		return "▲", sAmber
	case api.EpisodeDownloading:
		return "◐", sAmber
	case api.EpisodeProcessing, api.EpisodeImporting:
		return "●", sAmber
	case api.EpisodeUpcoming:
		return "·", sSubtle
	}
	return "○", sSubtle
}

// stateParts describes what the show's most urgent episode is doing.
func stateParts(sh api.Show, now time.Time) []part {
	f := sh.Focus
	if f == nil {
		return nil
	}
	switch f.State {
	case api.EpisodeWaiting, api.EpisodeNotSearched:
		// One missing episode tells what its search found, several their count.
		if n := sh.Counts[api.EpisodeNotSearched] + sh.Counts[api.EpisodeWaiting]; n > 1 {
			return []part{{strconv.Itoa(n) + " missing", sMuted}}
		}
	case api.EpisodeUpcoming:
		if sh.NextAired != "" {
			return []part{{"next episode " + day(sh.NextAired, now), sMuted}}
		}
		return nil
	}
	return statusParts(f.State, f.Download, f.Summary, f.NextRetry, 0, now)
}

func episodes(sh api.Show) string { return fmt.Sprintf("%d/%d", sh.Imported, sh.Aired) }

func (s *libraryScreen) flashed() *flash { return &s.flash }

func (s *libraryScreen) captures() bool { return s.typing || s.asking || s.deleting != "" }

func (s *libraryScreen) help() (string, []string) {
	return "Library", []string{
		"enter", "open the show", "i", "show details",
		"a", "add a show", "d", "delete the show",
		"tab", "next category", "1 2 3", "jump to a category",
		"/", "filter, esc clears", "s", "sort by the next column",
		"I", "reverse the order", "q", "quit",
	}
}
