package store

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/sudokvn/garner/internal/api"
)

// TestActivity lists each episode's latest download: running ones and
// imports waiting for the user always, finished ones within the window.
func TestActivity(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, t.TempDir(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	pos := func(n int) map[string]Position { return map[string]Position{"official": {Season: 1, Number: n}} }
	if err := st.SaveSeries(ctx, SeriesData{
		Series: Series{ID: 42, Slug: "x", Name: "Harbor Precinct", OriginalLanguage: "eng", OriginalCountry: "usa",
			Status: "Ended", DefaultOrder: "official", TVDBUpdated: "2026-01-01", Type: "tv"},
		Episodes: []Episode{{ID: 101, Numbers: pos(1)}, {ID: 102, Numbers: pos(2)}, {ID: 103, Numbers: pos(3)}, {ID: 104, Numbers: pos(4)}},
	}, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	old, recent := now.Add(-72*time.Hour), now.Add(-time.Hour)
	download := func(episode int64, title string, at time.Time, state api.DownloadState, imported api.ImportState) {
		t.Helper()
		d, err := st.StartDownload(ctx, Download{EpisodeID: episode, Title: title, Link: "x", Verdict: "accept"}, at)
		if err != nil {
			t.Fatal(err)
		}
		d.State, d.JobID = state, title
		if err := st.UpdateDownload(ctx, d, at); err != nil {
			t.Fatal(err)
		}
		if imported != "" {
			if err := st.UpdateImport(ctx, d.ID, Import{State: imported}, at); err != nil {
				t.Fatal(err)
			}
		}
	}
	download(101, "replaced", old, Completed, ImportDone)
	download(101, "running", now, Downloading, "")
	download(102, "imported long ago", old, Completed, ImportDone)
	download(103, "import failed long ago", old, Completed, ImportFailed)
	download(104, "imported an hour ago", recent, Completed, ImportDone)

	items, err := st.Activity(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range items {
		got = append(got, it.Download.Title)
		if it.SeriesName != "Harbor Precinct" || it.Season != 1 || it.Number == 0 {
			t.Errorf("%s: %+v", it.Download.Title, it)
		}
	}
	slices.Sort(got)
	if want := []string{"import failed long ago", "imported an hour ago", "running"}; !slices.Equal(got, want) {
		t.Errorf("activity = %q, want %q", got, want)
	}
}

// TestRejections counts the downloads the file check rejected: by kind,
// with those stored before kinds existed as unclassified, and their
// episodes by whether a later download filled them.
func TestRejections(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, t.TempDir(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	pos := func(n int) map[string]Position { return map[string]Position{"official": {Season: 1, Number: n}} }
	if err := st.SaveSeries(ctx, SeriesData{
		Series: Series{ID: 42, Slug: "x", Name: "Harbor Precinct", OriginalLanguage: "eng", OriginalCountry: "usa",
			Status: "Ended", DefaultOrder: "official", TVDBUpdated: "2026-01-01", Type: "tv"},
		Episodes: []Episode{{ID: 101, Numbers: pos(1)}, {ID: 102, Numbers: pos(2)}, {ID: 103, Numbers: pos(3)}},
	}, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	days := func(n int) time.Time { return now.AddDate(0, 0, -n) }
	download := func(episode int64, at time.Time, im Import) int64 {
		t.Helper()
		d, err := st.StartDownload(ctx, Download{EpisodeID: episode, Title: "Harbor.Precinct.S01E01.German.1080p.WEB-GRP", Link: "x", Verdict: "accept"}, at)
		if err != nil {
			t.Fatal(err)
		}
		d.State = Completed
		if im.State == ImportRejected {
			d.State, d.Retryable = Failed, true
		}
		if err := st.UpdateDownload(ctx, d, at); err != nil {
			t.Fatal(err)
		}
		if err := st.UpdateImport(ctx, d.ID, im, at); err != nil {
			t.Fatal(err)
		}
		return d.ID
	}
	checked := func(episode int64, problems ...string) []ImportFile {
		c := &api.FileCheck{Audio: []string{}, Subtitles: []string{}}
		for i := 0; i < len(problems); i += 2 {
			c.AddProblem(api.ProblemKind(problems[i]), problems[i+1])
		}
		return []ImportFile{{Source: "/dl/a.mkv", Episodes: []int64{episode}, Check: c}}
	}

	// Episode 101: rejected for its audio, then imported from the next.
	download(101, days(1), Import{State: ImportRejected, Files: checked(101, "audio_language", "the file's audio is English, with no German track")})
	next := download(101, days(1), Import{State: ImportDone})
	if err := st.SetEpisodeFile(ctx, EpisodeFile{EpisodeID: 101, Path: "/lib/1.mkv", Size: 1}, next, days(1)); err != nil {
		t.Fatal(err)
	}
	// Episode 102: a problem stored before kinds, then no video at all;
	// still missing.
	old := []ImportFile{{Source: "/dl/b.mkv", Episodes: []int64{102}, Check: &api.FileCheck{Problems: []string{"the video is 720x576, below 720p"}}}}
	download(102, days(30), Import{State: ImportRejected, Files: old})
	download(102, days(2), Import{State: ImportRejected})
	// Episode 103: keeps the file garner took over; the upgrade was
	// rejected for two problems.
	if _, err := st.TakeOverFile(ctx, EpisodeFile{EpisodeID: 103, Path: "/lib/3.mkv", Size: 1}, days(40)); err != nil {
		t.Fatal(err)
	}
	download(103, days(3), Import{State: ImportRejected, Files: checked(103,
		"resolution", "the video is 1280x720, but the release claimed 1080p", "length", "the file runs 1m0s, but the episodes run about 22m; it is incomplete or a sample")})

	got, err := st.Rejections(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 4 || got.LastWeek != 3 || !got.Oldest.Equal(days(30)) {
		t.Errorf("total %d, last week %d, oldest %s; want 4, 3, %s", got.Total, got.LastWeek, got.Oldest, days(30))
	}
	want := map[api.ProblemKind]int{api.ProblemAudioLanguage: 1, api.ProblemUnclassified: 1, api.ProblemNoVideoFile: 1, api.ProblemResolution: 1, api.ProblemLength: 1}
	if !maps.Equal(got.Kinds, want) {
		t.Errorf("kinds = %v, want %v", got.Kinds, want)
	}
	if got.Episodes != 3 || got.Imported != 1 || got.Missing != 1 {
		t.Errorf("episodes %d, imported %d, missing %d; want 3, 1, 1", got.Episodes, got.Imported, got.Missing)
	}
}

// TestActivityRejectedAttempts keeps the attempts the file check rejected
// under their episode's latest download while garner replaces them one
// after another, but not those of an earlier run: one garner stopped
// after, or one it did not replace before the user started another.
func TestActivityRejectedAttempts(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, t.TempDir(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	pos := func(n int) map[string]Position { return map[string]Position{"official": {Season: 1, Number: n}} }
	if err := st.SaveSeries(ctx, SeriesData{
		Series: Series{ID: 42, Slug: "x", Name: "Harbor Precinct", OriginalLanguage: "eng", OriginalCountry: "usa",
			Status: "Ended", DefaultOrder: "official", TVDBUpdated: "2026-01-01", Type: "tv"},
		Episodes: []Episode{{ID: 101, Numbers: pos(1)}, {ID: 102, Numbers: pos(2)}},
	}, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	// attempt starts a download at start that ends at end: rejected by the
	// file check, failed in SABnzbd, or still running when end is zero.
	attempt := func(episode int64, title string, start, end time.Time, rejected bool, stopped string) {
		t.Helper()
		d, err := st.StartDownload(ctx, Download{EpisodeID: episode, Title: title, Link: "x", Verdict: "accept"}, start)
		if err != nil {
			t.Fatal(err)
		}
		d.State, d.JobID = Downloading, title
		if !end.IsZero() {
			d.State, d.Retryable, d.Failure = Failed, true, "failed"
		}
		if err := st.UpdateDownload(ctx, d, end); err != nil {
			t.Fatal(err)
		}
		if rejected {
			if err := st.UpdateImport(ctx, d.ID, Import{State: ImportRejected, Detail: "file check: the video is 1280x720"}, end); err != nil {
				t.Fatal(err)
			}
		}
		if stopped != "" {
			if err := st.StopReplacing(ctx, d.ID, stopped); err != nil {
				t.Fatal(err)
			}
		}
	}
	at := func(minutes int) time.Time { return now.Add(time.Duration(minutes-60) * time.Minute) }

	attempt(101, "earlier run", at(-3000), at(-2990), true, "No other release like it is left to try.")
	attempt(101, "rejected first", at(0), at(10), true, "")
	attempt(101, "failed in SABnzbd", at(10), at(20), false, "")
	attempt(101, "rejected second", at(20), at(30), true, "")
	attempt(101, "running", at(30), time.Time{}, false, "")
	// garner could not replace this one; the user started the next.
	attempt(102, "not replaced", at(0), at(10), true, "")
	attempt(102, "started later", at(50), time.Time{}, false, "")

	items, err := st.Activity(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	for _, it := range items {
		var titles []string
		for _, d := range it.Rejected {
			titles = append(titles, d.Title)
		}
		got[it.Download.Title] = titles
	}
	if want := []string{"rejected second", "rejected first"}; !slices.Equal(got["running"], want) {
		t.Errorf("rejected attempts of the run = %q, want %q", got["running"], want)
	}
	if r, ok := got["started later"]; !ok || len(r) != 0 {
		t.Errorf("rejected attempts before a download the user started = %q, want none", r)
	}
}
