package tui

import (
	"regexp"
	"strings"
	"testing"

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
