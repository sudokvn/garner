package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/trucke/garner/internal/store/query"
)

// Backup is a database snapshot.
type Backup struct {
	Path    string
	Size    int64
	Reason  string
	Created time.Time
}

// Backup writes a consistent snapshot to path, or to the data directory's
// backups folder when path is empty.
func (s *Store) Backup(ctx context.Context, path string) (Backup, error) {
	if path == "" {
		name := "garner-" + time.Now().UTC().Format("20060102-150405") + ".db"
		path = filepath.Join(filepath.Dir(s.path), "backups", name)
	}
	b, err := s.snapshot(ctx, path)
	if err != nil {
		return Backup{}, err
	}
	b.Reason = "manual"
	s.record(ctx, b, b.Reason)
	return b, nil
}

func (s *Store) snapshot(ctx context.Context, path string) (Backup, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return Backup{}, err
	}
	if _, err := os.Stat(path); err == nil {
		return Backup{}, fmt.Errorf("%s already exists", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Backup{}, err
	}
	// VACUUM INTO takes no bound parameters; quote the path as a literal.
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO '"+strings.ReplaceAll(path, "'", "''")+"'"); err != nil {
		return Backup{}, fmt.Errorf("write snapshot: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return Backup{}, err
	}
	// Turso leaves an empty write-ahead log next to the snapshot; the
	// snapshot file is complete on its own.
	if info, err := os.Stat(path + "-wal"); err == nil && info.Size() == 0 {
		os.Remove(path + "-wal")
	}
	info, err := os.Stat(path)
	if err != nil {
		return Backup{}, err
	}
	return Backup{Path: path, Size: info.Size(), Created: time.Now()}, nil
}

// record notes a backup for status reports. Failing to record does not undo
// a backup that was written, so it is only logged.
func (s *Store) record(ctx context.Context, b Backup, reason string) {
	err := s.q.RecordBackup(ctx, query.RecordBackupParams{Path: b.Path, Size: b.Size, Reason: reason, CreatedAt: b.Created.Unix()})
	if err != nil {
		s.log.Warn("backup written but not recorded", "path", b.Path, "err", err)
	}
}
