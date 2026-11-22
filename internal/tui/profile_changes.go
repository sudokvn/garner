package tui

import (
	"image/color"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sudokvn/garner/internal/profile"
)

// The changes pane takes the place of the profile editor's preferences
// while the profile differs from the one opened. Each changed setting has
// two rows: what it was, on a dark red background, and what it is now, on
// a dark green one, with the new parts in bold. Without colour the words
// "was" and "now" tell the rows apart.

// changeWord is the room of "was" and "now" before the value.
const changeWord = 5

// changeTint is the background of the "was" rows, or the "now" rows when
// now is set: red or mint blended into the terminal's background, so the
// row stays dark on a dark terminal and light on a light one. The
// terminal's exact colour is not kept, so black or white stands for it.
func changeTint(now bool) color.Color {
	base, share := color.Color(color.Black), 0.16
	if lightTerminal() {
		base, share = color.White, 0.12
	}
	if now {
		return mix(base, cMint, share)
	}
	return mix(base, cRed, share)
}

// lightTerminal reports whether the palette is the light set: its text
// is dark.
func lightTerminal() bool {
	r, g, b, _ := cFg.RGBA()
	return r+g+b < 3*0x8000
}

// changeRows are a setting's two rows: label on the first, then "was" and
// "now" with their values, each row filled to inner cells with its tint.
// Values that do not fit wrap under themselves.
func changeRows(label string, was, now []part, inner int) []string {
	var out []string
	for i, side := range []struct {
		word  string
		parts []part
	}{{"was", was}, {"now", now}} {
		tint := func(st lipgloss.Style) lipgloss.Style {
			if colourless {
				return st
			}
			return st.Background(changeTint(i == 1))
		}
		room := max(10, inner-settingWidth-changeWord)
		for j, line := range wrapParts(side.parts, room) {
			l, w := "", ""
			if j == 0 {
				w = side.word
				if i == 0 {
					l = label
				}
			}
			row := tint(sSubtle).Render(pad(l, settingWidth)) + tint(sMuted).Render(pad(w, changeWord))
			for _, p := range line {
				row += tint(p.style).Render(p.text)
			}
			if gap := inner - lipgloss.Width(row); gap > 0 {
				row += tint(lipgloss.NewStyle()).Render(strings.Repeat(" ", gap))
			}
			out = append(out, row)
		}
	}
	return out
}

// wrapParts lays styled parts out in lines of width cells. A part that
// fits a line stays whole, so "English 720p" is not split; a longer one
// breaks at spaces, and a word wider than a line is cut.
func wrapParts(parts []part, width int) [][]part {
	lines := [][]part{nil}
	used := 0
	for _, p := range parts {
		words := []string{p.text}
		if ansi.StringWidth(strings.TrimSpace(p.text)) > width {
			words = strings.SplitAfter(p.text, " ")
		}
		for _, word := range words {
			if word == "" {
				continue
			}
			w := ansi.StringWidth(strings.TrimRight(word, " "))
			if used > 0 && used+w > width {
				lines = append(lines, nil)
				used = 0
				word = strings.TrimLeft(word, " ")
			}
			if used == 0 && w > width {
				word = ansi.Truncate(word, width, "…")
			}
			n := len(lines) - 1
			lines[n] = append(lines[n], part{word, p.style})
			used += ansi.StringWidth(word)
		}
	}
	return lines
}

// listParts writes a list best first, joined by sep, with the entries of
// now that differ from was at their place in bold; was itself is plain.
func listParts(was, now []string, sep string, isNow bool) []part {
	if len(now) == 0 {
		return []part{{"none", sMuted}}
	}
	var parts []part
	for i, v := range now {
		if i > 0 {
			parts = append(parts, part{sep, sNormal})
		}
		st := sNormal
		if isNow && (i >= len(was) || was[i] != v) {
			st = sBold
		}
		parts = append(parts, part{v, st})
	}
	return parts
}

// textParts is a name or description: bold when it is the new value.
func textParts(v string, isNow bool) []part {
	if v == "" {
		return []part{{"none", sMuted}}
	}
	if isNow {
		return []part{{v, sBold}}
	}
	return []part{{v, sNormal}}
}

// prefsList is a profile's preferences best first, as prefsLine writes
// them.
func prefsList(p profile.Profile) []string {
	var out []string
	for _, c := range p.Preferences() {
		out = append(out, c.String())
	}
	return out
}

// changeLines are the changed settings of now against was, each as two
// rows, ending with the preferences they make when those change.
func changeLines(was, now profile.Profile, inner int) []string {
	var lines []string
	text := func(label, a, b string) {
		if a != b {
			lines = append(lines, changeRows(label, textParts(a, false), textParts(b, true), inner)...)
		}
	}
	list := func(label string, a, b []string, sep string) {
		if !slices.Equal(a, b) {
			lines = append(lines, changeRows(label, listParts(nil, a, sep, false), listParts(a, b, sep, true), inner)...)
		}
	}
	text("Name", was.Name, now.Name)
	text("Description", was.Description, now.Description)
	list("Audio", languageNames(was.Audio), languageNames(now.Audio), ", then ")
	list("Resolution", resolutionNames(was.Resolution), resolutionNames(now.Resolution), ", then ")
	for _, o := range profileOptions {
		text(o.label, o.get(was), o.get(now))
	}
	list("Preferences", prefsList(was), prefsList(now), " → ")
	return lines
}

// usedBy says how many shows use the profile, for the pane's title.
func usedBy(shows int) string {
	switch shows {
	case 0:
		return "no show uses this profile"
	case 1:
		return "1 show uses this profile"
	}
	return strconv.Itoa(shows) + " shows use this profile"
}

// changesLines fill the changes pane, inner cells wide: the changed
// settings, then what saving does.
func (s *editorScreen) changesLines(inner int) []string {
	lines := changeLines(s.was, s.p, inner)
	lines = append(lines, "")
	return append(lines, wrapLabelled("If you save", sNormal.Render(ifSaved(s.shows)), inner)...)
}

// ifSaved says what saving changes for the shows that use the profile:
// nothing until their next search.
func ifSaved(shows int) string {
	switch shows {
	case 0:
		return "no show follows it until you give one this profile. Nothing is searched now."
	case 1:
		return "the 1 show follows the new rules from its next search. Nothing is searched now."
	}
	return "all " + strconv.Itoa(shows) + " shows follow the new rules from their next search. Nothing is searched now."
}
