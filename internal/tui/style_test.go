package tui

import (
	"image/color"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestCleanUntrustedText checks that text from indexers and the server
// cannot drive the terminal or reorder what is shown.
func TestCleanUntrustedText(t *testing.T) {
	for in, want := range map[string]string{
		"Show.S01E01\x1b]52;c;ZXZpbA==\x07-EXAMPLE": "Show.S01E01]52;c;ZXZpbA==-EXAMPLE",
		"Show.S01E01\x1b[2J-EXAMPLE":                "Show.S01E01[2J-EXAMPLE",
		"Show.S01E01\u009b31m-EXAMPLE":              "Show.S01E0131m-EXAMPLE",
		"Show.\u202eLPMAXE-10E10S":                  "Show.LPMAXE-10E10S",
		"line one\nline two\ttab":                   "line one line two tab",
		"Laternenhafen – Nach dem Ende der Nacht":   "Laternenhafen – Nach dem Ende der Nacht",
	} {
		if got := clean(in); got != want {
			t.Errorf("clean(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCursorWithoutColour checks that the cursor stays visible in a
// terminal without colour, as with NO_COLOR, where a background colour is
// dropped: the row under the cursor and the chosen answer of a question
// carry the > and reverse video.
func TestCursorWithoutColour(t *testing.T) {
	colourless = true
	defer func() { colourless = false }()
	reverse := regexp.MustCompile(`\x1b\[([0-9;]*;)?7[;m]`)
	lineWith := func(view, text string) string {
		for _, l := range strings.Split(view, "\n") {
			if strings.Contains(ansi.Strip(l), text) {
				return l
			}
		}
		t.Fatalf("no line with %q in\n%s", text, ansi.Strip(view))
		return ""
	}

	_, cl := newRecorder(t)
	a, lib := libraryApp(cl, true)
	row := lineWith(lib.view(120, 36), "Harbor Precinct")
	if !strings.HasPrefix(ansi.Strip(row), " > ") || !reverse.MatchString(row) {
		t.Errorf("cursor row %q: want the > and reverse video", row)
	}

	sendKeys(a, "d")
	answers := lineWith(lib.view(120, 36), "Keep files")
	if !strings.Contains(ansi.Strip(answers), "> Cancel ") || !reverse.MatchString(answers) {
		t.Errorf("answers %q: want > on Cancel and reverse video", answers)
	}
}

// TestLightTerminal checks that on a terminal with a white background the
// text is dark and the cursor row has a light background of its own: the
// dark set's text is all but invisible on white.
func TestLightTerminal(t *testing.T) {
	defer usePalette(true, nil)
	_, cl := newRecorder(t)
	a, lib := libraryApp(cl, true)
	a.Update(tea.BackgroundColorMsg{Color: color.White})
	view := lib.view(120, 36)
	const darkText, darkSelect = "38;2;237;241;242", "48;2;36;32;55"
	if strings.Contains(view, darkText) || strings.Contains(view, darkSelect) {
		t.Errorf("white terminal: the view uses the dark set's text or select colour")
	}
	if luma(cFg) > 0.3 {
		t.Errorf("white terminal: text colour %v is not dark", cFg)
	}
	if l := luma(cSelect); l < 0.8 || l > 0.97 {
		t.Errorf("white terminal: select background %v, want a light tint", cSelect)
	}
}

// luma is a colour's brightness from 0 to 1.
func luma(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	return (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 0xffff
}
