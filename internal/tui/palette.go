package tui

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sudokvn/garner/internal/api"
)

// The command palette is an overlay that : opens on any screen that is not
// capturing keys. With nothing typed it lists the screen's actions, taken
// from its help(), then what works anywhere. Typing filters them and adds
// shows, profiles and the open show's episodes.
//
// Choosing an action hands its key to the screen the way a key press
// arrives, so the palette has no second implementation of any action:
// deleting still asks its question.

// paletteWidth is the width of the palette's rows, borders excluded.
const paletteWidth = 64

// paletteKind is what an entry is; typed entries say it.
type paletteKind int

const (
	kindAction   paletteKind = iota // one of the screen's actions
	kindPlace                       // a place that works anywhere
	kindAnywhere                    // an action that works anywhere
	kindShow
	kindEpisode
	kindProfile
)

// paletteEntry is one row of the palette.
type paletteEntry struct {
	label string
	key   string // the key that does the same, or empty
	where string // where the key works, when not here, such as "in Help"
	kind  paletteKind
	note  string // what a thing is, such as "profile · 8 shows"
	run   func(a *app) tea.Cmd
}

type palette struct {
	typed    string
	choice   int // index into the selectable entries
	profiles []api.Profile
	page     int // rows shown, from the last lines
	keys     listKeys
}

// paletteProfilesMsg brings the profiles to the palette that asked.
type paletteProfilesMsg struct {
	from     *palette
	profiles []api.Profile
}

// openPalette opens the palette and loads the profiles for typing.
func (a *app) openPalette() tea.Cmd {
	p := &palette{}
	a.palette = p
	cl := a.sess.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		profiles, err := cl.Profiles(ctx)
		if err != nil {
			return nil // typing then finds no profiles
		}
		return paletteProfilesMsg{p, profiles}
	}
}

// paletteKey handles a key while the palette is open.
func (a *app) paletteKey(msg tea.KeyPressMsg) tea.Cmd {
	p := a.palette
	entries := a.paletteEntries()
	switch key := msg.String(); key {
	case "esc":
		a.palette = nil
	case "ctrl+p", "shift+tab":
		p.choice = max(0, p.choice-1)
	case "ctrl+n", "tab":
		p.choice = min(len(entries)-1, p.choice+1)
	case "up", "down", "pgup", "pgdown", "home", "end":
		// Letters are the field's; these keys still move.
		p.keys.key(key, p.page, func(n int) { p.choice = min(len(entries)-1, max(0, p.choice+n)) })
	case "enter":
		if p.choice < 0 || p.choice >= len(entries) {
			return nil
		}
		a.palette = nil
		return entries[p.choice].run(a)
	case "backspace":
		if r := []rune(p.typed); len(r) > 0 {
			p.setTyped(string(r[:len(r)-1]))
		}
	case "ctrl+u":
		p.setTyped("")
	default:
		if msg.Text != "" && (msg.Mod == 0 || msg.Mod == tea.ModShift) {
			p.setTyped(p.typed + clean(msg.Text))
		}
	}
	return nil
}

// setTyped sets the typed text; the cursor goes back to the best match.
func (p *palette) setTyped(s string) { p.typed, p.choice = s, 0 }

// press hands a key to the current screen as if it were pressed.
func (a *app) press(key string) tea.Cmd {
	_, cmd := a.update(keyPress(key))
	return cmd
}

// keyPress is the key press whose String is key: a single character, or a
// named key a help() entry uses.
func keyPress(key string) tea.KeyPressMsg {
	switch key {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	}
	r := []rune(key)
	if len(r) != 1 {
		return tea.KeyPressMsg{}
	}
	return tea.KeyPressMsg{Code: r[0], Text: key}
}

// paletteMoves are keys that move the cursor or the view, or choose a
// value next to it. help() entries with them are not actions worth a row.
var paletteMoves = map[string]bool{
	"j": true, "k": true, "h": true, "l": true, "g": true, "gg": true, "G": true, "K": true, "J": true,
	"tab": true, "shift+tab": true, "ctrl+d": true, "ctrl+u": true, "up": true, "down": true, "left": true, "right": true,
	"1": true, "2": true, "3": true, "4": true, "5": true, "6": true, "7": true, "8": true, "9": true,
	"esc": true,
}

// paletteGlobal are keys the app handles on every screen; the palette
// lists them under Anywhere.
var paletteGlobal = map[string]bool{"A": true, "P": true, "?": true, "n": true, ":": true}

// enterer is a screen whose help() lists what enter does in each place,
// such as on a season row or in the Library's preview. enterHelp is the
// label of the entry that enter does now, or "" when it does none.
type enterer interface{ enterHelp() string }

// screenActions are the screen's help() entries that do something: those
// whose keys neither move nor work everywhere. An entry with alternatives,
// such as "enter e", runs its first key. Of an enterer's enter entries,
// only the one enter does now is listed.
func screenActions(s screen) []paletteEntry {
	h, ok := s.(keyHelper)
	if !ok {
		return nil
	}
	enter, byPlace := "", false
	if e, ok := s.(enterer); ok {
		enter, byPlace = e.enterHelp(), true
	}
	_, pairs := h.help()
	var out []paletteEntry
	for i := 0; i+1 < len(pairs); i += 2 {
		keys := strings.Fields(pairs[i])
		if len(keys) == 0 || slices.ContainsFunc(keys, func(k string) bool { return paletteMoves[k] || paletteGlobal[k] }) {
			continue
		}
		if byPlace && keys[0] == "enter" && pairs[i+1] != enter {
			continue
		}
		key, label := keys[0], upperFirst(pairs[i+1])
		out = append(out, paletteEntry{label: label, key: key, kind: kindAction,
			run: func(a *app) tea.Cmd { return a.pressSaying(key, label) }})
	}
	return out
}

// pressSaying hands a key to the screen like press, and says so when it
// changed nothing: help() lists keys that need a row, such as cancelling
// a download, and the key does nothing elsewhere.
func (a *app) pressSaying(key, label string) tea.Cmd {
	// Every key clears the screen's notice, so that change is no action.
	if f, ok := a.screen.(flasher); ok {
		f.flashed().clear()
	}
	before, was := a.screen.view(a.width, a.height), a.screen
	cmd := a.press(key)
	if cmd != nil || a.screen != was || a.help || a.screen.view(a.width, a.height) != before {
		return cmd
	}
	if f, ok := a.screen.(flasher); ok {
		f.flashed().info(sMuted.Render(label + " does not apply here"))
	}
	return nil
}

// keepsWork reports whether the screen holds work not saved yet, which a
// palette entry must not drop by opening something that does not lead back.
func keepsWork(s screen) bool {
	switch s.(type) {
	case *editorScreen, *importPreviewScreen:
		return true
	}
	return false
}

// anywhere are the places and actions that work on every screen, without
// those that would do nothing here.
func (a *app) anywhere() []paletteEntry {
	out := []paletteEntry{{label: "Next episode that needs you", key: "n", kind: kindAnywhere,
		run: func(a *app) tea.Cmd { return a.press("n") }}}
	if _, there := a.screen.(*activityScreen); !there {
		out = append(out, paletteEntry{label: "Activity", key: "A", kind: kindPlace,
			run: func(a *app) tea.Cmd { return a.press("A") }})
	}
	switch a.screen.(type) {
	case *profilesScreen, *editorScreen, *importScreen, *importPreviewScreen:
	default:
		out = append(out, paletteEntry{label: "Profiles", key: "P", kind: kindPlace,
			run: func(a *app) tea.Cmd { return a.press("P") }})
	}
	if _, there := a.screen.(*libraryScreen); !there && !keepsWork(a.screen) {
		// a in the Library opens Add show from there; from elsewhere esc
		// leads back to where the palette was opened.
		out = append(out, paletteEntry{label: "Add a show", key: "a", where: "in the Library", kind: kindAnywhere,
			run: func(a *app) tea.Cmd {
				a.screen = newAddShow(a.sess, a.screen)
				return a.screen.init()
			}})
	}
	if _, there := a.screen.(*aboutScreen); !there {
		out = append(out, paletteEntry{label: "About garner", key: "a", where: "in Help", kind: kindPlace,
			run: func(a *app) tea.Cmd {
				a.help = true
				return a.helpKey("a")
			}})
	}
	return out
}

// things are what typing adds: the shows, the episodes of the open show
// and the profiles. Opening one leads back here with esc.
func (a *app) things() []paletteEntry {
	if keepsWork(a.screen) {
		return nil
	}
	var out []paletteEntry
	// The open show's episodes come before other shows of the same name.
	if show := openShow(a.screen); show != nil {
		for _, se := range show.seasons() {
			for _, e := range se.episodes {
				label := episodeLabel(e)
				if title := clean(cmp.Or(e.episode.NameEng, e.episode.Name)); title != "" {
					label += " " + title
				}
				key := rowKey{se.number, e.episode.ID}
				out = append(out, paletteEntry{label: label, kind: kindEpisode, note: "episode",
					run: func(a *app) tea.Cmd { return a.openEpisode(show, key) }})
			}
		}
	}
	for _, sh := range a.sess.shows {
		id, name := sh.ID, showName(sh)
		out = append(out, paletteEntry{label: name, kind: kindShow, note: "show",
			run: func(a *app) tea.Cmd {
				a.screen = newShow(a.sess, a.screen, id, name, "")
				return a.screen.init()
			}})
	}
	for _, p := range a.palette.profiles {
		id := p.ID
		uses := "no shows"
		if len(p.Shows) > 0 {
			uses = plural(len(p.Shows), "show")
		}
		out = append(out, paletteEntry{label: clean(p.Name), kind: kindProfile, note: "profile · " + uses,
			run: func(a *app) tea.Cmd {
				if s, ok := a.screen.(*profilesScreen); ok {
					s.selected = id
					return nil
				}
				s := newProfiles(a.sess, a.screen)
				s.selected = id
				a.screen = s
				return s.init()
			}})
	}
	return out
}

// openShow is the show the current screen belongs to, loaded; nil for
// screens of no show.
func openShow(s screen) *showScreen {
	var show *showScreen
	switch s := s.(type) {
	case *showScreen:
		show = s
	case *episodeScreen:
		show = s.show
	case *showDetailsScreen:
		show = s.show
	case *releasesScreen:
		show = s.back
	}
	if show == nil || show.detail == nil {
		return nil
	}
	return show
}

// openEpisode opens an episode as enter on its row of the Show screen
// does: its season unfolds and the cursor rests on it.
func (a *app) openEpisode(show *showScreen, key rowKey) tea.Cmd {
	if folded := a.sess.folded[show.id]; folded != nil {
		folded[key.season] = false
	}
	show.cursor = key
	a.screen = show
	return a.press("enter")
}

// paletteEntries are the selectable rows: with nothing typed the screen's
// actions, then what works anywhere; typed, every entry that matches, the
// best matches first.
func (a *app) paletteEntries() []paletteEntry {
	base := append(screenActions(a.screen), a.anywhere()...)
	needle := strings.ToLower(strings.TrimSpace(a.palette.typed))
	if needle == "" {
		return base
	}
	type scored struct {
		paletteEntry
		score int
	}
	var found []scored
	for _, e := range append(base, a.things()...) {
		if s, ok := matchScore(needle, e.label); ok {
			found = append(found, scored{e, s})
		}
	}
	slices.SortStableFunc(found, func(x, y scored) int { return cmp.Compare(x.score, y.score) })
	out := make([]paletteEntry, len(found))
	for i, f := range found {
		out[i] = f.paletteEntry
	}
	return out
}

// matchScore ranks how label matches needle, lower first: at the start of
// a word, inside a word, or its letters in order.
func matchScore(needle, label string) (int, bool) {
	l := strings.ToLower(label)
	i := strings.Index(l, needle)
	before, _ := utf8.DecodeLastRuneInString(l[:max(0, i)])
	switch {
	case i == 0 || i > 0 && !unicode.IsLetter(before) && !unicode.IsDigit(before):
		return 0, true
	case i > 0:
		return 1, true
	case fuzzy([]rune(needle), label):
		return 2, true
	}
	return 0, false
}

func upperFirst(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// paletteLines draws the palette for a screen width by height cells.
func (a *app) paletteLines(width, height int) []string {
	p := a.palette
	w := max(20, min(paletteWidth, width-10))
	// The field's cursor is a block, in reverse video without colour.
	block := lipgloss.NewStyle().Background(cFg)
	if colourless {
		block = lipgloss.NewStyle().Reverse(true)
	}
	field := sBold.Render("Go to or do") + "  " + sNormal.Render(p.typed) + block.Render(" ")
	if p.typed == "" {
		field += "   " + sMuted.Render("type a show, an episode or an action")
	}
	lines := []string{pad(ansi.Truncate(field, w, ""), w), ""}

	entries := a.paletteEntries()
	p.choice = min(max(0, p.choice), len(entries)-1)
	// Rows with their headers: a header is an index of -1.
	type row struct {
		header string
		entry  int
	}
	var rows []row
	actions := len(screenActions(a.screen))
	if p.typed == "" {
		if actions > 0 {
			rows = append(rows, row{header: "On this screen", entry: -1})
		}
		for i := range entries {
			if i == actions {
				if actions > 0 {
					rows = append(rows, row{entry: -1})
				}
				rows = append(rows, row{header: "Anywhere", entry: -1})
			}
			rows = append(rows, row{entry: i})
		}
	} else {
		for i := range entries {
			rows = append(rows, row{entry: i})
		}
	}
	if len(entries) == 0 {
		lines = append(lines, sMuted.Render("Nothing matches "+strconv.Quote(p.typed)+"."))
	}

	// The list keeps the rows it has with nothing typed, so the box and
	// its field stay in place while typing. Borders, the field, the key
	// bar and the blank lines take 7 rows; a window takes one more for
	// each mark.
	untyped := actions + len(a.anywhere()) + 1
	if actions > 0 {
		untyped += 2
	}
	room := min(max(3, height-7), max(3, untyped))
	first, last := 0, len(rows)
	if len(rows) > room {
		at := slices.IndexFunc(rows, func(r row) bool { return r.entry == p.choice })
		shown := room - 2
		first = min(max(0, at-shown/2), len(rows)-shown)
		last = first + shown
		lines = append(lines, sMuted.Render(mark(first > 0, " ↑")))
	}
	p.page = last - first
	for _, r := range rows[first:last] {
		switch {
		case r.entry < 0 && r.header == "":
			lines = append(lines, "")
		case r.entry < 0:
			lines = append(lines, sSubtle.Render(r.header))
		default:
			lines = append(lines, paletteRow(entries[r.entry], r.entry == p.choice, p.typed != "", w))
		}
	}
	if first > 0 || last < len(rows) {
		lines = append(lines, sMuted.Render(mark(last < len(rows), " ↓")))
	}
	for len(lines) < 2+room {
		lines = append(lines, "")
	}
	return append(lines, "", keyBar("↑ ↓", "choose", "enter", "do it", "esc", "close"))
}

// paletteRow draws one entry w cells wide: its label, then at the right
// where its key works and the key. Typed, it also says what the entry is.
func paletteRow(e paletteEntry, chosen, typed bool, w int) string {
	c := cursor(chosen)
	var notes []string
	if typed {
		switch e.kind {
		case kindAction, kindAnywhere:
			notes = append(notes, "action")
		case kindPlace:
			notes = append(notes, "place")
		default:
			notes = append(notes, e.note)
		}
	}
	if e.where != "" {
		notes = append(notes, e.where)
	}
	right := ""
	if len(notes) > 0 {
		right = strings.Join(notes, " · ")
		if e.key != "" {
			right += " · "
		}
	}
	rightWidth := ansi.StringWidth(right) + ansi.StringWidth(e.key) + 1
	label := ansi.Truncate(e.label, max(4, w-2-rightWidth-2), "…")
	line := c.option(sNormal, label)
	line += c.bg(lipgloss.NewStyle()).Render(strings.Repeat(" ", max(1, w-lipgloss.Width(line)-rightWidth)))
	line += c.bg(sMuted).Render(right) + c.bg(sKey).Render(e.key) + c.bg(lipgloss.NewStyle()).Render(" ")
	return line
}
