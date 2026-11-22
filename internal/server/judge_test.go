package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sudokvn/garner/internal/api"
	"github.com/sudokvn/garner/internal/client"
	"github.com/sudokvn/garner/internal/config"
	"github.com/sudokvn/garner/internal/connect"
	"github.com/sudokvn/garner/internal/profile"
	"github.com/sudokvn/garner/internal/prowlarr"
	"github.com/sudokvn/garner/internal/search"
	"github.com/sudokvn/garner/internal/store"
	"github.com/sudokvn/garner/internal/testprofiles"
)

// TestJudgeSearch judges a stored search again through the client. Under
// the show's own profile the verdicts, tiers and ranks are the stored
// ones, which needs the languages and the show the indexer reported; under
// another profile they change. Both summaries keep the note on an indexer
// that answered only some searches. Judging stores nothing, refuses a
// profile with problems, and refuses a search stored before garner kept
// what the indexers reported instead of guessing.
func TestJudgeSearch(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)
	st, err := store.Open(ctx, t.TempDir(), log)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	testprofiles.Install(t, st)
	if err := st.SaveSeries(ctx, store.SeriesData{
		Series: store.Series{ID: 42, Slug: "harbor-precinct", Name: "Harbor Precinct", OriginalLanguage: "eng",
			OriginalCountry: "usa", Status: "Continuing", DefaultOrder: "official", TVDBUpdated: "2026-01-01", Type: "tv"},
		Episodes: []store.Episode{
			{ID: 101, Aired: "2026-01-01", Numbers: map[string]store.Position{"official": {Season: 1, Number: 1}}},
			{ID: 102, Aired: "2026-01-08", Numbers: map[string]store.Position{"official": {Season: 1, Number: 2}}},
			{ID: 103, Aired: "2026-01-15", Numbers: map[string]store.Position{"official": {Season: 1, Number: 3}}},
		},
	}, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	// An anime episode outside season 1 is also searched by its absolute
	// number, a query the indexer refuses.
	if err := st.SaveSeries(ctx, store.SeriesData{
		Series: store.Series{ID: 43, Slug: "night-ferry", Name: "Night Ferry", OriginalLanguage: "jpn",
			OriginalCountry: "jpn", Status: "Continuing", DefaultOrder: "official", TVDBUpdated: "2026-01-01", Type: "anime"},
		Episodes: []store.Episode{
			{ID: 301, Aired: "2026-01-01", Numbers: map[string]store.Position{"official": {Season: 1, Number: 1}}},
			{ID: 302, Aired: "2026-01-08", Numbers: map[string]store.Position{"official": {Season: 2, Number: 1}}},
		},
	}, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/indexer" {
			fmt.Fprint(w, `[{"id":1,"name":"One","enable":true,"protocol":"usenet"}]`)
			return
		}
		if r.URL.Query().Get("t") == "search" {
			http.Error(w, "no text searches", http.StatusBadRequest)
			return
		}
		items := []struct{ title, attrs string }{
			{"Harbor.Precinct.S01E01.1080p.WEB-DL-GRP", `<newznab:attr name="language" value="German"/>`},
			{"Harbor.Precinct.S01E01.German.DL.720p.WEB-GRP", ""},
			{"Harbor.Precinct.S01E01.German.1080p.WEB-ONE", `<newznab:attr name="tvdbid" value="99"/>`},
			{"Harbor.Precinct.S01E01.1080p.WEB-ENG", ""},
		}
		if r.URL.Query().Get("tvdbid") == "43" {
			items = items[:0]
			items = append(items, struct{ title, attrs string }{"Night.Ferry.S02E01.German.DL.1080p.WEB-GRP", ""})
		}
		var b strings.Builder
		for i, it := range items {
			fmt.Fprintf(&b, `<item><title>%s</title><guid>id-%d</guid><link>http://prowlarr/1/download?apikey=SECRET&amp;id=%[2]d</link><size>1</size>%s</item>`, it.title, i, it.attrs)
		}
		fmt.Fprintf(w, `<rss xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel>%s</channel></rss>`, b.String())
	}))
	defer indexer.Close()

	s := &Server{cfg: &config.Config{}, store: st, log: log, events: newBroker(), tlsMode: TLSOff}
	if err := s.loadProfiles(ctx); err != nil {
		t.Fatal(err)
	}
	s.search = search.New(st, prowlarr.New(indexer.URL, "key", indexer.Client()), nil, s.profileFor, log)
	srv := httptest.NewServer(s.Routes())
	defer srv.Close()
	token, err := st.CreateToken(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	c, err := client.New(connect.Code{URL: srv.URL, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := c.SearchEpisode(ctx, 101)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Candidates) != 4 {
		t.Fatalf("search found %d releases, want 4", len(stored.Candidates))
	}
	storedJSON, _ := json.Marshal(stored)

	// The show's own profile gives the stored judgement.
	same, err := c.JudgeSearch(ctx, 101, profile.Encode(testprofiles.German))
	if err != nil {
		t.Fatal(err)
	}
	if len(same.Candidates) != len(stored.Candidates) || same.After.Summary != stored.Summary || same.Before.Summary != stored.Summary {
		t.Fatalf("judged again under the same profile: %+v, stored summary %q", same, stored.Summary)
	}
	for i, sc := range stored.Candidates {
		jc := same.Candidates[i]
		for _, j := range []api.Judged{jc.Before, jc.After} {
			if jc.ID != sc.ID || j.Verdict != sc.Verdict || j.Tier != sc.Tier || j.Rank != sc.Rank {
				t.Errorf("%s: judged again %s %q rank %d, stored %s %q rank %d", sc.Title, j.Verdict, j.Tier, j.Rank, sc.Verdict, sc.Tier, sc.Rank)
			}
		}
	}

	// Under English, the English release comes first and the German ones
	// drop out; the release filed under another show stays rejected.
	var en profile.Profile
	for _, p := range profile.Shipped() {
		if p.Name == "English" {
			en = p
		}
	}
	english, err := c.JudgeSearch(ctx, 101, profile.Encode(en))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, jc := range english.Candidates {
		got = append(got, fmt.Sprintf("%d %s %s->%s %d->%d", jc.After.Rank, jc.Title, jc.Before.Verdict, jc.After.Verdict, jc.Before.Rank, jc.After.Rank))
	}
	want := []string{
		"1 Harbor.Precinct.S01E01.1080p.WEB-ENG reject->accept 4->1",
	}
	if english.After.Profile != "English" || len(got) != 4 || got[0] != want[0] || english.After.Outcome != api.SearchFound ||
		english.Before.Profile != "German" {
		t.Errorf("judged under English:\n%s\nafter %+v", strings.Join(got, "\n"), english.After)
	}
	for _, jc := range english.Candidates {
		if strings.HasSuffix(jc.Title, "-ONE") && (jc.After.Verdict != api.Reject || jc.After.Reasons[0] != "the indexer files it under another show") {
			t.Errorf("the release filed under another show: %+v", jc.After)
		}
	}

	// An indexer that answered only some searches keeps its note in both
	// summaries.
	partial, err := c.SearchEpisode(ctx, 302)
	if err != nil {
		t.Fatal(err)
	}
	const note = " (One answered only some searches)"
	if !strings.HasSuffix(partial.Summary, note) {
		t.Fatalf("the search's summary %q has no note on One", partial.Summary)
	}
	judged, err := c.JudgeSearch(ctx, 302, profile.Encode(en))
	if err != nil {
		t.Fatal(err)
	}
	if judged.Before.Summary != partial.Summary || !strings.HasSuffix(judged.After.Summary, note) {
		t.Errorf("judged again: before %q, after %q; stored %q", judged.Before.Summary, judged.After.Summary, partial.Summary)
	}

	// Nothing changed.
	again, err := c.EpisodeSearch(ctx, 101)
	if err != nil {
		t.Fatal(err)
	}
	if againJSON, _ := json.Marshal(again); string(againJSON) != string(storedJSON) {
		t.Errorf("judging changed the stored search:\n%s\nwas\n%s", againJSON, storedJSON)
	}

	var se *client.ServerError
	_, err = c.JudgeSearch(ctx, 101, "garner_profile = 1\nname = \"Broken\"\naudio = []\nresolution = [\"1080p\"]\n")
	if !errors.As(err, &se) || se.Status != http.StatusBadRequest || len(se.Problems) == 0 {
		t.Errorf("a profile with problems: %v", err)
	}
	_, err = c.JudgeSearch(ctx, 102, profile.Encode(testprofiles.German))
	if !errors.As(err, &se) || se.Status != http.StatusNotFound {
		t.Errorf("an episode never searched: %v", err)
	}
	// A search stored before garner kept what the indexers reported has
	// no report on its candidates.
	if err := st.SaveSearch(ctx, store.Search{EpisodeID: 103, Searched: time.Now(), Outcome: api.SearchFound, Summary: "Found 1 matching release",
		Candidates: []store.Candidate{{Indexer: "One", Title: "Harbor.Precinct.S01E03.1080p.WEB-DL-GRP", Link: "x", Verdict: api.Accept,
			Tier: "German 1080p", Reasons: []string{"German audio, as the indexer reports"}, Decisive: 1, Episodes: []int64{103}}},
	}); err != nil {
		t.Fatal(err)
	}
	_, err = c.JudgeSearch(ctx, 103, profile.Encode(testprofiles.German))
	if !errors.As(err, &se) || se.Status != http.StatusConflict || !strings.Contains(se.Message, "search again") {
		t.Errorf("a search from before: %v", err)
	}
}
