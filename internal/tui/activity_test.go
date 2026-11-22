package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/sudokvn/garner/internal/api"
)

// TestActivityRejected hangs an attempt the file check rejected under its
// episode's row with the server's reason, so it does not vanish once its
// replacement is imported, and labels the imported file by what the check
// found.
func TestActivityRejected(t *testing.T) {
	_, downloads := lanterns(t)
	imported, rejected := downloads[0], downloads[2]
	imported.Tier = "German 1080p" // what the release claimed, not what was found
	s := newActivity(&session{}, nil)
	s.update(activityMsg{from: s, items: []api.ActivityItem{{Download: imported, Rejected: []api.Download{rejected},
		SeriesID: 42, SeriesName: "Lanterns and Tides", Season: 1, Number: 11}}})
	lines := strings.Split(ansi.Strip(s.view(120, 36)), "\n")
	row := -1
	for i, l := range lines {
		if strings.Contains(l, "Lanterns and Tides") {
			row = i
		}
	}
	if row < 0 || row+1 >= len(lines) || !strings.Contains(lines[row], "Japanese 1080p · 780 MB") ||
		!strings.Contains(lines[row+1], "└ rejected after download · the file's audio is English, with no Japanese track") {
		t.Errorf("the rejected attempt does not hang under its episode:\n%s", strings.Join(lines, "\n"))
	}
}
