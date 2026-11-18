package prowlarr

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func item(title string, langs ...string) string {
	var attrs strings.Builder
	for _, l := range langs {
		fmt.Fprintf(&attrs, `<newznab:attr name="language" value="%s"/>`, l)
	}
	return fmt.Sprintf(`<item><title>%s</title><link>http://prowlarr/1/download?apikey=SECRET&amp;link=X</link><size>1000</size>
		<pubDate>Thu, 01 Oct 2026 19:28:29 +0000</pubDate>%s<newznab:attr name="tvdbid" value="42"/><newznab:attr name="grabs" value="7"/></item>`, title, attrs.String())
}

func feedOf(items ...string) string {
	return `<?xml version="1.0"?><rss xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel>` +
		strings.Join(items, "") + `</channel></rss>`
}

func TestSearch(t *testing.T) {
	var mu sync.Mutex
	var queries []string
	asked := func() []string { mu.Lock(); defer mu.Unlock(); return slices.Clone(queries) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "key" || r.URL.Query().Has("apikey") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/indexer":
			fmt.Fprint(w, `[{"id":1,"name":"One","enable":true,"protocol":"usenet"},{"id":2,"name":"Two","enable":true,"protocol":"usenet"},
				{"id":3,"name":"Off","enable":false,"protocol":"usenet"},{"id":4,"name":"Torrents","enable":true,"protocol":"torrent"}]`)
		case "/1/api":
			mu.Lock()
			queries = append(queries, r.URL.RawQuery)
			mu.Unlock()
			// A full first page makes the client ask for the next one.
			n := 100
			if r.URL.Query().Get("offset") != "0" {
				n = 2
			}
			items := make([]string, n)
			for i := range items {
				items[i] = item(fmt.Sprintf("Show.S01E01.%d.German.DL.1080p.WEB-GRP", i), "German", "English")
			}
			fmt.Fprint(w, feedOf(items...))
		case "/2/api":
			fmt.Fprint(w, `<?xml version="1.0"?><error code="100" description="Incorrect user credentials"/>`)
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "key", srv.Client())
	ctx := context.Background()

	indexers, err := c.Indexers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(indexers) != 2 || indexers[0].Name != "One" || indexers[1].Name != "Two" {
		t.Fatalf("indexers = %+v", indexers)
	}

	start := time.Now()
	results, err := c.Search(ctx, 1, Query{TVDBID: 42, Season: 1, Episode: 1})
	if err != nil {
		t.Fatal(err)
	}
	if q := asked(); len(results) != 102 || len(q) != 2 || !strings.Contains(q[0], "tvdbid=42") || !strings.Contains(q[0], "ep=1") {
		t.Fatalf("%d results from queries %q", len(results), q)
	}
	if elapsed := time.Since(start); elapsed < requestGap {
		t.Errorf("two requests took %s; want at least %s apart", elapsed, requestGap)
	}
	r := results[0]
	if r.Size != 1000 || r.Grabs != 7 || r.TVDBID != 42 || !slices.Equal(r.Languages, []string{"German", "English"}) || r.Published.IsZero() {
		t.Errorf("result = %+v", r)
	}

	// A text search asks for the words alone, and only for one page.
	mu.Lock()
	queries = nil
	mu.Unlock()
	if results, err := c.Search(ctx, 1, Query{TVDBID: 42, Text: "Harbor Precinct 059"}); err != nil || len(results) != 100 {
		t.Fatalf("text search: %d results, %v", len(results), err)
	}
	if q := asked(); len(q) != 1 || !strings.Contains(q[0], "t=search") || !strings.Contains(q[0], "q=Harbor+Precinct+059") ||
		strings.Contains(q[0], "tvdbid") {
		t.Errorf("text search queries %q", q)
	}

	_, err = c.Search(ctx, 2, Query{TVDBID: 42, Season: 1})
	if err == nil || !strings.Contains(err.Error(), "Incorrect user credentials") || QueryFailed(err) {
		t.Errorf("error = %v, only the query failed: %v", err, QueryFailed(err))
	}
	// A server that does not answer fails as a whole.
	gone := New("http://prowlarr.invalid", "key", &http.Client{Transport: refused{}})
	if _, err := gone.Search(ctx, 1, Query{TVDBID: 42, Season: 1}); err == nil || QueryFailed(err) {
		t.Errorf("unreachable: error = %v, only the query failed: %v", err, QueryFailed(err))
	}
}

// TestPostings tells postings of the same release apart by the indexer's
// ID for them, not by the link Prowlarr encrypts anew for every search, and
// keeps the ID itself, which may hold credentials, out of the result.
func TestPostings(t *testing.T) {
	const title = "Harbor.Precinct.S01E01.German.DL.1080p.WEB-GRP"
	posting := func(indexer int, guid, link string) string {
		t.Helper()
		it := strings.Replace(item(title), "<title>", "<guid>"+guid+"</guid><title>", 1)
		it = strings.Replace(it, "link=X", "link="+link, 1)
		results, err := parseFeed([]byte(feedOf(it)), indexer)
		if err != nil || len(results) != 1 {
			t.Fatalf("parse: %v, %d results", err, len(results))
		}
		if r := fmt.Sprintf("%+v", results[0]); strings.Contains(r, "SECRET-ID") {
			t.Errorf("the result holds the indexer's ID: %s", r)
		}
		return results[0].Posting
	}
	first := posting(1, "https://indexer.invalid/details/a1?apikey=SECRET-ID", "L1")
	switch {
	case first == "":
		t.Error("no posting")
	case posting(1, "https://indexer.invalid/details/a1?apikey=SECRET-ID", "L2") != first:
		t.Error("a new link made the same posting another one")
	case posting(1, "https://indexer.invalid/details/b2?apikey=SECRET-ID", "L1") == first:
		t.Error("two postings of the same release are one")
	case posting(2, "https://indexer.invalid/details/a1?apikey=SECRET-ID", "L1") == first:
		t.Error("the same ID at another indexer is the same posting")
	case posting(1, "", "L1") == "" || posting(1, "", "L1") != posting(1, "", "L2"):
		t.Error("without an ID, the name, size and time do not stand in")
	}
}

// TestPages keeps the address of a release's page at the indexer only when
// it cannot hold credentials, taking the comments link, else the ID.
func TestPages(t *testing.T) {
	for raw, want := range map[string]string{
		"https://indexer.invalid/details/a1b2":                       "https://indexer.invalid/details/a1b2",
		"https://indexer.invalid/geekseek.php?guid=a1b2":             "https://indexer.invalid/geekseek.php?guid=a1b2",
		"https://indexer.invalid/details/a1b2#comments":              "https://indexer.invalid/details/a1b2",
		"https://indexer.invalid/details/a1b2?apikey=SECRET":         "",
		"https://indexer.invalid/getnzb/a1b2.nzb?i=7&r=SECRET":       "",
		"https://indexer.invalid/details?id=a1b2&token=SECRET":       "",
		"https://user:SECRET@indexer.invalid/details/a1b2":           "",
		"https://indexer.invalid/api/SECRET/details/a1b2":            "",
		"https://indexer.invalid/details/a1b2/SECRET":                "",
		"https://indexer.invalid/SECRET/geekseek.php?guid=a1b2":      "",
		"https://indexer.invalid/details/a1b2?guid=a1b2":             "",
		"https://indexer.invalid/geekseek.php?guid=a1b2&guid=SECRET": "",
		"ftp://indexer.invalid/details/a1b2":                         "",
		"a1b2c3":                                                     "",
		"":                                                           "",
	} {
		if got := page(raw); got != want {
			t.Errorf("page(%q) = %q, want %q", raw, got, want)
		}
	}
	it := func(guid, comments string) string {
		x := strings.Replace(item("Harbor.Precinct.S01E01.German.DL.1080p.WEB-GRP"), "<title>", guid+comments+"<title>", 1)
		results, err := parseFeed([]byte(feedOf(x)), 1)
		if err != nil || len(results) != 1 {
			t.Fatalf("parse: %v, %d results", err, len(results))
		}
		return results[0].Page
	}
	if got := it("<guid>https://indexer.invalid/details/g1</guid>", "<comments>https://indexer.invalid/details/c1#comments</comments>"); got != "https://indexer.invalid/details/c1" {
		t.Errorf("page from the comments link = %q", got)
	}
	if got := it("<guid>https://indexer.invalid/details/g1</guid>", ""); got != "https://indexer.invalid/details/g1" {
		t.Errorf("page from the ID = %q", got)
	}
	if got := it("<guid>https://indexer.invalid/details/g1</guid>", "<comments>https://indexer.invalid/api/SECRET/c1</comments>"); got != "https://indexer.invalid/details/g1" {
		t.Errorf("page from the ID after an unusable comments link = %q", got)
	}
}

// refused is a transport whose every connection is refused.
type refused struct{}

func (refused) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connect: connection refused")
}

// TestQueryFailed tells failures of one query, after which another kind of
// query may still work, from failures of the whole indexer, after which
// garner asks it nothing more in that search.
func TestQueryFailed(t *testing.T) {
	for body, want := range map[string]bool{
		`<error code="203" description="Function not available"/>`:     true,
		`<error code="201" description="Incorrect parameter"/>`:        true,
		`<error code="100" description="Incorrect user credentials"/>`: false,
		`<error code="500" description="Request limit reached"/>`:      false,
		`<error code="900" description="Indexer is not responding"/>`:  false,
	} {
		e, ok := newznabError([]byte(body))
		if !ok || QueryFailed(e) != want {
			t.Errorf("%s: only the query failed = %v, want %v", body, QueryFailed(e), want)
		}
	}
	for status, want := range map[int]bool{http.StatusBadRequest: true, http.StatusTooManyRequests: false, http.StatusUnauthorized: false, http.StatusBadGateway: false} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		_, err := New(srv.URL, "key", srv.Client()).Search(context.Background(), 1, Query{TVDBID: 42, Season: 1})
		srv.Close()
		if err == nil || QueryFailed(err) != want {
			t.Errorf("HTTP %d: %v, only the query failed = %v, want %v", status, err, QueryFailed(err), want)
		}
	}
}
