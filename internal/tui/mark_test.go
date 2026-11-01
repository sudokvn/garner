package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// TestWelcomeMark checks where the empty library shows garner's mark:
// centred over the message with two blank rows around it, only in
// terminals with 256 colours or more and only when the table has room.
// Otherwise the message stands alone at the table's left edge.
func TestWelcomeMark(t *testing.T) {
	sess := &session{}
	a := &app{sess: sess, library: newLibrary(sess)}
	a.screen = a.library
	a.library.loaded = true
	rows := func(height int) []string { return strings.Split(ansi.Strip(a.library.view(120, height)), "\n") }
	const message = "No shows yet. Press a to add one."

	a.Update(tea.ColorProfileMsg{Profile: colorprofile.TrueColor})
	got := rows(36)
	if top := libraryTop + 2; strings.TrimRight(got[top], " ") != strings.Repeat(" ", 51)+markLines[0] {
		t.Errorf("row %d = %q, want the mark's first row centred", top, got[top])
	}
	if at := libraryTop + 2 + len(markLines) + 2; got[at] != strings.Repeat(" ", 43)+message {
		t.Errorf("row %d = %q, want the message centred under the mark", at, got[at])
	}

	for _, c := range []struct {
		name    string
		profile colorprofile.Profile
		height  int
	}{
		{"16 colours", colorprofile.ANSI, 36},
		{"NO_COLOR", colorprofile.ASCII, 36},
		{"too short for the mark", colorprofile.TrueColor, 20},
	} {
		a.Update(tea.ColorProfileMsg{Profile: c.profile})
		got := rows(c.height)
		if view := strings.Join(got, "\n"); strings.Contains(view, "█") || got[libraryTop] != "   "+message {
			t.Errorf("%s: want the message alone at the left, got\n%s", c.name, view)
		}
	}
}

// TestAboutMark checks that About sets the name and versions beside the
// mark where the terminal shows it and is 80 columns wide, and as without
// the mark otherwise.
func TestAboutMark(t *testing.T) {
	s := newAbout(&session{mark: true}, nil, "/home/user/.config/garner/connection", "0.4.0")
	got := strings.Split(ansi.Strip(s.view(120, 36)), "\n")
	column := -1
	if i := strings.Index(got[3], "garner"); i >= 0 {
		column = ansi.StringWidth(got[3][:i])
	}
	if strings.TrimRight(got[2], " ") != "   "+markLines[0] || column != 3+markWidth+4 {
		t.Errorf("want the mark from row 2 and the name beside it in column %d, got column %d in\n%s", 3+markWidth+4, column, strings.Join(got[:12], "\n"))
	}

	got = strings.Split(ansi.Strip(s.view(79, 36)), "\n")
	if strings.Contains(strings.Join(got, "\n"), "█") || got[2] != "   garner" {
		t.Errorf("at 79 columns, want no mark and the name at the left, got\n%s", strings.Join(got[:12], "\n"))
	}
}
