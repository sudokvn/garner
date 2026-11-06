package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/sudokvn/garner/internal/api"
)

// TestFailedDownloadDetails shows a failed download's concrete cause in
// Activity and its full report on the episode screen, without inventing
// completion.
func TestFailedDownloadDetails(t *testing.T) {
	now := time.Now()
	data := api.SeriesData{
		Series:   api.Series{ID: 42, Name: "Harbor Precinct", DefaultOrder: "official", Type: "tv"},
		Episodes: []api.Episode{{ID: 101, Numbers: map[string]api.Position{"official": {Season: 1, Number: 1}}}},
	}
	d := api.Download{
		ID: 1, EpisodeID: 101, Title: "Harbor.Precinct.S01.German.1080p.WEB-GRP", State: api.Failed, JobID: "job-1", Retryable: true,
		Failure:        "Aborted, cannot be completed - https://sabnzbd.org/not-complete",
		FailureSummary: "27 articles missing",
		Stopped:        "No other release meets German 1080p, and garner does not switch to another preference on its own.",
		Stages: []api.Stage{
			{Name: "Source", Actions: []string{"https://indexer.invalid/download?file=1"}},
			{Name: "Download", Actions: []string{"Downloaded in 12 seconds at an average of 8 MB/s", "Age: 210d", "27 articles were missing"}},
			{Name: "Servers", Actions: []string{"news.example.invalid=96 MB"}},
		},
		Reasons: []string{}, Created: now, Updated: now,
	}
	sess := &session{}
	activity := newActivity(sess, nil)
	activity.update(activityMsg{from: activity, items: []api.ActivityItem{{Download: d, SeriesID: 42, SeriesName: "Harbor Precinct", Season: 1, Number: 1}}})
	show := newShow(sess, nil, 42, "Harbor Precinct", "")
	show.setDetail(api.SeriesDetail{SeriesData: data})
	episode := newEpisode(sess, show, 101)
	episode.detail = &api.EpisodeDetail{Downloads: []api.Download{d}}
	for name, view := range map[string]string{
		"activity": activity.view(120, 36),
		"episode":  episode.view(120, 36),
	} {
		plain := ansi.Strip(view)
		evidence := []string{"27 articles missing"}
		if name == "episode" {
			evidence = append(evidence, "Aborted, cannot be completed", "Downloaded in 12 seconds", "Age: 210d", "news.example.invalid=96 MB",
				"No other release meets German 1080p") // why nothing else started
		}
		for _, want := range evidence {
			if !strings.Contains(plain, want) {
				t.Errorf("%s hides %q", name, want)
			}
		}
		if strings.Contains(plain, "100%") {
			t.Errorf("%s invents completion", name)
		}
	}
}
