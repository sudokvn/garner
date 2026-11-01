-- name: RecordBackup :exec
INSERT INTO backups (path, size, reason, created_at) VALUES (?, ?, ?, ?);

-- name: LastBackup :one
SELECT path, size, reason, created_at FROM backups ORDER BY created_at DESC, id DESC LIMIT 1;
