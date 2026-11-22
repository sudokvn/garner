package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/sudokvn/garner/internal/api"
	"github.com/sudokvn/garner/internal/client"
	"github.com/sudokvn/garner/internal/connect"
	"github.com/sudokvn/garner/internal/profile"
)

// TestEditorTrial checks trying unsaved changes on a search: the server
// is asked once the profile rests, with the unsaved text, about a show
// that uses the profile and misses an episode; the pane shows the release
// whose verdict changes as was and now, and the release still first; t
// keeps the show when no other search can be judged.
func TestEditorTrial(t *testing.T) {
	var mu sync.Mutex
	judged := map[string][]string{} // episode → profile texts sent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		episode := func(id int64, n int, aired string, state api.EpisodeState) (api.Episode, api.EpisodeStatus) {
			return api.Episode{ID: id, Aired: aired, Numbers: map[string]api.Position{"official": {Season: 1, Number: n}}},
				api.EpisodeStatus{EpisodeID: id, State: state, Summary: "Found 3 matching releases"}
		}
		detail := func(id int64, name string, eps ...int64) api.SeriesDetail {
			d := api.SeriesDetail{SeriesData: api.SeriesData{Series: api.Series{ID: id, Name: name, DefaultOrder: "official", ProfileID: 1}}, LanguageName: "Japanese"}
			for i, ep := range eps {
				state := api.EpisodeImported
				if i == len(eps)-1 && id == 11 {
					state = api.EpisodeWaiting
				}
				e, st := episode(ep, i+1, fmt.Sprintf("2026-01-%02d", i+1), state)
				d.Episodes, d.States = append(d.Episodes, e), append(d.States, st)
			}
			return d
		}
		switch {
		case r.URL.Path == "/api/v1/series":
			json.NewEncoder(w).Encode([]api.Show{
				{Series: api.Series{ID: 10, Name: "Copper Valley", ProfileID: 1}},
				{Series: api.Series{ID: 11, Name: "Harbor Precinct", ProfileID: 1}, Counts: map[api.EpisodeState]int{api.EpisodeWaiting: 1}},
				{Series: api.Series{ID: 12, Name: "Glass Orchard", ProfileID: 2}, Counts: map[api.EpisodeState]int{api.EpisodeWaiting: 1}},
			})
		case r.URL.Path == "/api/v1/series/10":
			json.NewEncoder(w).Encode(detail(10, "Copper Valley", 100))
		case r.URL.Path == "/api/v1/series/11":
			json.NewEncoder(w).Encode(detail(11, "Harbor Precinct", 110, 111))
		case strings.HasSuffix(r.URL.Path, "/search/judge"):
			var body struct{ Text string }
			raw, _ := io.ReadAll(r.Body)
			json.Unmarshal(raw, &body)
			mu.Lock()
			judged[r.URL.Path] = append(judged[r.URL.Path], body.Text)
			mu.Unlock()
			if r.URL.Path != "/api/v1/episodes/111/search/judge" {
				w.WriteHeader(http.StatusConflict)
				fmt.Fprint(w, `{"error":"this search is too old to judge again; search again"}`)
				return
			}
			keep := api.Judged{Verdict: api.Accept, Tier: "Japanese 1080p", Rank: 1}
			json.NewEncoder(w).Encode(api.Judgement{EpisodeID: 111, Searched: time.Now(), Candidates: []api.JudgedCandidate{
				{ID: 1, Title: "Harbor.Precinct.S01E02.1080p.WEB-DL.H.264-GRP", Before: keep, After: keep},
				{ID: 2, Title: "Harbor.Precinct.S01E02.English.Dub.1080p.WEB-DL.H.264-DUB",
					Before: api.Judged{Verdict: api.Reject, Rank: 3, Reasons: []string{"audio is English, not Japanese"}, Decisive: 1},
					After:  api.Judged{Verdict: api.Accept, Tier: "English 1080p", Rank: 2}},
				{ID: 3, Title: "Harbor.Precinct.S01E02.720p.HDTV-OLD", Before: api.Judged{Verdict: api.Reject, Rank: 2, Reasons: []string{"720p HDTV is not wanted"}, Decisive: 1},
					After: api.Judged{Verdict: api.Reject, Rank: 3, Reasons: []string{"720p HDTV is not wanted"}, Decisive: 1}},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	cl, err := client.New(connect.Code{URL: srv.URL, Token: "garner_" + strings.Repeat("a", 26)})
	if err != nil {
		t.Fatal(err)
	}
	colourless = true
	t.Cleanup(func() { colourless = false })

	a, ed := editorApp(cl, 1)
	// Dual audio: prefer, back to ignore, prefer again.
	sendKeys(a, "j", "j", "j", "j", "j", "h", "l", "h")
	if _, cmd := ed.update(trialTickMsg{ed, ed.trial.seq - 1}); cmd != nil {
		t.Fatal("a tick of an earlier change asked the server")
	}
	_, cmd := ed.update(trialTickMsg{ed, ed.trial.seq})
	if cmd == nil {
		t.Fatal("the tick after the last change did not ask the server")
	}
	ed.update(cmd())
	sent := judged["/api/v1/episodes/111/search/judge"]
	if len(sent) != 1 || sent[0] != profile.Encode(ed.p) {
		t.Fatalf("the server judged %d texts for the missing episode, the unsaved profile once was expected", len(sent))
	}
	view := ansi.Strip(ed.view(120, 50))
	for _, want := range []string{
		"Tried on       Harbor Precinct S01E02 · its last search, ",
		"original language Japanese",
		"Harbor.Precinct.S01E02.English.Dub.1080p.WEB-DL.H.264-DUB",
		"was  ✗ rejected · audio is English, not Japanese",
		"now  ✓ accepted · English 1080p · last of the 2 accepted releases",
		"Still first: Harbor.Precinct.S01E02.1080p.WEB-DL.H.264-GRP · Japanese 1080p",
		"No other verdict changes.",
		"If you save    all 2 shows follow the new rules from their next search. Nothing is searched now.",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the pane lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "HDTV-OLD") {
		t.Error("the pane lists a release whose verdict did not change")
	}

	// Unnamed audio changes, and t comes before the wait ends. Copper
	// Valley's search is too old: Harbor Precinct stays and is judged
	// again under the profile as it is now.
	sendKeys(a, "j", "h")
	_, cmd = ed.update(keyMsg("t"))
	_, cmd = ed.update(cmd())
	if ed.trial.pick == nil || ed.trial.pick.episode != 111 || !strings.Contains(ansi.Strip(ed.flash.text), "No other show") {
		t.Errorf("t without another judgeable search: pick %+v, message %q", ed.trial.pick, ansi.Strip(ed.flash.text))
	}
	if len(judged["/api/v1/episodes/100/search/judge"]) != 1 {
		t.Error("t did not try the other show")
	}
	if cmd == nil {
		t.Fatal("the kept judgement of an earlier profile was not asked again")
	}
	ed.update(cmd())
	if sent := judged["/api/v1/episodes/111/search/judge"]; len(sent) != 2 || sent[1] != profile.Encode(ed.p) || !ed.trial.current(judgeKey(ed.p)) {
		t.Errorf("Harbor Precinct was judged %d times, the second not under the profile as it is", len(sent))
	}
}
