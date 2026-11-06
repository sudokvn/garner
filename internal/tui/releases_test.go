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
// however long its language's name.
func TestReleasesPreferenceFits(t *testing.T) {
	s := &releasesScreen{sess: &session{}, back: &showScreen{name: "Lanterns and Tides"}, search: &api.Search{Candidates: []api.Candidate{
		{ID: 1, Rank: 1, Title: "Lanterns.and.Tides.S01E11.1080p.WEB-DL.H.264-TIDE", Size: 780e6, Verdict: "accept", Tier: "Japanese 1080p"},
		{ID: 2, Rank: 2, Title: "Lanterns.and.Tides.S01E11.720p.WEB-DL.H.264-TIDE", Size: 377e6, Verdict: "accept", Tier: "Japanese 720p"},
	}}, selected: 1}
	if view := ansi.Strip(s.view(150, 30)); !strings.Contains(view, "Japanese 1080p") {
		t.Errorf("the preference is cut:\n%s", view)
	}
}
