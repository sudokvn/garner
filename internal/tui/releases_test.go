package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/sudokvn/garner/internal/api"
)

// TestReleasesTried shows which of two postings of the same release garner
// tried and what came of it, and tells them apart by their upload time and
// their pages (GRR-43). The Tried column appears only once garner tried one.
func TestReleasesTried(t *testing.T) {
	const title = "Harbor.Precinct.S01E01.German.DL.1080p.WEB-GRP"
	first := time.Date(2021, 9, 10, 4, 26, 0, 0, time.Local)
	second := time.Date(2021, 9, 8, 12, 28, 0, 0, time.Local)
	failed := &api.Download{State: api.Failed, Failure: "Aborted, cannot be completed", FailureSummary: "102 articles missing",
		Updated: time.Now()}
	s := &releasesScreen{sess: &session{}, back: &showScreen{name: "Harbor Precinct"}, search: &api.Search{Candidates: []api.Candidate{
		{ID: 1, Rank: 1, Indexer: "One", Title: title, Size: 725e6, Published: &first, Verdict: "accept", Tier: "German 1080p",
			Audio: []string{"de"}, Reasons: []string{"German audio"}, Attempt: failed, Page: "https://indexer.invalid/details/a1", Source: "WEB-DL"},
		{ID: 2, Rank: 2, Indexer: "One", Title: title, Size: 698e6, Published: &second, Verdict: "accept", Tier: "German 1080p",
			Audio: []string{"de"}, Reasons: []string{"German audio"}},
	}}, selected: 1}

	raw := s.view(150, 30)
	if !strings.Contains(raw, "\x1b]8;") || !strings.Contains(raw, "https://indexer.invalid/details/a1") {
		t.Error("the page is no link")
	}
	lines := strings.Split(ansi.Strip(raw), "\n")
	var head, tried, untried string
	for _, l := range lines {
		switch {
		case strings.Contains(l, "Release") && strings.Contains(l, "Preference"):
			head = l
		case strings.Contains(l, "725 MB"):
			tried = l
		case strings.Contains(l, "698 MB"):
			untried = l
		}
	}
	if !strings.Contains(head, "Tried") || !strings.Contains(tried, "failed") || strings.Contains(untried, "failed") {
		t.Errorf("rows do not tell the tried posting apart:\n%s\n%s\n%s", head, tried, untried)
	}
	// The source has a column; the indexer moved to WHY.
	if !strings.Contains(head, "Source") || strings.Contains(head, "Indexer") || !strings.Contains(tried, "WEB-DL") || strings.Contains(tried, " One") {
		t.Errorf("columns:\n%s\n%s", head, tried)
	}
	why := strings.Join(lines, "\n")
	for _, want := range []string{"Indexer  One      Uploaded  10 Sep 2021, 04:26", "Page  https://indexer.invalid/details/a1", "Failed", "102 articles missing",
		"does not pick this upload again"} {
		if !strings.Contains(why, want) {
			t.Errorf("the selected posting's details lack %q:\n%s", want, why)
		}
	}

	s.selected = 2
	if why := ansi.Strip(s.view(150, 30)); !strings.Contains(why, "Uploaded  8 Sep 2021, 12:28") || strings.Contains(why, "Failed") ||
		strings.Contains(why, "Page") {
		t.Errorf("the untried posting's details:\n%s", why)
	}
	s.search.Candidates[0].Attempt = nil
	if view := ansi.Strip(s.view(150, 30)); strings.Contains(view, "Tried") {
		t.Errorf("a Tried column without tried releases:\n%s", view)
	}
}

// TestReleasesPreferenceFits shows every release's preference in full,
// however long its language's name, and a rejected release's decisive
// reason in its place, cut at the end.
func TestReleasesPreferenceFits(t *testing.T) {
	s := &releasesScreen{sess: &session{}, back: &showScreen{name: "Lanterns and Tides"}, search: &api.Search{Candidates: []api.Candidate{
		{ID: 1, Rank: 1, Title: "Lanterns.and.Tides.S01E11.1080p.WEB-DL.H.264-TIDE", Size: 780e6, Verdict: "accept", Tier: "Japanese 1080p"},
		{ID: 2, Rank: 2, Title: "Lanterns.and.Tides.S01E11.720p.WEB-DL.H.264-TIDE", Size: 377e6, Verdict: "accept", Tier: "Japanese 720p"},
		{ID: 3, Rank: 3, Title: "Lanterns.and.Tides.S01E11.English.Dub.1080p.WEB-DL.H.264-FERRY", Size: 780e6, Verdict: "reject",
			Reasons: []string{"audio is English, not Japanese or German, as the indexer reports", "English audio"}, Decisive: 1},
	}}, selected: 1}
	view := ansi.Strip(s.view(150, 30))
	if !strings.Contains(view, "Japanese 1080p") {
		t.Errorf("the preference is cut:\n%s", view)
	}
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, "FERRY") && !strings.Contains(l, "audio is English, not Japan…") {
			t.Errorf("the rejected row lacks its reason:\n%s", view)
		}
	}
}

// TestRejectedReleaseForgotten keeps a rejected release from downloading
// on a single enter after its warning went out of sight: a detour through
// a screen or an overlay the app opens before Releases sees the key, or a
// reload of the list, drops the release waiting for its second enter.
func TestRejectedReleaseForgotten(t *testing.T) {
	sess := &session{}
	search := api.Search{Candidates: []api.Candidate{
		{ID: 1, Rank: 1, Title: "Harbor.Precinct.S01E01.English.1080p.WEB-GRP", Verdict: api.Reject,
			Reasons: []string{"audio is English, not German"}, Decisive: 1},
	}}
	show := &showScreen{name: "Harbor Precinct"}
	for _, detour := range [][]string{{"A", "esc"}, {"P", "esc"}, {"?", "esc"}, {":", "esc"}, {"n"}, {"reload"}} {
		s := &releasesScreen{sess: sess, back: show, search: &search, selected: 1}
		a := &app{sess: sess, screen: s, width: 120, height: 36}
		sendKeys(a, "enter")
		if s.confirm != 1 || !strings.Contains(ansi.Strip(s.flash.text), "again to download it anyway") {
			t.Fatalf("enter on a rejected release did not warn: %q", ansi.Strip(s.flash.text))
		}
		for _, k := range detour {
			if k == "reload" {
				a.update(releasesMsg{from: s, search: search})
				continue
			}
			cmd := sendKeys(a, k)
			// esc leaves Activity and Profiles; follow it back.
			if k == "esc" && cmd != nil {
				if m, ok := cmd().(switchMsg); ok {
					a.update(m)
				}
			}
		}
		if a.screen != screen(s) {
			t.Fatalf("after %v the screen is %T, not Releases", detour, a.screen)
		}
		sendKeys(a, "enter")
		if s.busy != "" || !strings.Contains(ansi.Strip(s.flash.text), "again to download it anyway") {
			t.Errorf("after %v, one enter started the download of the rejected release (busy %q, bottom %q)",
				detour, s.busy, ansi.Strip(s.flash.text))
		}
	}
}
