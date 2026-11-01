-- +goose Up
-- garner keeps a failure's concrete cause, such as "27 articles missing",
-- from the moment the download fails, instead of working it out from the
-- log on every read. Downloads from before have none, and clients show
-- SABnzbd's own failure message instead.
ALTER TABLE downloads ADD COLUMN failure_summary TEXT NOT NULL DEFAULT '';

-- Versions before 23 October 2026 stored SABnzbd's processing log as the
-- HTML SABnzbd sends. There an escaped query separator, &amp;, could hide a
-- credential in a quoted download link from the redaction, so later
-- versions decoded and redacted these logs on every read. Instead, the logs
-- of all downloads created before the first public release, on 26 October
-- 2026, go now: only installations from before it can hold such logs, and
-- this does not depend on how each early version wrote them. SABnzbd's own
-- failure message stays. Later logs are stored decoded and redacted.
-- 1792972800 is 26 October 2026, 00:00 UTC.
UPDATE downloads SET stages = '[]' WHERE created_at < 1792972800;

-- +goose Down
-- The emptied logs do not come back.
ALTER TABLE downloads DROP COLUMN failure_summary;
