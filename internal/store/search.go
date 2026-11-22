package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sudokvn/garner/internal/api"
	"github.com/sudokvn/garner/internal/store/query"
)

func (s *Store) SeriesOfEpisode(ctx context.Context, episodeID int64) (int64, error) {
	id, err := s.q.SeriesOfEpisode(ctx, episodeID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("episode %d: %w", episodeID, ErrNotFound)
	}
	return id, err
}

// SaveSearch replaces the episode's previous search. Candidates are saved
// in the order given; their IDs are assigned here.
func (s *Store) SaveSearch(ctx context.Context, sr Search) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := s.q.WithTx(tx)
	if err := q.DeleteSearch(ctx, sr.EpisodeID); err != nil {
		return err
	}
	var retry sql.NullInt64
	if sr.NextRetry != nil {
		retry = sql.NullInt64{Int64: sr.NextRetry.Unix(), Valid: true}
	}
	id, err := q.InsertSearch(ctx, query.InsertSearchParams{
		EpisodeID: sr.EpisodeID, SearchedAt: sr.Searched.Unix(), Outcome: string(sr.Outcome), Summary: sr.Summary, NextRetryAt: retry,
	})
	if err != nil {
		return fmt.Errorf("save search: %w", err)
	}
	for _, ix := range sr.Indexers {
		var partial sql.NullInt64
		if ix.Partial != nil {
			partial.Valid = true
			if *ix.Partial {
				partial.Int64 = 1
			}
		}
		if err := q.InsertSearchIndexer(ctx, query.InsertSearchIndexerParams{
			SearchID: id, Indexer: ix.Name, Results: int64(ix.Results), Error: nullString(ix.Error), Partial: partial,
		}); err != nil {
			return err
		}
	}
	for i, c := range sr.Candidates {
		reasons, _ := json.Marshal(c.Reasons)
		episodes, _ := json.Marshal(c.Episodes)
		var published sql.NullInt64
		if c.Published != nil {
			published = sql.NullInt64{Int64: c.Published.Unix(), Valid: true}
		}
		languages, tvdbID := reportColumns(c.Reported)
		if err := q.InsertCandidate(ctx, query.InsertCandidateParams{
			SearchID: id, Rank: int64(i + 1), Indexer: c.Indexer, Title: c.Title, Link: c.Link, Posting: c.Posting, Page: c.Page, Size: c.Size,
			Published: published, Grabs: int64(c.Grabs), Verdict: string(c.Verdict), Tier: c.Tier,
			Audio: strings.Join(c.Audio, ","), Reasons: string(reasons), Episodes: string(episodes),
			Decisive: int64(c.Decisive), IndexerLanguages: languages, IndexerTvdbID: tvdbID,
		}); err != nil {
			return fmt.Errorf("save candidate %q: %w", c.Title, err)
		}
	}
	return tx.Commit()
}

// LoadSearch returns the latest search for an episode, without download
// links, with the latest download of each candidate's posting.
func (s *Store) LoadSearch(ctx context.Context, episodeID int64) (Search, error) {
	row, err := s.q.GetSearch(ctx, episodeID)
	if errors.Is(err, sql.ErrNoRows) {
		return Search{}, fmt.Errorf("search for episode %d: %w", episodeID, ErrNotFound)
	}
	if err != nil {
		return Search{}, err
	}
	sr := Search{
		EpisodeID: row.EpisodeID, Searched: time.Unix(row.SearchedAt, 0), Outcome: api.SearchOutcome(row.Outcome), Summary: row.Summary,
		Indexers: []SearchIndexer{}, Candidates: []Candidate{},
	}
	if row.NextRetryAt.Valid {
		t := time.Unix(row.NextRetryAt.Int64, 0)
		sr.NextRetry = &t
	}
	indexers, err := s.q.ListSearchIndexers(ctx, row.ID)
	if err != nil {
		return sr, err
	}
	for _, ix := range indexers {
		indexer := SearchIndexer{Name: ix.Indexer, Results: int(ix.Results), Error: ix.Error.String}
		if ix.Partial.Valid {
			partial := ix.Partial.Int64 != 0
			indexer.Partial = &partial
		}
		sr.Indexers = append(sr.Indexers, indexer)
	}
	candidates, err := s.q.ListCandidates(ctx, row.ID)
	if err != nil {
		return sr, err
	}
	for _, c := range candidates {
		cand := Candidate{
			ID: c.ID, Rank: int(c.Rank), Indexer: c.Indexer, Title: c.Title, Posting: c.Posting, Page: c.Page, Size: c.Size, Grabs: int(c.Grabs),
			Verdict: api.Verdict(c.Verdict), Tier: c.Tier, Audio: []string{}, Reasons: []string{}, Episodes: []int64{},
			Decisive: int(c.Decisive),
		}
		if c.Published.Valid {
			t := time.Unix(c.Published.Int64, 0)
			cand.Published = &t
		}
		if c.Audio != "" {
			cand.Audio = strings.Split(c.Audio, ",")
		}
		if err := json.Unmarshal([]byte(c.Reasons), &cand.Reasons); err != nil {
			return sr, fmt.Errorf("candidate %d reasons: %w", c.ID, err)
		}
		if err := json.Unmarshal([]byte(c.Episodes), &cand.Episodes); err != nil {
			return sr, fmt.Errorf("candidate %d episodes: %w", c.ID, err)
		}
		if cand.Reported, err = reportFromColumns(c.IndexerLanguages, c.IndexerTvdbID); err != nil {
			return sr, fmt.Errorf("candidate %d: %w", c.ID, err)
		}
		sr.Candidates = append(sr.Candidates, cand)
	}
	downloads, err := s.EpisodeDownloads(ctx, episodeID)
	if err != nil {
		return sr, err
	}
	withAttempts(sr.Candidates, downloads)
	return sr, nil
}

// reportColumns stores what the indexer reported about a candidate; NULL
// when it is unknown.
func reportColumns(r *IndexerReport) (sql.NullString, sql.NullInt64) {
	if r == nil {
		return sql.NullString{}, sql.NullInt64{}
	}
	languages := r.Languages
	if languages == nil {
		languages = []string{}
	}
	b, _ := json.Marshal(languages)
	return sql.NullString{String: string(b), Valid: true}, sql.NullInt64{Int64: r.TVDBID, Valid: true}
}

// reportFromColumns reads what the indexer reported about a candidate:
// nil for one stored before garner kept it.
func reportFromColumns(languages sql.NullString, tvdbID sql.NullInt64) (*IndexerReport, error) {
	if !languages.Valid || !tvdbID.Valid {
		return nil, nil
	}
	r := &IndexerReport{TVDBID: tvdbID.Int64, Languages: []string{}}
	if err := json.Unmarshal([]byte(languages.String), &r.Languages); err != nil {
		return nil, fmt.Errorf("indexer languages: %w", err)
	}
	return r, nil
}

// withAttempts gives each candidate the latest of the downloads, newest
// first, of its posting.
func withAttempts(candidates []Candidate, downloads []Download) {
	for i := range candidates {
		for _, d := range downloads {
			if candidates[i].SamePosting(d) {
				candidates[i].Attempt = &d
				break
			}
		}
	}
}
