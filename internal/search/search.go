// Package search finds releases for an episode: it queries every indexer
// through Prowlarr, matches each result to the catalogue, judges it against
// the show's profile and stores the ranked outcome.
package search

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/sudokvn/garner/internal/api"
	"github.com/sudokvn/garner/internal/match"
	"github.com/sudokvn/garner/internal/prowlarr"
	"github.com/sudokvn/garner/internal/release"
	"github.com/sudokvn/garner/internal/selection"
	"github.com/sudokvn/garner/internal/store"
)

type Service struct {
	store    *store.Store
	prowlarr *prowlarr.Client
	// rules returns a show's reviewed streaming-service order rules.
	rules func(seriesID int64) map[release.Service]string
	// profile applies a show's stored profile to the show.
	profile func(store.Series) selection.Profile
	log     *slog.Logger
	// Now tells the time: time.Now, or the simulation's clock.
	Now func() time.Time
	// OnDone is called after a search was stored.
	OnDone func(store.Search)
	// OnProgress is called when a search starts asking the indexers and
	// after each of them answered.
	OnProgress func(api.SearchProgress)
}

// New returns a search service that judges releases by the profile that
// profile applies to a show. rules may be nil.
func New(st *store.Store, p *prowlarr.Client, rules func(int64) map[release.Service]string,
	profile func(store.Series) selection.Profile, log *slog.Logger) *Service {
	if rules == nil {
		rules = func(int64) map[release.Service]string { return nil }
	}
	return &Service{store: st, prowlarr: p, rules: rules, profile: profile, log: log, Now: time.Now}
}

// Episode searches for an episode and stores the outcome, replacing the
// previous search.
func (s *Service) Episode(ctx context.Context, episodeID int64) (store.Search, error) {
	seriesID, err := s.store.SeriesOfEpisode(ctx, episodeID)
	if err != nil {
		return store.Search{}, err
	}
	d, err := s.store.LoadSeries(ctx, seriesID)
	if err != nil {
		return store.Search{}, err
	}
	pos, err := position(d, episodeID)
	if err != nil {
		return store.Search{}, err
	}

	files, err := s.store.SeriesFileTiers(ctx, seriesID)
	if err != nil {
		return store.Search{}, err
	}
	start := s.Now()
	profile := s.profile(d.Series)
	show := match.NewShow(d, match.Options{Anime: d.Series.Type == store.TypeAnime, ServiceOrders: s.rules(seriesID)})
	judge := episodeContext(d, episodeID, pos, files, profile, show, start)

	sr := store.Search{EpisodeID: episodeID, Searched: start, Indexers: []store.SearchIndexer{}}
	var hits []hit
	indexers, err := s.prowlarr.Indexers(ctx)
	if err != nil {
		sr.Indexers = append(sr.Indexers, store.SearchIndexer{Name: "Prowlarr", Error: err.Error(), Partial: new(bool)})
	}
	progress := api.SearchProgress{EpisodeID: episodeID, Indexers: make([]api.IndexerProgress, len(indexers))}
	for i, ix := range indexers {
		progress.Indexers[i] = api.IndexerProgress{Name: ix.Name}
	}
	s.report(progress)
	queries := []prowlarr.Query{{TVDBID: seriesID, Season: pos.Season, Episode: pos.Number}}
	for _, text := range absoluteQueries(d.Series, show, episodeID, pos) {
		queries = append(queries, prowlarr.Query{TVDBID: seriesID, Text: text})
	}
	for i, ix := range indexers {
		res := store.SearchIndexer{Name: ix.Name, Partial: new(bool)}
		seen := map[string]bool{} // postings, as several queries may find one
		answered, failure := false, ""
		for _, q := range queries {
			results, err := s.prowlarr.Search(ctx, ix.ID, q)
			if ctx.Err() != nil {
				return store.Search{}, ctx.Err()
			}
			kept := 0
			for _, r := range results {
				if seen[r.Posting] {
					continue
				}
				claims := release.Parse(r.Title)
				m := show.Match(claims, r.Published)
				// A text search also finds other shows and episodes that share
				// a word or digits; only what it was asked for is worth showing.
				if q.Text != "" && !slices.Contains(m.Episodes, episodeID) {
					continue
				}
				seen[r.Posting] = true
				kept++
				eval := evaluate(judge, d, r, claims, m)
				hits = append(hits, hit{indexer: ix.Name, result: r, eval: eval, episodes: m.Episodes})
				res.Results++
			}
			answered = answered || err == nil || kept > 0
			if err != nil {
				failure = cmp.Or(failure, err.Error())
				s.log.Warn("indexer search failed", "indexer", ix.Name, "episode", episodeID, "text", q.Text, "err", err)
				// An indexer that is unreachable, limits requests or refuses
				// the credentials would fail the other queries too; one that
				// refused only this query may answer another kind.
				if !prowlarr.QueryFailed(err) {
					break
				}
			}
		}
		// An indexer failed when none of its queries was answered; one that
		// answered only some of them searched, but not completely.
		switch {
		case !answered:
			res.Error = failure
		case failure != "":
			*res.Partial = true
		}
		sr.Indexers = append(sr.Indexers, res)
		progress.Indexers[i] = api.IndexerProgress{Name: ix.Name, Done: true, Results: res.Results, Error: res.Error}
		s.report(progress)
	}

	evals := make([]selection.Evaluation, len(hits))
	for i, h := range hits {
		evals[i] = h.eval
	}
	sr.Candidates = []store.Candidate{}
	for _, i := range selection.Rank(evals) {
		sr.Candidates = append(sr.Candidates, hits[i].candidate())
	}
	sr.Outcome, sr.Summary = outcome(selection.Summary(profile, judge.Have, evals), evals, sr.Indexers)
	if err := s.store.SaveSearch(ctx, sr); err != nil {
		return store.Search{}, err
	}
	saved, err := s.store.LoadSearch(ctx, episodeID)
	if err != nil {
		return store.Search{}, err
	}
	s.log.Info("searched", "series", seriesID, "episode", show.Label(episodeID), "outcome", saved.Outcome,
		"candidates", len(saved.Candidates), "duration", s.Now().Sub(start).Round(time.Millisecond))
	if s.OnDone != nil {
		s.OnDone(saved)
	}
	return saved, nil
}

// Season searches a whole season for packs: releases that hold several of
// its episodes. It asks each indexer once for the season; single episodes
// are left to the episode search, which asks for them precisely, while a
// season's answer may stop at Prowlarr's page limit on popular shows. The
// season is one of the show's default order, as the show lists it; the
// indexers are asked for the aired-order seasons its episodes belong to,
// usually just the one. The outcome replaces the season's previous search.
func (s *Service) Season(ctx context.Context, seriesID int64, season int) (store.Search, error) {
	d, err := s.store.LoadSeries(ctx, seriesID)
	if err != nil {
		return store.Search{}, err
	}
	episodes := seasonEpisodes(d, d.Series.DefaultOrder, season)
	var aired []int // the seasons indexers know them by
	for _, e := range d.Episodes {
		if p, ok := e.Numbers["official"]; ok && p.Season > 0 && slices.Contains(episodes, e.ID) && !slices.Contains(aired, p.Season) {
			aired = append(aired, p.Season)
		}
	}
	slices.Sort(aired)
	if season < 1 || len(aired) == 0 {
		return store.Search{}, fmt.Errorf("season %d: %w", season, store.ErrNotFound)
	}
	files, err := s.store.SeriesFileTiers(ctx, seriesID)
	if err != nil {
		return store.Search{}, err
	}
	start := s.Now()
	wanted := missing(d, episodes, files, start)
	profile := s.profile(d.Series)
	show := match.NewShow(d, match.Options{Anime: d.Series.Type == store.TypeAnime, ServiceOrders: s.rules(seriesID)})
	original := selection.OriginalLanguage(d.Series.OriginalLanguage)

	sr := store.Search{Season: season, Searched: start, Indexers: []store.SearchIndexer{}}
	var hits []hit
	indexers, err := s.prowlarr.Indexers(ctx)
	if err != nil {
		sr.Indexers = append(sr.Indexers, store.SearchIndexer{Name: "Prowlarr", Error: err.Error()})
	}
	progress := api.SearchProgress{SeriesID: seriesID, Season: season, Indexers: make([]api.IndexerProgress, len(indexers))}
	for i, ix := range indexers {
		progress.Indexers[i] = api.IndexerProgress{Name: ix.Name}
	}
	s.report(progress)
	for i, ix := range indexers {
		var results []prowlarr.Result
		var err error
		seen := map[string]bool{} // by link: a pack can span two seasons
		for _, n := range aired {
			var found []prowlarr.Result
			found, err = s.prowlarr.Search(ctx, ix.ID, prowlarr.Query{TVDBID: seriesID, Season: n})
			for _, r := range found {
				if !seen[r.Link] {
					seen[r.Link] = true
					results = append(results, r)
				}
			}
			if err != nil {
				break
			}
		}
		if ctx.Err() != nil {
			return store.Search{}, ctx.Err()
		}
		res := store.SearchIndexer{Name: ix.Name, Results: len(results)}
		if err != nil {
			res.Error = err.Error()
			s.log.Warn("indexer search failed", "indexer", ix.Name, "series", seriesID, "season", season, "err", err)
		}
		sr.Indexers = append(sr.Indexers, res)
		progress.Indexers[i] = api.IndexerProgress{Name: ix.Name, Done: true, Results: res.Results, Error: res.Error}
		s.report(progress)
		for _, r := range results {
			claims := release.Parse(r.Title)
			m := show.Match(claims, r.Published)
			held := slices.DeleteFunc(slices.Clone(episodes), func(id int64) bool { return !slices.Contains(m.Episodes, id) })
			if len(m.Episodes) < 2 || len(held) == 0 {
				continue // a single episode, or another season's
			}
			// Judged for its first missing episode, which a download of it
			// then belongs to.
			owner := held[0]
			if k := slices.IndexFunc(wanted, func(id int64) bool { return slices.Contains(held, id) }); k >= 0 {
				owner = wanted[k]
			}
			judge := selection.Context{Profile: profile, Show: show, Want: owner, Original: original, Missing: wanted}
			eval := evaluate(judge, d, r, claims, m)
			hits = append(hits, hit{indexer: ix.Name, result: r, eval: eval, episodes: m.Episodes, owner: owner})
		}
	}

	evals := make([]selection.Evaluation, len(hits))
	for i, h := range hits {
		evals[i] = h.eval
	}
	sr.Candidates = []store.Candidate{}
	var owners []int64
	for _, i := range selection.Rank(evals) {
		sr.Candidates = append(sr.Candidates, hits[i].candidate())
		owners = append(owners, hits[i].owner)
	}
	sr.Outcome, sr.Summary = outcome(selection.PackSummary(profile, evals), evals, sr.Indexers)
	if err := s.store.SaveSeasonSearch(ctx, seriesID, sr, owners); err != nil {
		return store.Search{}, err
	}
	saved, err := s.store.LoadSeasonSearch(ctx, seriesID, season)
	if err != nil {
		return store.Search{}, err
	}
	s.log.Info("searched a season", "series", seriesID, "season", season, "outcome", saved.Outcome,
		"packs", len(saved.Candidates), "duration", s.Now().Sub(start).Round(time.Millisecond))
	// No OnDone: a season search changes no episode's state, so clients
	// have nothing to reload; the caller gets the result itself.
	return saved, nil
}

// position is an episode's place in the aired order, which indexers know
// it by.
func position(d store.SeriesData, episodeID int64) (api.Position, error) {
	i := slices.IndexFunc(d.Episodes, func(e store.Episode) bool { return e.ID == episodeID })
	if i < 0 {
		return api.Position{}, fmt.Errorf("episode %d: %w", episodeID, store.ErrNotFound)
	}
	pos, ok := d.Episodes[i].Numbers["official"]
	if !ok {
		return api.Position{}, fmt.Errorf("episode %d has no aired-order position to search for", episodeID)
	}
	return pos, nil
}

// episodeContext is what judging an episode's releases needs as of now:
// the same for a search and for judging its results again.
func episodeContext(d store.SeriesData, episodeID int64, pos api.Position, files map[int64]string, profile selection.Profile,
	show *match.Show, now time.Time) selection.Context {
	return selection.Context{Profile: profile, Show: show, Want: episodeID, Original: selection.OriginalLanguage(d.Series.OriginalLanguage),
		Missing: missing(d, seasonEpisodes(d, "official", pos.Season), files, now), Have: files[episodeID]}
}

// evaluate judges a release an indexer returned, with the claims of its
// name and its match to the show: the same for a search and for judging
// its results again.
func evaluate(judge selection.Context, d store.SeriesData, r prowlarr.Result, claims release.Claims, m match.Match) selection.Evaluation {
	other := otherShow(judge.Show, d.Series, r.TVDBID, claims, &m)
	return selection.Evaluate(judge, selection.Release{
		Claims: claims, Match: m, IndexerLanguages: selection.IndexerLanguages(r.Languages),
		OtherShow: other, Published: r.Published,
		Size: r.Size, Runtime: runtimeOf(d, m.Episodes),
	})
}

// otherShow reports whether the indexer files the release under another
// show than the name does: the indexer's show, tvdbID, counts, unless the
// name gives the year the show first aired. Indexers confuse a show with
// its remake or original of the same name; the year tells them apart. When
// the name overrules the indexer, a note on the match says so.
func otherShow(show *match.Show, sr store.Series, tvdbID int64, claims release.Claims, m *match.Match) bool {
	if tvdbID == 0 || tvdbID == sr.ID {
		return false
	}
	if !show.NamesYear(claims) {
		return true
	}
	m.Notes = append(m.Notes, fmt.Sprintf("the indexer files it under another show, but its name gives %d, the year this show first aired", sr.Year))
	return false
}

// absoluteQueries are the text searches for an anime episode by its
// absolute number, such as "Harbor Precinct 059": groups that number a
// whole show write episode 59 as E059 or S01E059, and indexers file those
// under no TVDB position they could search by. They match the digits only
// as written, so the number is padded as such groups pad it, both ways
// past 999 episodes, and searched under the English and the German title.
// The aired-order search finds such releases in the first season of a
// show with fewer than 100 episodes, where both numberings agree.
func absoluteQueries(sr store.Series, show *match.Show, id int64, pos api.Position) []string {
	n, total := show.Absolute(id), show.Regular()
	if sr.Type != store.TypeAnime || n == 0 || pos.Season == 1 && n == pos.Number && total < 100 {
		return nil
	}
	// Groups pad to the show's length when they release, so episodes from
	// before it reached 100 or 1000 episodes keep one digit fewer.
	digits := max(2, len(strconv.Itoa(total)))
	widths := []int{digits - 1, digits}
	if digits == 2 {
		widths = []int{2}
	}
	var numbers []string
	for _, w := range widths {
		if s := fmt.Sprintf("%0*d", w, n); !slices.Contains(numbers, s) {
			numbers = append(numbers, s)
		}
	}
	var titles []string
	for _, t := range []string{cmp.Or(sr.NameEng, sr.Name), sr.NameDeu} {
		if t = searchTitle(t); t != "" && !slices.ContainsFunc(titles, func(o string) bool { return strings.EqualFold(o, t) }) {
			titles = append(titles, t)
		}
	}
	var out []string
	for _, t := range titles {
		for _, num := range numbers {
			out = append(out, t+" "+num)
		}
	}
	return out
}

// searchTitle turns a show's title into the words release names use:
// "Harbor Precinct (2013)" becomes "Harbor Precinct", "Harbor's: Night"
// "Harbors Night", "Hafenwächter" "Hafenwaechter" and "Crème" "Creme".
// A title without Latin letters, such as a Japanese one, gives nothing.
func searchTitle(title string) string {
	title = yearSuffix.ReplaceAllString(title, "")
	// Composed first, so an umlaut written as a letter and a combining
	// mark becomes "ae" too.
	title = umlauts.Replace(norm.NFC.String(title))
	var b strings.Builder
	latin := false
	for _, r := range norm.NFD.String(title) {
		switch {
		case unicode.Is(unicode.Mn, r), r == '\'', r == '’':
			// accents and apostrophes
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			latin = latin || unicode.IsLetter(r)
			b.WriteRune(r)
		default:
			b.WriteByte(' ')
		}
	}
	if !latin {
		return ""
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

var (
	yearSuffix = regexp.MustCompile(`\s*\((19|20)\d\d\)\s*$`)
	// umlauts are the letters that do not lose only an accent, written as
	// release names write them, as the importer names files.
	umlauts = strings.NewReplacer(
		"ä", "ae", "ö", "oe", "ü", "ue", "Ä", "Ae", "Ö", "Oe", "Ü", "Ue", "ß", "ss",
		"æ", "ae", "Æ", "Ae", "œ", "oe", "Œ", "Oe", "ø", "o", "Ø", "O", "ł", "l", "Ł", "L",
		"đ", "d", "Đ", "D", "ð", "d", "þ", "th", "Þ", "Th",
	)
)

// seasonEpisodes lists a season's episodes in the order's sequence.
func seasonEpisodes(d store.SeriesData, order string, season int) []int64 {
	var eps []store.Episode
	for _, e := range d.Episodes {
		if p, ok := e.Numbers[order]; ok && p.Season == season {
			eps = append(eps, e)
		}
	}
	slices.SortFunc(eps, func(a, b store.Episode) int {
		return cmp.Compare(a.Numbers[order].Number, b.Numbers[order].Number)
	})
	ids := make([]int64, len(eps))
	for i, e := range eps {
		ids[i] = e.ID
	}
	return ids
}

// missing keeps the episodes that have aired by now and have no file.
func missing(d store.SeriesData, episodes []int64, files map[int64]string, now time.Time) []int64 {
	aired := map[int64]bool{}
	today := now.Format(time.DateOnly)
	for _, e := range d.Episodes {
		aired[e.ID] = e.Aired != "" && e.Aired <= today
	}
	return slices.DeleteFunc(slices.Clone(episodes), func(id int64) bool {
		_, has := files[id]
		return has || !aired[id]
	})
}

// report passes a copy on, so the next answer cannot change what was sent.
func (s *Service) report(p api.SearchProgress) {
	if s.OnProgress != nil {
		p.Indexers = slices.Clone(p.Indexers)
		s.OnProgress(p)
	}
}

// hit is one search result with its verdict.
type hit struct {
	indexer  string
	result   prowlarr.Result
	eval     selection.Evaluation
	episodes []int64
	owner    int64 // for a season search: the episode a download belongs to
}

func (h hit) candidate() store.Candidate {
	c := store.Candidate{
		Indexer: h.indexer, Title: h.result.Title, Link: h.result.Link, Posting: h.result.Posting, Page: h.result.Page, Size: h.result.Size, Grabs: h.result.Grabs,
		Verdict: h.eval.Verdict, Tier: h.eval.Tier, Reasons: h.eval.Reasons, Decisive: h.eval.Decisive,
		Episodes: h.episodes,
		Reported: &store.IndexerReport{Languages: slices.Clone(h.result.Languages), TVDBID: h.result.TVDBID},
	}
	if !h.result.Published.IsZero() {
		p := h.result.Published
		c.Published = &p
	}
	for _, l := range h.eval.Audio {
		c.Audio = append(c.Audio, string(l))
	}
	if c.Episodes == nil {
		c.Episodes = []int64{}
	}
	return c
}

// outcome sums up a search: found when a release is accepted, fallback
// when only last resorts are left, failed when every indexer failed
// without returning anything. An indexer that failed on a later page still
// delivered the results before it. The summary notes the indexers that
// failed and those that answered only some queries, by name in order, so
// judging a stored search again, whose indexers the store lists by name,
// gives the same summary.
func outcome(summary string, evals []selection.Evaluation, indexers []store.SearchIndexer) (api.SearchOutcome, string) {
	var failed, partial []string
	for _, ix := range indexers {
		switch {
		case ix.Error != "":
			failed = append(failed, ix.Name)
		case ix.Partial != nil && *ix.Partial:
			partial = append(partial, ix.Name)
		}
	}
	slices.Sort(failed)
	slices.Sort(partial)
	if len(failed) == len(indexers) && len(evals) == 0 {
		if len(indexers) == 0 {
			return api.SearchFailed, "Search failed: Prowlarr has no enabled Usenet indexers"
		}
		return api.SearchFailed, "Search failed: " + indexers[0].Error
	}
	if len(failed) > 0 {
		summary += fmt.Sprintf(" (%s failed)", strings.Join(failed, ", "))
	}
	if len(partial) > 0 {
		summary += fmt.Sprintf(" (%s answered only some searches)", strings.Join(partial, ", "))
	}
	result := api.SearchNothing
	for _, e := range evals {
		switch {
		case e.Verdict == api.Accept:
			return api.SearchFound, summary
		case e.Verdict == api.Fallback:
			result = api.SearchFallback
		}
	}
	return result, summary
}

// runtimeOf is how long the episodes run, each by its own runtime or the
// show's average; 0 when TVDB knows neither.
func runtimeOf(d store.SeriesData, episodes []int64) time.Duration {
	var total time.Duration
	for _, id := range episodes {
		i := slices.IndexFunc(d.Episodes, func(e store.Episode) bool { return e.ID == id })
		minutes := d.Series.AverageRuntime
		if i >= 0 && d.Episodes[i].Runtime > 0 {
			minutes = d.Episodes[i].Runtime
		}
		if minutes <= 0 {
			return 0
		}
		total += time.Duration(minutes) * time.Minute
	}
	return total
}
