package tui

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/sudokvn/garner/internal/api"
	"github.com/sudokvn/garner/internal/client"
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

// TestEpisodeSearchKeys splits what r once did by state: enter shows the
// last search's releases without searching, s searches anew wherever the
// Show screen's s would, and r only retries an import. esc on the
// releases comes back to the episode. An episode never searched has no
// releases to show.
func TestEpisodeSearchKeys(t *testing.T) {
	show, downloads := lanterns(t)
	open := func(e *episodeScreen, key string) *releasesScreen {
		t.Helper()
		cmd := e.key(key)
		if cmd == nil {
			return nil
		}
		sw, _ := cmd().(switchMsg)
		r, ok := sw.screen.(*releasesScreen)
		if !ok {
			t.Fatalf("%s opened %T, want Releases", key, sw.screen)
		}
		if r.leave != screen(e) || r.episode.episode.ID != 111 {
			t.Errorf("%s opened the releases of %d, leading back to %T", key, r.episode.episode.ID, r.leave)
		}
		return r
	}
	for _, c := range []struct {
		name      string
		downloads []api.Download
		summary   string
		stored    bool // enter shows the last search
		retry     bool // r retries the import
	}{
		{name: "never searched"},
		{name: "searched, nothing found", summary: "No matching release found", stored: true},
		{name: "downloading", downloads: []api.Download{{ID: 9, EpisodeID: 111, State: api.Downloading}}, stored: true},
		{name: "import failed", downloads: []api.Download{{ID: 9, EpisodeID: 111, State: api.Completed,
			Import: api.Import{State: api.ImportFailed, Detail: "permission denied"}}}, stored: true, retry: true},
		{name: "needs you", downloads: []api.Download{{ID: 9, EpisodeID: 111, State: api.Completed,
			Import: api.Import{State: api.ImportUnverified}}}, stored: true, retry: true},
		{name: "imported", downloads: downloads, stored: true},
	} {
		show.detail.States[0].Summary = c.summary
		e := newEpisode(&session{}, show, 111)
		e.detail = &api.EpisodeDetail{Downloads: c.downloads}
		if r := open(e, "s"); r == nil || !r.searching {
			t.Errorf("%s: s did not start a new search", c.name)
		}
		switch r := open(e, "enter"); {
		case c.stored && (r == nil || r.searching):
			t.Errorf("%s: enter did not show the last search as it stands", c.name)
		case !c.stored && r != nil:
			t.Errorf("%s: enter opened releases of an episode never searched", c.name)
		case c.stored:
			// A download from a season pack leaves the episode without a
			// search of its own: enter must say so, not search.
			_, cmd := r.update(releasesMsg{from: r, err: &client.ServerError{Status: http.StatusNotFound}})
			if cmd != nil || r.searching || !strings.Contains(ansi.Strip(r.view(120, 36)), "no search of its own") {
				t.Errorf("%s: enter searched, or did not say that the episode has no search of its own", c.name)
			}
		}
		if cmd := e.key("r"); (cmd != nil) != c.retry || c.retry && e.busy != "Retrying the import…" {
			t.Errorf("%s: r retried the import %v (busy %q), want %v", c.name, cmd != nil, e.busy, c.retry)
		}
	}
}
