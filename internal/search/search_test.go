package search

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sudokvn/garner/internal/api"
	"github.com/sudokvn/garner/internal/match"
	"github.com/sudokvn/garner/internal/prowlarr"
	"github.com/sudokvn/garner/internal/store"
	"github.com/sudokvn/garner/internal/testprofiles"
)

// series is a made-up show with the given number of episodes per season.
func series(typ string, seasons ...int) store.SeriesData {
	d := store.SeriesData{Series: store.Series{ID: 42, Slug: "lantern-harbor", Name: "灯籠の港", NameEng: "Lantern Harbor (2011)",
		NameDeu: "Laternenhäfen", OriginalLanguage: "jpn", Status: "Ended", DefaultOrder: "official", TVDBUpdated: "2026-01-01", Type: typ}}
	for s, count := range seasons {
		for n := 1; n <= count; n++ {
			d.Episodes = append(d.Episodes, store.Episode{ID: int64((s+1)*10000 + n), Aired: "2020-01-01",
				Numbers: map[string]store.Position{"official": {Season: s + 1, Number: n}}})
		}
	}
	return d
}

// TestAbsoluteQueries searches an anime episode also by its absolute
// number where its TVDB position differs from it or the show runs long,
// padded as groups pad it, under the English and the German title.
func TestAbsoluteQueries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		d       store.SeriesData
		episode int64
		want    []string
	}{
		{"regular TV", series(store.TypeTV, 3, 3), 20001, nil},
		{"first season of a short anime", series(store.TypeAnime, 3, 3), 10002, nil},
		{"second season", series(store.TypeAnime, 3, 3), 20001, []string{"Lantern Harbor 04", "Laternenhaefen 04"}},
		{"first season of a long anime", series(store.TypeAnime, 58, 78, 12), 10030,
			[]string{"Lantern Harbor 30", "Lantern Harbor 030", "Laternenhaefen 30", "Laternenhaefen 030"}},
		{"past 999 episodes", series(store.TypeAnime, 58, 1000), 20001, []string{"Lantern Harbor 059", "Lantern Harbor 0059", "Laternenhaefen 059", "Laternenhaefen 0059"}},
		{"past 999 episodes, four digits", series(store.TypeAnime, 58, 1000), 21000, []string{"Lantern Harbor 1058", "Laternenhaefen 1058"}},
	} {
		show := match.NewShow(tc.d, match.Options{Anime: tc.d.Series.Type == store.TypeAnime})
		i := slices.IndexFunc(tc.d.Episodes, func(e store.Episode) bool { return e.ID == tc.episode })
		if got := absoluteQueries(tc.d.Series, show, tc.episode, tc.d.Episodes[i].Numbers["official"]); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
	// Where TVDB skips a number in season 1, the numberings differ there.
	gap := series(store.TypeAnime, 3)
	gap.Episodes = slices.Delete(gap.Episodes, 1, 2)
	if got := absoluteQueries(gap.Series, match.NewShow(gap, match.Options{Anime: true}), 10003, store.Position{Season: 1, Number: 3}); !slices.Contains(got, "Lantern Harbor 02") {
		t.Errorf("gap in season 1: %q", got)
	}
	// Letters that are more than an accented ASCII letter are written out.
	if got := searchTitle("Łódź Harbor"); got != "Lodz Harbor" {
		t.Errorf("Polish title: %q", got)
	}
	// An umlaut written as a letter and a combining mark reads the same.
	if got := searchTitle("Laternenha\u0308fen (2011)"); got != "Laternenhaefen" {
		t.Errorf("combining umlaut: %q", got)
	}
	// A title only in Japanese gives no query.
	d := series(store.TypeAnime, 3, 3)
	d.Series.NameEng, d.Series.NameDeu = "", ""
	if got := absoluteQueries(d.Series, match.NewShow(d, match.Options{Anime: true}), 20001, store.Position{Season: 2, Number: 1}); got != nil {
		t.Errorf("Japanese title: %q", got)
	}
}

// TestAbsoluteSearch finds an anime episode that groups number absolutely:
// the aired-order search misses it, the text searches find it under both
// numberings, a release found twice counts once, and what a text search
// finds for other shows or episodes stays out.
func TestAbsoluteSearch(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)
	st, err := store.Open(ctx, t.TempDir(), log)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.SaveSeries(ctx, series(store.TypeAnime, 3, 3), true, time.Now()); err != nil {
		t.Fatal(err)
	}

	var (
		mu        sync.Mutex
		texts     []string
		failAired bool // the aired-order search answers with an error
		failText  bool // the text searches do
	)
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/indexer" {
			fmt.Fprint(w, `[{"id":1,"name":"One","enable":true,"protocol":"usenet"}]`)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		q := r.URL.Query().Get("q")
		if q != "" {
			texts = append(texts, q)
		}
		if q == "" && failAired || q != "" && failText {
			fmt.Fprint(w, `<?xml version="1.0"?><error code="203" description="Function not available"/>`)
			return
		}
		// The aired-order search finds only the first episode, as indexers
		// do that ignore the season.
		titles := []string{"Lantern.Harbor.S01E01.German.DL.1080p.BluRay-ONE"}
		if q != "" {
			titles = []string{
				"Lantern.Harbor.E04.German.DL.1080p.BluRay-ABS",
				"Lantern.Harbor.S01E04.German.DL.1080p.BluRay-SEA",
				"Lantern.Harbor.S02E04.German.DL.1080p.BluRay-OTHER",
				"Moonlit.Harbor.S01E04.German.DL.1080p.BluRay-NOISE",
			}
		}
		var items strings.Builder
		for _, title := range titles {
			fmt.Fprintf(&items, `<item><title>%s</title><guid>https://indexer.invalid/details/%[1]s</guid><link>http://prowlarr/1/download</link><size>1</size></item>`, title)
		}
		fmt.Fprintf(w, `<rss><channel>%s</channel></rss>`, items.String())
	}))
	defer indexer.Close()

	s := New(st, prowlarr.New(indexer.URL, "key", indexer.Client()), nil, testprofiles.ByType, log)
	sr, err := s.Episode(ctx, 20001)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	asked := slices.Clone(texts)
	mu.Unlock()
	if want := []string{"Lantern Harbor 04", "Laternenhaefen 04"}; !slices.Equal(asked, want) {
		t.Errorf("text searches %q, want %q", asked, want)
	}
	var got []string
	for _, c := range sr.Candidates {
		got = append(got, fmt.Sprintf("%s %s", c.Title[strings.LastIndex(c.Title, "-")+1:], c.Verdict))
	}
	slices.Sort(got)
	if want := []string{"ABS accept", "ONE reject", "SEA accept"}; !slices.Equal(got, want) || sr.Indexers[0].Results != 3 {
		t.Errorf("candidates %q from %d results, want %q", got, sr.Indexers[0].Results, want)
	}
	if sr.Outcome != api.SearchFound {
		t.Errorf("outcome %s", sr.Outcome)
	}

	// A failed query neither stops the others nor fails the indexer while
	// another one was answered; only when none was is the indexer failed.
	for _, tc := range []struct {
		aired, text bool
		want        api.SearchOutcome
	}{{true, false, api.SearchFound}, {false, true, api.SearchNothing}, {true, true, api.SearchFailed}} {
		mu.Lock()
		failAired, failText, texts = tc.aired, tc.text, nil
		mu.Unlock()
		sr, err := s.Episode(ctx, 20001)
		mu.Lock()
		n := len(texts)
		mu.Unlock()
		partial := strings.Contains(sr.Summary, "One answered only some searches")
		if err != nil || sr.Outcome != tc.want || n != 2 || partial != (tc.want != api.SearchFailed) {
			t.Errorf("aired fails %v, text fails %v: %s %q after %d text searches (%v), want %s", tc.aired, tc.text, sr.Outcome, sr.Summary, n, err, tc.want)
		}
	}
}

// TestOtherShowByYear keeps rejecting what the indexer files under another
// show, unless the name gives the year this show first aired: indexers
// confuse a show with its original or remake of the same name.
func TestOtherShowByYear(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)
	st, err := store.Open(ctx, t.TempDir(), log)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	d := series(store.TypeAnime, 3, 3)
	d.Series.Year = 2011
	if err := st.SaveSeries(ctx, d, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/indexer" {
			fmt.Fprint(w, `[{"id":1,"name":"One","enable":true,"protocol":"usenet"}]`)
			return
		}
		var items strings.Builder
		for title, show := range map[string]int{
			"Lantern.Harbor.E04.Some.x.Title.German.2011.ANiME.DL.1080p.BluRay-YEAR": 77,
			"Lantern.Harbor.E04.German.DL.1080p.BluRay-NOYEAR":                       77,
			"Lantern.Harbor.1999.E04.German.DL.1080p.BluRay-OLD":                     77,
			"Lantern.Harbor.E04.German.DL.1080p.WEB-OWN":                             42,
		} {
			fmt.Fprintf(&items, `<item><title>%s</title><guid>https://indexer.invalid/details/%[1]s</guid><link>http://prowlarr/1/download</link><size>1</size>`+
				`<newznab:attr name="tvdbid" value="%d"/></item>`, title, show)
		}
		fmt.Fprintf(w, `<rss xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel>%s</channel></rss>`, items.String())
	}))
	defer indexer.Close()

	sr, err := New(st, prowlarr.New(indexer.URL, "key", indexer.Client()), nil, testprofiles.ByType, log).Episode(ctx, 20001)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range sr.Candidates {
		group := c.Title[strings.LastIndex(c.Title, "-")+1:]
		other := slices.Contains(c.Reasons, "the indexer files it under another show")
		noted := slices.ContainsFunc(c.Reasons, func(r string) bool { return strings.Contains(r, "its name gives 2011") })
		want := map[string]bool{"YEAR": false, "NOYEAR": true, "OLD": true, "OWN": false}[group]
		if other != want || noted != (group == "YEAR") || (c.Verdict == api.Reject) != want {
			t.Errorf("%s: %s %q", group, c.Verdict, c.Reasons)
		}
	}
	if len(sr.Candidates) != 4 {
		t.Errorf("%d candidates", len(sr.Candidates))
	}
}
