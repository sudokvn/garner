package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/sudokvn/garner/internal/api"
)

// lanterns is a show whose S01E11 garner downloaded: the first release
// held an English dub, which the file check rejected, then a download
// failed and the third was imported.
func lanterns(t *testing.T) (*showScreen, []api.Download) {
	t.Helper()
	now := time.Now()
	data := api.SeriesData{
		Series:   api.Series{ID: 42, Name: "Lanterns and Tides", DefaultOrder: "official", Type: api.TypeAnime},
		Episodes: []api.Episode{{ID: 111, Aired: "2026-11-21", Numbers: map[string]api.Position{"official": {Season: 1, Number: 11}}}},
	}
	dub := &api.FileCheck{Audio: []string{"English"}, Resolution: "1920x1080", Duration: "24m0s"}
	dub.AddProblem(api.ProblemAudioLanguage, "the file's audio is English, with no Japanese track")
	good := &api.FileCheck{Audio: []string{"Japanese"}, Resolution: "1920x1080", Duration: "24m0s", Notes: []string{"Japanese audio track found"}}
	downloads := []api.Download{
		{ID: 3, EpisodeID: 111, Title: "Lanterns.and.Tides.S01E11.1080p.WEBRip.x264-ORCHARD", Verdict: api.Accept, Tier: "Japanese 1080p",
			State: api.Completed, JobID: "c", Size: 780e6, Created: now, Updated: now,
			Import: api.Import{State: api.ImportDone, Files: []api.ImportFile{{Episodes: []int64{111}, Check: good}}}},
		{ID: 2, EpisodeID: 111, Title: "Lanterns.and.Tides.S01E11.720p.WEB-DL.H.264-TIDE", Verdict: api.Accept, Tier: "Japanese 720p",
			State: api.Failed, JobID: "b", Failure: "Aborted", FailureSummary: "SABnzbd could not repair it", Created: now, Updated: now},
		{ID: 1, EpisodeID: 111, Title: "Lanterns.and.Tides.S01E11.1080p.WEB-DL.H.264-TIDE", Verdict: api.Accept, Tier: "Japanese 1080p",
			State: api.Failed, JobID: "a", Failure: "file check: the file's audio is English, with no Japanese track", Created: now, Updated: now,
			Import: api.Import{State: api.ImportRejected, Files: []api.ImportFile{{Episodes: []int64{111}, Check: dub}}}},
	}
	show := newShow(&session{}, nil, 42, "Lanterns and Tides", "")
	show.setDetail(api.SeriesDetail{SeriesData: data, Best: "Japanese 1080p",
		States: []api.EpisodeStatus{{EpisodeID: 111, State: api.EpisodeImported, Size: 780e6, Tier: "Japanese 1080p", Check: good}}})
	return show, downloads
}

// TestEpisodeFileCheck lists every earlier attempt, newest first, with
// what a rejected release claimed and what the file check found, and
// shows a file kept without the language its release was for as kept
// with a caveat, with the check's notes, not as a plain pass.
func TestEpisodeFileCheck(t *testing.T) {
	show, downloads := lanterns(t)
	episode := newEpisode(&session{}, show, 111)
	episode.detail = &api.EpisodeDetail{Downloads: downloads}
	view := ansi.Strip(episode.view(120, 60))
	second, first := strings.Index(view, "Attempt 2"), strings.Index(view, "Attempt 1")
	if second < 0 || first < second || !strings.Contains(view, "Attempt  3") {
		t.Errorf("the attempts are not listed newest first:\n%s", view)
	}
	for _, want := range []string{"download failed · SABnzbd could not repair it",
		"rejected after download · claimed Japanese 1080p   found English audio", "Japanese audio track found"} {
		if !strings.Contains(view, want) {
			t.Errorf("the episode lacks %q:\n%s", want, view)
		}
	}

	// The user chose the dub by hand: the check kept it and noted why.
	kept := &api.FileCheck{Audio: []string{"English"}, Resolution: "1920x1080", Duration: "24m0s",
		Notes: []string{"no Japanese audio track; kept because the release was chosen by hand"}}
	dub := api.Download{ID: 4, EpisodeID: 111, Title: "Lanterns.and.Tides.S01E11.English.Dub.1080p.WEB-DL.H.264-FERRY", Verdict: api.Reject,
		Manual: true, State: api.Completed, JobID: "d", Created: time.Now(), Updated: time.Now(),
		Import: api.Import{State: api.ImportDone, Files: []api.ImportFile{{Episodes: []int64{111}, Check: kept}}}}
	episode.detail = &api.EpisodeDetail{Downloads: []api.Download{dub}}
	view = ansi.Strip(episode.view(120, 60))
	for _, want := range []string{"chosen by hand", "File check                English audio, no Japanese track · 1080p · 24 min",
		"no Japanese audio track; kept because the release was chosen by hand"} {
		if !strings.Contains(view, want) {
			t.Errorf("the kept dub lacks %q:\n%s", want, view)
		}
	}
	steps := pipeline(dub, time.Now())
	markCaveat(steps, dub, "Japanese 1080p")
	if steps[3].glyph == gDone {
		t.Error("the file check of the kept dub is a plain pass")
	}
}
