package simulation

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/trucke/garner/internal/connect"
	"github.com/trucke/garner/internal/inspect"
	"github.com/trucke/garner/internal/server"
	"github.com/trucke/garner/internal/store"
)

// The demo serves garner on a fixed world, for recording the terminal
// interface with a VHS tape: mise run demo <tape> runs both (tools/demo.sh).
// It runs only with -demo and serves until it is stopped.
var (
	demoFlag       = flag.Bool("demo", false, "serve garner on the demo world until stopped")
	demoAddr       = flag.String("demo-addr", "127.0.0.1:0", "where the demo serves garner")
	demoConnection = flag.String("demo-connection", "", "write the demo's connection code to this file once it serves")
	// A download takes six ticks.
	demoTick = flag.Duration("demo-tick", 600*time.Millisecond, "how often the demo's world and garner's loops move on")
	// The interface shows paths in the library, so a recording wants a
	// short one rather than a temporary folder's.
	demoRoot = flag.String("demo-root", "", "build the demo's share and database in this empty folder instead of a temporary one")
)

func TestDemo(t *testing.T) {
	if !*demoFlag {
		t.Skip("runs only with -demo")
	}
	if *demoConnection == "" {
		t.Fatal("-demo-connection is required")
	}
	ctx, stop := signal.NotifyContext(t.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := *demoRoot
	if root == "" {
		root = t.TempDir()
	} else if entries, err := os.ReadDir(root); err != nil || len(entries) > 0 {
		t.Fatalf("-demo-root %s must be an empty folder (%v)", root, err)
	}
	w, err := newDemoWorld(root, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	logFile, err := os.Create(filepath.Join(root, "garner.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	log := slog.New(slog.NewTextHandler(logFile, nil))

	cfg := w.config()
	cfg.Monitor.Enabled = false // only what the recording asks for happens
	// The library's screens need the catalogue. TheTVDB is not simulated:
	// the shows come from the world, and every request to TheTVDB fails.
	cfg.TVDB.APIKey = "demo"
	st, err := store.Open(ctx, cfg.DataDir, log)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	// The world's fakes expect one caller at a time; the demo serves many.
	var mu sync.Mutex
	deps := server.Deps{Transport: lockedTransport{&mu, transport{w}}, Prober: demoProber{}}
	srv, err := server.New(cfg, st, "demo", deps, log)
	if err != nil {
		t.Fatal(err)
	}
	token, err := st.CreateToken(ctx, "demo")
	if err != nil {
		t.Fatal(err)
	}
	api := srv.Routes()
	for _, s := range w.shows {
		if err := w.seedShow(ctx, st, s, store.MonitorAll); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/series/%d/files/take-over", s.id), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("take over %s: %d %s", s.name, rec.Code, rec.Body)
		}
	}

	ln, err := net.Listen("tcp", *demoAddr)
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: api, ReadHeaderTimeout: 10 * time.Second}
	go hs.Serve(ln)
	defer hs.Close()
	code := connect.Code{URL: "http://" + ln.Addr().String(), Token: token}
	if err := connect.Save(*demoConnection, code); err != nil {
		t.Fatal(err)
	}
	t.Logf("serving on %s; the log is %s", code.URL, logFile.Name())

	go every(ctx, *demoTick, func() {
		mu.Lock()
		defer mu.Unlock()
		w.now = time.Now()
		for _, j := range w.sab.jobs {
			if !j.active() {
				continue
			}
			if _, err := w.progress(j); err != nil {
				log.Error("demo: SABnzbd job", "job", j.id, "err", err)
			} else if j.status == "Completed" {
				if err := inflateJob(j); err != nil {
					log.Error("demo: inflate the job's videos", "job", j.id, "err", err)
				}
			}
		}
		w.scan()
	})
	go every(ctx, *demoTick, func() {
		srv.StepMonitor(ctx) // searches the shows asked for; the schedule is off
		srv.PollDownloads(ctx)
		srv.WorkImports(ctx)
	})
	<-ctx.Done()
}

func every(ctx context.Context, d time.Duration, f func()) {
	tick := time.NewTicker(d)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			f()
		}
	}
}

type lockedTransport struct {
	mu *sync.Mutex
	t  transport
}

func (l lockedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.t.RoundTrip(r)
}

// demoKind is a release as the demo describes it: what its name says,
// what the indexers report and what FFprobe finds.
type demoKind struct {
	tags     string
	reported []string // the indexers' languages
	audio    []string
	height   int
}

var (
	webEnglish  = demoKind{"1080p.WEB-DL.H.264", []string{"English"}, []string{"eng"}, 1080}
	webJapanese = demoKind{"1080p.WEB-DL.H.264", []string{"Japanese"}, []string{"jpn"}, 1080}
	webGerman   = demoKind{"German.1080p.WEB-DL.H.264", []string{"German"}, []string{"ger"}, 1080}
)

// demoLibrary are the shows the library holds in full.
var demoLibrary = []struct {
	name              string
	year              int
	typ, lang, status string
	seasons, episodes int
	runtime           int
	kind              demoKind
	group             string
}{
	{"Glass Orchard", 2021, store.TypeTV, "eng", "", 2, 10, 45, webEnglish, "ORCHARD"},
	{"Harbor Precinct", 2013, store.TypeTV, "eng", "Ended", 3, 10, 45, webEnglish, "WEBTEAM"},
	{"Copper Valley", 2024, store.TypeTV, "eng", "Ended", 1, 5, 50, webEnglish, "GRP"},
	{"Night Ferry", 2022, store.TypeTV, "deu", "", 2, 8, 50, webGerman, "FERRY"},
	{"Quiet Harbour", 2023, store.TypeAnime, "jpn", "", 1, 12, 24, webJapanese, "TIDE"},
	{"Salt and Signal", 2025, store.TypeTV, "eng", "", 1, 10, 45, webEnglish, "WEBTEAM"},
	{"The Iron Lighthouse", 2016, store.TypeTV, "eng", "Ended", 4, 10, 45, webEnglish, "GRP"},
}

// newDemoWorld builds the demo's library: shows garner has, with their
// files, and one new episode of an anime whose best-looking release is
// an English dub that its name and the indexers call Japanese. The file
// check catches it, and garner downloads the next release instead.
func newDemoWorld(root string, now time.Time) (*world, error) {
	w := &world{seed: 1, root: root, now: now, rnd: rand.New(rand.NewPCG(1, 1)),
		indexerError: map[int]bool{}, foreign: map[string]foreignFile{}}
	w.jf.items, w.jf.folders = map[string]*jfItem{}, map[string]int64{}
	for _, dir := range []string{"usenet/complete/tv", "usenet/incomplete", "library/tv", "library/anime", "media-old/tv", "garner"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			return nil, err
		}
	}
	const week = 7 * 24 * time.Hour

	lanterns := w.demoShow(7101, "Lanterns and Tides", 2025, store.TypeAnime, "jpn", "")
	titles := []string{"The Lamplighter's Daughter", "Low Water", "Salt on the Wick", "A Ferry at Dusk", "The Harbour Count",
		"Driftglass", "What the Tide Keeps", "Ember Season", "The Long Breakwater", "Fog Lanterns", "The Night Market", "High Water"}
	for i, title := range titles {
		// Episode 11 aired yesterday, episode 12 airs next week.
		e := w.demoEpisode(lanterns, 1, i+1, now.Add(time.Duration(i-10)*week-24*time.Hour), 24)
		e.title = title
		if i < 10 {
			if err := w.demoFile(e, webJapanese, "TIDE"); err != nil {
				return nil, err
			}
		}
	}
	e11 := lanterns.episodes[10]
	for _, r := range []struct {
		kind   demoKind
		group  string
		posted time.Duration // after the episode aired
	}{
		// The dub: the name and the indexers say Japanese, the file has English audio.
		{demoKind{"1080p.WEB-DL.H.264", []string{"Japanese"}, []string{"eng"}, 1080}, "TIDE", 3 * time.Hour},
		{demoKind{"1080p.WEBRip.x264", []string{"Japanese"}, []string{"jpn"}, 1080}, "ORCHARD", 5 * time.Hour},
		{demoKind{"720p.WEB-DL.H.264", []string{"Japanese"}, []string{"jpn"}, 720}, "TIDE", 3 * time.Hour},
		{demoKind{"English.Dub.1080p.WEB-DL.H.264", []string{"English"}, []string{"eng"}, 1080}, "FERRY", 6 * time.Hour},
		{demoKind{"2160p.WEB-DL.H.265", []string{"Japanese"}, []string{"jpn"}, 2160}, "TIDE", 4 * time.Hour},
	} {
		w.demoPosting(e11, r.kind, r.group, e11.aired.Add(r.posted), true)
	}

	for i, l := range demoLibrary {
		s := w.demoShow(int64(7102+i), l.name, l.year, l.typ, l.lang, l.status)
		total := l.seasons * l.episodes
		// One episode a week, the last some weeks ago, or years ago for
		// an ended show.
		last := now.Add(-time.Duration(3+i) * week)
		if l.status == "Ended" {
			last = now.AddDate(-1-i%3, 0, 0)
		}
		for season := 1; season <= l.seasons; season++ {
			for n := 1; n <= l.episodes; n++ {
				left := total - ((season-1)*l.episodes + n)
				e := w.demoEpisode(s, season, n, last.Add(-time.Duration(left)*week), l.runtime)
				if err := w.demoFile(e, l.kind, l.group); err != nil {
					return nil, err
				}
			}
		}
	}

	// Jellyfin scanned the library before the demo began.
	w.jf.refresh = true
	w.scan()
	return w, nil
}

func (w *world) demoShow(id int64, name string, year int, typ, language, status string) *show {
	s := &show{id: id, name: name, year: year, monitor: store.MonitorAll, profile: typ, added: true, language: language, status: status}
	w.shows = append(w.shows, s)
	return s
}

func (w *world) demoEpisode(s *show, season, number int, aired time.Time, runtime int) *episode {
	s.numbered++
	e := &episode{id: s.id*100 + int64(s.numbered), show: s, season: season, number: number, aired: aired, runtime: runtime}
	s.episodes = append(s.episodes, e)
	return e
}

// demoPosting puts a release of an episode on the indexers, or with
// indexed false makes the release a file in the library came from.
func (w *world) demoPosting(e *episode, k demoKind, group string, posted time.Time, indexed bool) *posting {
	title := fmt.Sprintf("%s.S%02dE%02d.%s-%s", e.show.dotted(), e.season, e.number, k.tags, group)
	p := &posting{id: len(w.postings) + 1, title: title, show: e.show, season: e.season, episodes: []*episode{e}, posted: posted,
		languages: k.reported, audio: k.audio, height: k.height, longer: 1,
		size: int64(e.runtime) * map[int]int64{720: 15, 1080: 31, 2160: 80}[k.height] << 20}
	if indexed {
		// One indexer each: the same upload on both would list every
		// release twice.
		p.indexers = []int{1 + p.id%2}
	}
	w.postings = append(w.postings, p)
	return p
}

// demoFile puts an episode's file into garner's folder of the show, named
// the way garner names its files.
func (w *world) demoFile(e *episode, k demoKind, group string) error {
	p := w.demoPosting(e, k, group, e.aired, false)
	library := "library/tv"
	if e.show.profile == store.TypeAnime {
		library = "library/anime"
	}
	var langs []string
	for _, a := range k.audio {
		langs = append(langs, map[string]string{"eng": "EN", "ger": "DE", "jpn": "JA"}[a])
	}
	title := strings.NewReplacer(" ", ".", "'", "").Replace(e.name())
	name := fmt.Sprintf("%s.S%02dE%02d.%s.WEB-%dp.%s-%s.mkv", e.show.garnerName(), e.season, e.number, title, k.height, strings.Join(langs, "."), group)
	dir := filepath.Join(w.root, library, fmt.Sprintf("%s.[tvdbid-%d]", e.show.garnerName(), e.show.id), fmt.Sprintf("Season.%02d", e.season))
	if err := w.newVideo(filepath.Join(dir, name), p, []*episode{e}, false); err != nil {
		return err
	}
	// The size of a real video, so the interface shows real sizes. The file
	// stays sparse, and demoProber reads only its start.
	return os.Truncate(filepath.Join(dir, name), p.size)
}

// inflateJob gives the videos SABnzbd put into a completed job's folder,
// which world.complete writes small, the size of real ones.
func inflateJob(j *job) error {
	entries, err := os.ReadDir(j.dir)
	if err != nil {
		return err
	}
	for _, d := range entries {
		p := filepath.Join(j.dir, d.Name())
		if v, ok := readVideo(p); ok && !v.Extra {
			if err := os.Truncate(p, j.posting.size/int64(len(j.posting.episodes))); err != nil {
				return err
			}
		}
	}
	return nil
}

// demoProber is prober for inflated videos: it reads what the video holds
// from the start of the file.
type demoProber struct{}

func (demoProber) Probe(_ context.Context, path string) (inspect.Probe, error) {
	f, err := os.Open(path)
	if err != nil {
		return inspect.Probe{}, err
	}
	defer f.Close()
	head := make([]byte, 4096)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return inspect.Probe{}, err
	}
	return probeVideo(bytes.TrimRight(head[:n], "\x00"))
}
