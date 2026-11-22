package tui

import (
	"image/color"
	"slices"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The palette; internal/tui/README.md lists the colours' uses. usePalette
// sets it and the styles and glyphs below.
var cFg, cMuted, cSubtle, cBorder, cSelect, cMint, cAmber, cRed, cIris, cFaded color.Color

var sNormal, sBold, sMuted, sSubtle, sBorder, sMint, sAmber, sRed, sKey, sCommand lipgloss.Style

// Glyphs, one set everywhere (see internal/tui/README.md).
var gDone, gWorking, gWaiting, gFailed string

// The dark set applies until the terminal tells its background.
func init() { usePalette(true, nil) }

// usePalette sets the palette for the terminal's background: the dark set
// on a dark one, the light set of the same roles on a light one. On a
// light background bg, the select background is derived from bg, so the
// cursor row stands out from that background by the same small step.
//
// The app calls it once the terminal answers, before it draws with the
// answer; nothing else may change the palette.
func usePalette(dark bool, bg color.Color) {
	ld := lipgloss.LightDark(dark)
	pick := func(light, dark string) color.Color { return ld(lipgloss.Color(light), lipgloss.Color(dark)) }
	cFg = pick("#1C2024", "#EDF1F2")
	cMuted = pick("#545B66", "#9199A3")
	cSubtle = pick("#6B7079", "#80848D")
	cBorder = pick("#C4C8CF", "#3D3D47")
	cSelect = lipgloss.Color("#242037")
	cMint = pick("#2D7A5E", "#75B39C")
	cAmber = pick("#A65400", "#F79926")
	cRed = pick("#C42B26", "#F54D47")
	cIris = pick("#4A50B8", "#9BA0E3")
	cFaded = pick("#C9CCD1", "#3A3A44")
	if !dark {
		if bg == nil {
			bg = lipgloss.Color("#FFFFFF")
		}
		cSelect = mix(bg, lipgloss.Color("#4B3FA8"), 0.14)
	}

	sNormal = lipgloss.NewStyle().Foreground(cFg)
	sBold = sNormal.Bold(true)
	sMuted = lipgloss.NewStyle().Foreground(cMuted)
	sSubtle = lipgloss.NewStyle().Foreground(cSubtle)
	sBorder = lipgloss.NewStyle().Foreground(cBorder)
	sMint = lipgloss.NewStyle().Foreground(cMint)
	sAmber = lipgloss.NewStyle().Foreground(cAmber)
	sRed = lipgloss.NewStyle().Foreground(cRed)
	sKey = sMint.Bold(true)
	sCommand = sBold.Background(cSelect)

	gDone = sMint.Render("✓")
	gWorking = sAmber.Render("●")
	gWaiting = sSubtle.Render("○")
	gFailed = sRed.Render("✗")
}

// mix blends colour b into a by share, from 0 to 1.
func mix(a, b color.Color, share float64) color.Color {
	ar, ag, ab, _ := a.RGBA()
	br, bgr, bb, _ := b.RGBA()
	blend := func(x, y uint32) uint8 {
		return uint8((float64(x>>8)*(1-share) + float64(y>>8)*share) + 0.5)
	}
	return color.RGBA{R: blend(ar, br), G: blend(ag, bgr), B: blend(ab, bb), A: 0xff}
}

// clean makes untrusted text, such as release names and server messages,
// safe to print: control characters (including the escape that starts
// terminal sequences) are removed, line breaks and tabs become spaces, and
// direction overrides that could make text read differently are dropped.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case unicode.IsControl(r), unicode.Is(unicode.Bidi_Control, r):
			return -1
		}
		return r
	}, s)
}

// pad fills styled text with spaces to width cells.
func pad(s string, width int) string {
	return s + strings.Repeat(" ", max(0, width-lipgloss.Width(s)))
}

// frame draws a framed pane: square corners in the border colour, the title
// uppercase in subtle cut into the top edge, two cells of padding. Lines
// wider than the pane are wrapped.
func frame(title string, lines []string, width int) string {
	inner := width - 6
	title = strings.ToUpper(title)
	top := sBorder.Render("┌─ ") + sSubtle.Render(title) + sBorder.Render(" "+strings.Repeat("─", max(0, width-5-ansi.StringWidth(title)))+"┐")
	out := []string{top}
	side := sBorder.Render("│")
	for _, l := range lines {
		for _, w := range strings.Split(ansi.Wrap(l, inner, ""), "\n") {
			out = append(out, side+"  "+pad(w, inner)+"  "+side)
		}
	}
	out = append(out, sBorder.Render("└"+strings.Repeat("─", width-2)+"┘"))
	return strings.Join(out, "\n")
}

// keyBar renders the bottom line's keys: the key in mint bold, its label
// in muted.
func keyBar(pairs ...string) string {
	parts := make([]string, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, sKey.Render(pairs[i])+" "+sMuted.Render(pairs[i+1]))
	}
	return joinKeys(parts...)
}

// joinKeys joins rendered keys and key bars into one key bar.
func joinKeys(parts ...string) string { return strings.Join(parts, keyGap) }

// keySlot renders a key whose action depends on the row under the cursor,
// so the keys after it keep their place as the cursor moves: its label is
// padded to the widest of labels, and a row without the action shows the
// key greyed out instead of leaving it out. The padding is no-break spaces,
// as fitKeys splits a key bar at plain ones.
func keySlot(key, label string, on bool, labels ...string) string {
	w := ansi.StringWidth(label)
	for _, l := range labels {
		w = max(w, ansi.StringWidth(l))
	}
	pad := strings.Repeat("\u00a0", w-ansi.StringWidth(label))
	if !on {
		return sSubtle.Render(key+" "+label) + pad
	}
	return sKey.Render(key) + " " + sMuted.Render(label) + pad
}

// minName is the room a table's name column keeps on a narrow terminal;
// other columns give way first.
const minName = 24

// keyGap separates the keys of a key bar.
const keyGap = "    "

// keyImportance ranks a key of the key bar; fitKeys leaves out the least
// important first. j k go first, as every screen moves with them. enter,
// esc and q, which act or leave, go last, and ? last of all: Help lists
// every key.
func keyImportance(part string) int {
	key, _, _ := strings.Cut(ansi.Strip(part), " ")
	switch key {
	case "j":
		return 0
	case "enter", "esc", "q":
		return 2
	case "?":
		return 3
	}
	return 1
}

// fitKeys makes a key bar fit width by leaving out whole keys, the least
// important first and, among equals, from the right. The keys that stay
// keep their order. Other lines are cut.
func fitKeys(bar string, width int) string {
	parts := strings.Split(bar, keyGap)
	if lipgloss.Width(bar) <= width || len(parts) < 2 {
		return ansi.Truncate(bar, width, "")
	}
	order := make([]int, len(parts))
	for i := range order {
		order[i] = len(parts) - 1 - i
	}
	slices.SortStableFunc(order, func(a, b int) int { return keyImportance(parts[a]) - keyImportance(parts[b]) })
	order = order[:len(order)-1] // the most important key stays
	left := map[int]bool{}
	join := func() string {
		var kept []string
		for i, p := range parts {
			if !left[i] {
				kept = append(kept, p)
			}
		}
		return strings.Join(kept, keyGap)
	}
	for _, i := range order {
		if lipgloss.Width(join()) <= width {
			break
		}
		left[i] = true
	}
	return ansi.Truncate(join(), width, "")
}

// factLines lays labelled facts out on one line, closer together when the
// line is narrow, and over more lines when they still do not fit.
func factLines(width int, facts ...string) string {
	room := width - 3
	for _, gap := range []string{"      ", "   "} {
		if line := strings.Join(facts, gap); lipgloss.Width(line) <= room {
			return "   " + line
		}
	}
	var lines []string
	for _, f := range facts {
		if n := len(lines); n > 0 && lipgloss.Width(lines[n-1]+"   "+f) <= room {
			lines[n-1] += "   " + f
		} else {
			lines = append(lines, f)
		}
	}
	return "   " + strings.Join(lines, "\n   ")
}

// breadcrumb renders the first row: garner › … › screen, the ends in bold.
func breadcrumb(parts ...string) string {
	out := make([]string, len(parts))
	for i, p := range parts {
		if i == 0 || i == len(parts)-1 {
			out[i] = sBold.Render(p)
		} else {
			out[i] = sMuted.Render(p)
		}
	}
	return strings.Join(out, sSubtle.Render(" › "))
}

// barHeight is the rows every screen keeps at the bottom: a rule, then
// the key bar.
const barHeight = 2

// screenLayout places body at the top and bottom on the last row, one cell
// in, under a rule that keeps a full screen apart from it. It cuts the body
// when the terminal is too short.
func screenLayout(width, height int, body, bottom string) string {
	room := max(0, height-barHeight)
	lines := strings.Split(body, "\n")
	if len(lines) > room {
		lines = lines[:room]
	}
	for len(lines) < room {
		lines = append(lines, "")
	}
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, width, "")
	}
	rule := sBorder.Render(strings.Repeat("─", max(0, width)))
	return strings.Join(append(lines, rule, " "+fitKeys(bottom, width-1)), "\n")
}

func indent(s string, n int) string {
	prefix := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}

// overlay draws a rounded box with lines centred over base, everything
// behind it in the faded colour.
func overlay(base string, lines []string, width, height int) string {
	boxWidth := 0
	for _, l := range lines {
		boxWidth = max(boxWidth, lipgloss.Width(l))
	}
	boxWidth = max(6, min(boxWidth+6, width)) // borders and two cells of padding, within the screen
	box := []string{sBorder.Render("╭" + strings.Repeat("─", boxWidth-2) + "╮")}
	for _, l := range lines {
		box = append(box, sBorder.Render("│")+"  "+pad(ansi.Truncate(l, boxWidth-6, "…"), boxWidth-6)+"  "+sBorder.Render("│"))
	}
	box = append(box, sBorder.Render("╰"+strings.Repeat("─", boxWidth-2)+"╯"))

	faded := lipgloss.NewStyle().Foreground(cFaded)
	rows := strings.Split(base, "\n")
	for len(rows) < height {
		rows = append(rows, "")
	}
	top := max(0, (height-len(box))/2-2)
	left := max(0, (width-boxWidth)/2)
	for y, row := range rows {
		plain := ansi.Strip(row)
		i := y - top
		if i < 0 || i >= len(box) {
			rows[y] = faded.Render(plain)
			continue
		}
		plain += strings.Repeat(" ", max(0, left-ansi.StringWidth(plain)))
		rows[y] = faded.Render(ansi.Truncate(plain, left, "")) + box[i] + faded.Render(ansi.TruncateLeft(plain, left+boxWidth, ""))
	}
	return strings.Join(rows, "\n")
}

// colourless is whether the terminal shows no colour, as with NO_COLOR.
// The app sets it from the terminal's colour profile.
var colourless bool

// cursor is whether a row or an option is the one under the cursor. Every
// table row, option and pick-list entry is drawn with it, so the cursor
// looks the same everywhere: the select background across the row and a
// bold mint > in its mark cell. Without colour, reverse video takes the
// background's place and the > stays.
//
// A table row starts with margin, styles each part with bg and ends with
// fill; an option starts with option.
type cursor bool

// bg adds the cursor's background to a style: the select background, or
// reverse video without colour. Off the cursor it changes nothing.
func (c cursor) bg(st lipgloss.Style) lipgloss.Style {
	switch {
	case !bool(c):
		return st
	case colourless:
		return st.Reverse(true)
	}
	return st.Background(cSelect)
}

// mark is the cell that carries the cursor's >, or a space.
func (c cursor) mark() string {
	if c {
		return c.bg(sKey).Render(">")
	}
	return " "
}

// margin is a table row's three cells before its first column, the mark
// in the middle one.
func (c cursor) margin() string {
	space := c.bg(lipgloss.NewStyle()).Render(" ")
	return space + c.mark() + space
}

// option starts an option of a question or a pick list: the mark, then
// text in st after a space, on the cursor's background.
func (c cursor) option(st lipgloss.Style, text string) string {
	return c.mark() + c.bg(st).Render(" "+text)
}

// fill pads a row to width, keeping the cursor's background across.
func (c cursor) fill(line string, width int) string {
	if w := lipgloss.Width(line); w < width {
		line += c.bg(lipgloss.NewStyle()).Render(strings.Repeat(" ", width-w))
	}
	return line
}

// choice renders one option of an overlay, marked when chosen.
func choice(label string, chosen bool) string {
	return cursor(chosen).option(sNormal, label+" ")
}

// flashFor is how long a success or info message stays in the bottom line.
const flashFor = 3 * time.Second

// flash is a message in the bottom line. Success and info messages go after
// flashFor; errors and work in progress stay until replaced or the next
// key, so there is time to read them.
type flash struct {
	text   string
	sticky bool
	seq    int // counts messages, so a timer clears only its own
	timed  int // the message a timer runs for
}

func (f *flash) info(text string) { f.text, f.sticky = text, false; f.seq++ }
func (f *flash) stay(text string) { f.text, f.sticky = text, true; f.seq++ }

func (f *flash) clear() {
	if f.text != "" {
		f.text = ""
		f.seq++
	}
}

// flasher is a screen with a bottom-line message.
type flasher interface{ flashed() *flash }

// flashDoneMsg ends a message's time, if it is still showing.
type flashDoneMsg struct {
	f   *flash
	seq int
}

// expire starts the timer for a new message that should go by itself.
func (f *flash) expire() tea.Cmd {
	if f.text == "" || f.sticky || f.timed == f.seq {
		return nil
	}
	f.timed = f.seq
	seq := f.seq
	return tea.Tick(flashFor, func(time.Time) tea.Msg { return flashDoneMsg{f, seq} })
}

// scroller moves through content taller than the screen: j k a line,
// ctrl+d u half a page, gg G to the ends. Screens with tables move their
// cursor instead.
type scroller struct {
	offset, page, max int
	last              string // the key before, for gg
}

// key moves the content for a scrolling key and reports whether it was one.
func (s *scroller) key(key string) bool {
	prev := s.last
	s.last = key
	switch key {
	case "j", "down":
		s.offset++
	case "k", "up":
		s.offset--
	case "ctrl+d", "pgdown":
		s.offset += max(1, s.page/2)
	case "ctrl+u", "pgup":
		s.offset -= max(1, s.page/2)
	case "G", "end":
		s.offset = s.max
	case "home":
		s.offset = 0
	case "g":
		if prev == "g" {
			s.offset, s.last = 0, ""
		}
	default:
		return false
	}
	s.offset = min(max(0, s.offset), s.max)
	return true
}

// window shows room of the lines from the offset; the first and last line
// say when there is more above or below.
func (s *scroller) window(lines []string, room int) []string {
	room = max(3, room)
	s.page, s.max = room, max(0, len(lines)-room)
	s.offset = min(max(0, s.offset), s.max)
	if s.max == 0 {
		return lines
	}
	view := slices.Clone(lines[s.offset : s.offset+room])
	if s.offset > 0 {
		view[0] = "   " + sSubtle.Render("↑ more above")
	}
	if s.offset < s.max {
		view[room-1] = "   " + sSubtle.Render("↓ more below · j k scroll")
	}
	return view
}

// layout is screenLayout for a screen whose content scrolls below its
// first two lines, the breadcrumb and a blank.
func (s *scroller) layout(width, height int, body, bottom string) string {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	const fixed = 2
	if len(lines) > fixed {
		lines = append(lines[:fixed:fixed], s.window(lines[fixed:], height-barHeight-fixed)...)
	}
	return screenLayout(width, height, strings.Join(lines, "\n"), bottom)
}

// designWidth is the width the screens are designed for, in cells.
const designWidth = 120

// pickItem is one row of a pickList: a choice, or a header the cursor
// skips. An empty header is a blank line.
type pickItem struct {
	label, note string
	header      bool
	value       any
}

// pickList is an overlay's list of choices with section headers, such as
// the new-profile picker and the language picker.
type pickList struct {
	items      []pickItem
	choice     int // index into items, never a header
	labelWidth int // the label column, gap included
	width      int // of a row, borders excluded
}

// key moves the cursor or answers: done closes the list, chosen is the
// index of the choice, or -1 when cancelled.
func (p *pickList) key(key string) (done bool, chosen int) {
	switch key {
	case "esc":
		return true, -1
	case "k", "up", "shift+tab":
		p.step(-1)
	case "j", "down", "tab":
		p.step(1)
	case "G", "end":
		p.choice = len(p.items)
		p.step(-1)
	case "enter":
		if p.choice >= 0 && p.choice < len(p.items) && !p.items[p.choice].header {
			return true, p.choice
		}
	}
	return false, -1
}

// step moves the cursor past headers in direction dir.
func (p *pickList) step(dir int) {
	for i := p.choice + dir; i >= 0 && i < len(p.items); i += dir {
		if !p.items[i].header {
			p.choice = i
			return
		}
	}
}

// lines draws the list for a screen height rows high. A list longer than
// fits shows a window around the cursor, with ↑ and ↓ where rows are
// hidden.
func (p *pickList) lines(title string, height int) []string {
	lines := []string{title, ""}
	first, last := 0, len(p.items)
	// The overlay's borders, the title, the key bar and their gaps take 6
	// rows; a window also takes one row for each mark.
	if rows := height - 6; len(p.items) > rows {
		shown := max(1, rows-2)
		first = min(max(0, p.choice-shown/2), len(p.items)-shown)
		last = first + shown
		lines = append(lines, sMuted.Render(mark(first > 0, " ↑")))
	}
	for i := first; i < last; i++ {
		it := p.items[i]
		switch {
		case it.header && it.label == "":
			lines = append(lines, "")
		case it.header:
			lines = append(lines, sSubtle.Render(it.label))
		default:
			c := cursor(i == p.choice)
			noteWidth := max(0, p.width-2-p.labelWidth)
			lines = append(lines, c.option(sNormal, ansi.Truncate(pad(it.label, p.labelWidth), p.width-2, ""))+
				c.bg(sMuted).Render(fit(it.note, noteWidth, false)))
		}
	}
	if first > 0 || last < len(p.items) {
		lines = append(lines, sMuted.Render(mark(last < len(p.items), " ↓")))
	}
	return append(lines, "", keyBar("j k", "choose", "enter", "ok", "esc", "cancel"))
}
