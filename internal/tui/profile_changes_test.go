package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestEditorChanges checks the changes pane: it appears only while the
// profile differs from the one opened, says how many shows use it, lists
// each changed setting as was and now, including the preferences, and
// says what saving does. Without colour, the words carry the difference.
func TestEditorChanges(t *testing.T) {
	colourless = true
	t.Cleanup(func() { colourless = false })
	_, cl := newRecorder(t)
	a, ed := editorApp(cl, 1)
	if view := ansi.Strip(ed.view(120, 36)); !strings.Contains(view, "PREFERENCES") || strings.Contains(view, "NOT SAVED YET") {
		t.Fatalf("an unchanged profile shows changes:\n%s", view)
	}
	// German, English fallback: remove English, prefer dual audio.
	sendKeys(a, "j", "d", "j", "j", "j", "j", "h")
	view := ansi.Strip(ed.view(120, 36))
	for _, want := range []string{
		"NOT SAVED YET · 2 SHOWS USE THIS PROFILE",
		"Audio          was  German, then English",
		"now  German  ",
		"Dual audio     was  ignore",
		"now  prefer",
		"Preferences    was  German 1080p → German 720p → English 1080p → English 720p",
		"now  German 1080p → German 720p  ",
		"If you save    all 2 shows follow the new rules from their next search. Nothing is searched now.",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the changes pane lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "PREFERENCES") || strings.Contains(view, "Priority       was") {
		t.Errorf("the pane shows the preferences pane or an unchanged setting:\n%s", view)
	}
	// Back as opened, the preferences return.
	sendKeys(a, "l")
	ed.p.Audio = append(ed.p.Audio, "en")
	if view := ansi.Strip(ed.view(120, 36)); ed.changed() || !strings.Contains(view, "PREFERENCES") {
		t.Errorf("back as opened: changed %v\n%s", ed.changed(), view)
	}
}
