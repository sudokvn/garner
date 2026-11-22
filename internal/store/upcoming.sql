-- The episodes that air from first to last, both dates included, with
-- their names.
-- name: ListEpisodesAiring :many
SELECT id, name, name_eng FROM episodes WHERE aired >= sqlc.arg(first) AND aired <= sqlc.arg(last);
