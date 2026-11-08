package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/sudokvn/garner/internal/api"
)

// Source offers must describe the actual builds, including forks, rather
// than silently substituting upstream source when versions differ.
func TestSourceChoices(t *testing.T) {
	const serverSource = "https://fork.example/v0.4.1/source.tar.gz"
	const tuiSource = "https://garner.example/v0.4.0/source.tar.gz"
	p := sourceChoices("v0.4.0", tuiSource, &api.Status{Version: "v0.4.1", SourceURL: serverSource})
	p.width = 100
	view := ansi.Strip(strings.Join(p.lines("Source and licence", 36), "\n"))
	for _, want := range []string{"v0.4.0", "v0.4.1", serverSource, tuiSource, "No warranty"} {
		if !strings.Contains(view, want) {
			t.Errorf("source lookup is missing %q: %s", want, view)
		}
	}
	for _, want := range []string{licenceURL, serverSource, tuiSource} {
		_, chosen := p.key("enter")
		if chosen < 0 || p.items[chosen].value != want {
			t.Fatalf("selected source = %v, want %s", chosen, want)
		}
		p.key("j")
	}
}

// An older server or a custom build without a source offer must remain
// honest. The lookup captures global keys and Escape returns to About.
func TestAboutSourceLookup(t *testing.T) {
	_, cl := newRecorder(t)
	sess := &session{client: cl}
	s := newAbout(sess, nil, "/tmp/connection", "dev")
	s.update(aboutStatusMsg{from: s, status: api.Status{Version: "v0.4.0"}})
	a := &app{sess: sess, screen: s}
	sendKeys(a, "o", "?", "A")
	if !s.captures() || a.help || a.screen != s {
		t.Fatal("source lookup did not capture the global keys")
	}
	for _, width := range []int{80, 120} {
		view := ansi.Strip(s.view(width, 36))
		if !strings.Contains(view, "Server v0.4.0: no source archive advertised") {
			t.Fatalf("missing source is not explained at %d columns: %s", width, view)
		}
	}
	sendKeys(a, "esc")
	if s.captures() || a.screen != s {
		t.Fatal("Escape did not return to About")
	}
	view := ansi.Strip(s.view(120, 36))
	if !strings.Contains(view, "AGPLv3 · source and licence") || !strings.Contains(view, "Metadata: TheTVDB") {
		t.Fatalf("About does not show the licence entry and credit: %s", view)
	}
}

func TestSourceLinkRejectsNonWebTargets(t *testing.T) {
	for _, raw := range []string{
		"file:///etc/passwd", "javascript:alert(1)", "garner://connect",
		"https://user:password@example.invalid/source", "https:///source",
		"https://example.invalid/\x1b]8;;file:///etc/passwd", "https://example.invalid/\u202esource",
		"--help", "https://example.invalid\\evil/source", "https://example.invalid/\nsource",
	} {
		if got := sourceLink(raw); got != "" {
			t.Error("unsafe source location was accepted")
		}
	}
	for _, raw := range []string{"https://fork.example/source.tar.gz", "http://127.0.0.1:8080/source.tar.gz"} {
		if sourceLink(raw) != raw {
			t.Errorf("valid source location %q was rejected", raw)
		}
	}
}
