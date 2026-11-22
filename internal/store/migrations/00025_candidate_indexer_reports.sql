-- +goose Up
-- Keep what the indexer reported about each release beyond its name, so a
-- stored search can be judged again as it was judged: the audio languages,
-- a JSON array as the indexer writes them, and the TVDB ID of the show it
-- files the release under, 0 when it names none. Both are NULL for
-- candidates from before, which cannot be judged again faithfully.
ALTER TABLE candidates ADD COLUMN indexer_languages TEXT;
ALTER TABLE candidates ADD COLUMN indexer_tvdb_id INTEGER;
ALTER TABLE season_candidates ADD COLUMN indexer_languages TEXT;
ALTER TABLE season_candidates ADD COLUMN indexer_tvdb_id INTEGER;

-- Whether an indexer answered only some of an episode search's queries,
-- which the search's summary notes: 1 or 0, NULL for searches from before.
-- A season search asks one query per season and counts any failure as the
-- indexer's, so its indexers, kept as JSON, need no such mark.
ALTER TABLE search_indexers ADD COLUMN partial INTEGER;

-- +goose Down
ALTER TABLE search_indexers DROP COLUMN partial;
ALTER TABLE season_candidates DROP COLUMN indexer_tvdb_id;
ALTER TABLE season_candidates DROP COLUMN indexer_languages;
ALTER TABLE candidates DROP COLUMN indexer_tvdb_id;
ALTER TABLE candidates DROP COLUMN indexer_languages;
