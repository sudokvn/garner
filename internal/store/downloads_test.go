package store

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestOldDownloadLogs reads a failed download stored before garner
// cleaned SABnzbd's processing log: the log reads as plain lines, the
// credential in the link SABnzbd quotes is hidden, and the failure gets
// its concrete cause.
func TestOldDownloadLogs(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, t.TempDir(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now()
	if err := st.SaveSeries(ctx, SeriesData{
		Series:   Series{ID: 42, Name: "Harbor Precinct", DefaultOrder: "official", Type: TypeTV},
		Episodes: []Episode{{ID: 101, Numbers: map[string]Position{"official": {Season: 1, Number: 1}}}},
	}, true, now); err != nil {
		t.Fatal(err)
	}
	d, err := st.StartDownload(ctx, Download{EpisodeID: 101, Title: "Harbor.Precinct.S01E01.German.1080p.WEB-GRP", Link: "x"}, now)
	if err != nil {
		t.Fatal(err)
	}
	d.State, d.Failure = Failed, "Aborted, cannot be completed - https://sabnzbd.org/not-complete"
	d.Stages = []Stage{
		{Name: "Source", Actions: []string{"https://indexer.invalid/download?file=1&amp;apikey=SECRET"}},
		{Name: "Download", Actions: []string{"Downloaded in 12 seconds<br/>Age: 210d<br/>27 articles were missing"}},
	}
	if err := st.UpdateDownload(ctx, d, now); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetDownload(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Downloaded in 12 seconds", "Age: 210d", "27 articles were missing"}; len(got.Stages) != 2 || !slices.Equal(got.Stages[1].Actions, want) {
		t.Errorf("log = %q, want the download's lines %q", got.Stages, want)
	}
	if source := strings.Join(got.Stages[0].Actions, "\n"); strings.Contains(source, "SECRET") || strings.Contains(source, "&amp;") {
		t.Errorf("source = %q, want the link without HTML or the credential", source)
	}
	if got.FailureSummary != "27 articles missing" || got.Failure != d.Failure {
		t.Errorf("failure %q, summary %q; want SABnzbd's report and its concrete cause", got.Failure, got.FailureSummary)
	}
}
