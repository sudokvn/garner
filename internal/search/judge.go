package search

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/sudokvn/garner/internal/api"
	"github.com/sudokvn/garner/internal/match"
	"github.com/sudokvn/garner/internal/profile"
	"github.com/sudokvn/garner/internal/prowlarr"
	"github.com/sudokvn/garner/internal/release"
	"github.com/sudokvn/garner/internal/selection"
	"github.com/sudokvn/garner/internal/store"
)

// ErrTooOld means a stored search is from before garner kept what the
// indexers reported about its releases and which indexers answered only
// some queries, without which judging them again could differ from the
// search for reasons other than the profile.
var ErrTooOld = errors.New("this search is from before garner kept what the indexers reported about its releases; search again to judge them under another profile")

// Judge judges the releases of an episode's latest stored search again,
// under the show's own profile, as current applies it, and under p, both
// as a search would judge them now. rules returns a show's reviewed
// streaming-service order rules and may be nil. It searches, stores and
// downloads nothing. It returns store.ErrNotFound when the episode has no
// stored search, and ErrTooOld for a search from before garner kept what
// the indexers reported.
func Judge(ctx context.Context, st *store.Store, episodeID int64, rules func(int64) map[release.Service]string,
	current func(store.Series) selection.Profile, p profile.Profile, now time.Time) (api.Judgement, error) {
	sr, err := st.LoadSearch(ctx, episodeID)
	if err != nil {
		return api.Judgement{}, err
	}
	if slices.ContainsFunc(sr.Candidates, func(c store.Candidate) bool { return c.Reported == nil }) ||
		slices.ContainsFunc(sr.Indexers, func(ix store.SearchIndexer) bool { return ix.Partial == nil }) {
		return api.Judgement{}, ErrTooOld
	}
	seriesID, err := st.SeriesOfEpisode(ctx, episodeID)
	if err != nil {
		return api.Judgement{}, err
	}
	d, err := st.LoadSeries(ctx, seriesID)
	if err != nil {
		return api.Judgement{}, err
	}
	pos, err := position(d, episodeID)
	if err != nil {
		return api.Judgement{}, err
	}
	files, err := st.SeriesFileTiers(ctx, seriesID)
	if err != nil {
		return api.Judgement{}, err
	}
	var orders map[release.Service]string
	if rules != nil {
		orders = rules(seriesID)
	}
	show := match.NewShow(d, match.Options{Anime: d.Series.Type == store.TypeAnime, ServiceOrders: orders})
	results := make([]prowlarr.Result, len(sr.Candidates))
	for i, c := range sr.Candidates {
		results[i] = prowlarr.Result{Title: c.Title, Size: c.Size, Languages: c.Reported.Languages, TVDBID: c.Reported.TVDBID}
		if c.Published != nil {
			results[i].Published = *c.Published
		}
	}

	// judged judges every release under a profile and ranks them.
	judged := func(prof selection.Profile) (api.JudgedSearch, []api.Judged) {
		judge := episodeContext(d, episodeID, pos, files, prof, show, now)
		evals := make([]selection.Evaluation, len(results))
		for i, r := range results {
			claims := release.Parse(r.Title)
			evals[i] = evaluate(judge, d, r, claims, show.Match(claims, r.Published))
		}
		out := make([]api.Judged, len(evals))
		for k, i := range selection.Rank(evals) {
			e := evals[i]
			out[i] = api.Judged{Verdict: e.Verdict, Tier: e.Tier, Rank: k + 1, Audio: []string{}, Reasons: e.Reasons, Decisive: e.Decisive}
			for _, l := range e.Audio {
				out[i].Audio = append(out[i].Audio, string(l))
			}
		}
		js := api.JudgedSearch{Profile: prof.Name, Rules: prof.Describe()}
		js.Outcome, js.Summary = outcome(selection.Summary(prof, judge.Have, evals), evals, sr.Indexers)
		return js, out
	}
	j := api.Judgement{EpisodeID: episodeID, Searched: sr.Searched, Candidates: []api.JudgedCandidate{}}
	var before, after []api.Judged
	j.Before, before = judged(current(d.Series))
	j.After, after = judged(selection.FromProfile(p, selection.OriginalLanguage(d.Series.OriginalLanguage)))
	for i, c := range sr.Candidates {
		j.Candidates = append(j.Candidates, api.JudgedCandidate{
			ID: c.ID, Indexer: c.Indexer, Title: c.Title, Size: c.Size, Before: before[i], After: after[i], Attempt: c.Attempt,
		})
	}
	slices.SortFunc(j.Candidates, func(a, b api.JudgedCandidate) int { return a.After.Rank - b.After.Rank })
	return j, nil
}
