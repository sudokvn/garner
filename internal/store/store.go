// Package store owns garner's database: opening it, applying migrations,
// backups and the queries the server needs.
package store

//go:generate sqlc generate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
	turso "turso.tech/database/tursogo"

	"github.com/trucke/garner/internal/store/query"
)

//go:embed migrations/*.sql
var migrations embed.FS

// FileName is the database file inside the data directory.
const FileName = "garner.db"

var (
	ErrNotFound     = errors.New("not found")
	ErrExists       = errors.New("already exists")
	ErrUnauthorized = errors.New("unauthorized")
)

// Store is an open database. garner uses a single connection: its load is
// small, and every Turso connection opens its own database instance.
type Store struct {
	db   *sql.DB
	q    *query.Queries
	path string
	log  *slog.Logger
}

// Open opens the database in dataDir, creating it if needed, and applies
// pending migrations. An existing database is backed up before migrating.
// Turso locks the file, so a second process cannot open it.
func Open(ctx context.Context, dataDir string, log *slog.Logger) (*Store, error) {
	db, path, err := connect(ctx, dataDir, log)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, q: query.New(db), path: path, log: log}
	if err := s.migrate(ctx, dataDir); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// connect opens the database in dataDir, creating it if needed, without
// migrating it.
func connect(ctx context.Context, dataDir string, log *slog.Logger) (*sql.DB, string, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, "", fmt.Errorf("create data directory: %w", err)
	}
	// Turso unpacks its engine library on first use. It goes into the data
	// directory, a folder per Turso version, and once the database is open
	// the folders of other versions go. A cache directory set from outside
	// is left alone.
	libs, version := "", tursoVersion()
	if os.Getenv("TURSO_GO_CACHE_DIR") == "" {
		libs = filepath.Join(dataDir, "lib")
		os.Setenv("TURSO_GO_CACHE_DIR", filepath.Join(libs, version))
	}
	path := filepath.Join(dataDir, FileName)
	inner, err := turso.NewConnector(path)
	if err != nil {
		return nil, "", err
	}
	db := sql.OpenDB(pragmaConnector{inner})
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		if strings.Contains(err.Error(), "locked by another process") {
			return nil, "", fmt.Errorf("database %s is in use by another garner process", path)
		}
		return nil, "", fmt.Errorf("open database: %w", err)
	}
	if libs != "" && version != "" {
		removeOtherLibraries(libs, version, log)
	}
	return db, path, nil
}

// tursoVersion is the version of the Turso library package this program
// was built with; empty when the build does not say.
func tursoVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, m := range info.Deps {
		if m.Path != "github.com/tursodatabase/turso-go-platform-libs" {
			continue
		}
		if m.Replace != nil {
			m = m.Replace
		}
		return m.Version
	}
	return ""
}

// removeOtherLibraries removes everything in libs but the folder of the
// Turso version in use: libraries of earlier versions, unpacked before an
// upgrade. Only the process holding the database lock calls it.
func removeOtherLibraries(libs, keep string, log *slog.Logger) {
	entries, err := os.ReadDir(libs)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.Name() == keep {
			continue
		}
		if err := os.RemoveAll(filepath.Join(libs, e.Name())); err != nil {
			log.Warn("could not remove an earlier database library", "folder", e.Name(), "err", err)
			continue
		}
		log.Info("removed an earlier database library", "folder", e.Name())
	}
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Path() string { return s.path }

// pragmaConnector enables foreign keys on every new connection.
type pragmaConnector struct{ inner driver.Connector }

func (c pragmaConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	exec, ok := conn.(driver.ExecerContext)
	if !ok {
		conn.Close()
		return nil, errors.New("database driver cannot execute statements")
	}
	if _, err := exec.ExecContext(ctx, "PRAGMA foreign_keys = ON", nil); err != nil {
		conn.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	return conn, nil
}

func (c pragmaConnector) Driver() driver.Driver { return c.inner.Driver() }

// migrator returns goose's view of garner's migrations on db.
func migrator(db *sql.DB) (*goose.Provider, error) {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return nil, err
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, db, sub, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return nil, fmt.Errorf("load migrations: %w", err)
	}
	return p, nil
}

func (s *Store) migrate(ctx context.Context, dataDir string) error {
	p, err := migrator(s.db)
	if err != nil {
		return err
	}
	current, err := p.GetDBVersion(ctx)
	if err != nil {
		return err
	}
	// goose sees no pending migrations in a database a newer garner
	// migrated, but this garner does not know its tables.
	sources := p.ListSources()
	if newest := sources[len(sources)-1].Version; current > newest {
		return fmt.Errorf("the database is at version %d, but this garner knows versions up to %d: a newer garner migrated it. "+
			"Run that newer garner, or restore the backup it made before migrating, pre-migration-v*.db in %s",
			current, newest, filepath.Join(dataDir, "backups"))
	}
	pending, err := p.HasPending(ctx)
	if err != nil || !pending {
		return err
	}
	var before *Backup
	if current > 0 {
		name := fmt.Sprintf("pre-migration-v%d-%s.db", current, time.Now().UTC().Format("20060102-150405"))
		b, err := s.snapshot(ctx, filepath.Join(dataDir, "backups", name))
		if err != nil {
			return fmt.Errorf("back up before migrating: %w", err)
		}
		before = &b
		s.log.Info("backed up database before migrating", "path", b.Path)
	}
	results, err := p.Up(ctx)
	if err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	for _, r := range results {
		s.log.Info("applied migration", "version", r.Source.Version, "duration", r.Duration.Round(time.Millisecond))
	}
	if before != nil {
		s.record(ctx, *before, "pre-migration")
	}
	return nil
}

type Status struct {
	Path       string
	Size       int64 // database file plus write-ahead log
	Integrity  string
	Tokens     int64
	LastBackup *Backup
}

// Status reports the database's size, integrity and last backup.
func (s *Store) Status(ctx context.Context) (Status, error) {
	st := Status{Path: s.path}
	for _, p := range []string{s.path, s.path + "-wal"} {
		if info, err := os.Stat(p); err == nil {
			st.Size += info.Size()
		}
	}
	if err := s.db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&st.Integrity); err != nil {
		return st, fmt.Errorf("integrity check: %w", err)
	}
	n, err := s.q.CountTokens(ctx)
	if err != nil {
		return st, err
	}
	st.Tokens = n
	last, err := s.q.LastBackup(ctx)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return st, err
	default:
		st.LastBackup = &Backup{Path: last.Path, Size: last.Size, Reason: last.Reason, Created: time.Unix(last.CreatedAt, 0)}
	}
	return st, nil
}
