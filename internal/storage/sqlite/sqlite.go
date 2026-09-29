package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Store struct {
	path  string
	write *sql.DB
	read  *sql.DB

	mu     sync.Mutex
	series map[string]int64
}

func Open(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("cannot create database directory %s: %w", filepath.Dir(path), err)
	}
	common := "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	w, err := sql.Open("sqlite", path+"?"+common+"&_pragma=journal_mode(WAL)&_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	w.SetMaxOpenConns(1)
	w.SetMaxIdleConns(1)
	w.SetConnMaxLifetime(0)

	s := &Store{path: path, write: w, series: map[string]int64{}}
	if err := s.init(ctx); err != nil {
		_ = w.Close()
		return nil, diagnose(path, err)
	}

	r, err := sql.Open("sqlite", path+"?"+common+"&_pragma=query_only(1)")
	if err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("open read pool for %s: %w", path, err)
	}
	r.SetMaxOpenConns(4)
	r.SetMaxIdleConns(2)
	s.read = r
	return s, nil
}

func (s *Store) Path() string { return s.path }

func (s *Store) Close() error {
	var errs []error
	if s.read != nil {
		errs = append(errs, s.read.Close())
	}
	if s.write != nil {
		_, _ = s.write.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
		errs = append(errs, s.write.Close())
	}
	return errors.Join(errs...)
}

func (s *Store) init(ctx context.Context) error {
	if err := s.write.PingContext(ctx); err != nil {
		return err
	}
	var tables int
	if err := s.write.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table'").Scan(&tables); err != nil {
		return err
	}
	if tables == 0 {
		if _, err := s.write.ExecContext(ctx, "PRAGMA auto_vacuum = INCREMENTAL"); err != nil {
			return err
		}
	}
	return s.migrate(ctx)
}

type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		name := e.Name()
		num, _, ok := strings.Cut(name, "_")
		v, err := strconv.Atoi(num)
		if !ok || err != nil || !strings.HasSuffix(name, ".sql") {
			return nil, fmt.Errorf("invalid migration file name %q (want NNNN_name.sql)", name)
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: v, name: strings.TrimSuffix(name, ".sql"), sql: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

func (s *Store) migrate(ctx context.Context) error {
	migs, err := loadMigrations()
	if err != nil {
		return err
	}
	if _, err := s.write.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT    NOT NULL,
		applied_at INTEGER NOT NULL)`); err != nil {
		return err
	}
	var current int
	if err := s.write.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&current); err != nil {
		return err
	}
	if latest := migs[len(migs)-1].version; current > latest {
		return fmt.Errorf("database schema version %d is newer than this build supports (%d); upgrade Pulse or point storage.path elsewhere", current, latest)
	}
	for _, m := range migs {
		if m.version <= current {
			continue
		}
		tx, err := s.write.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %s failed: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version, name, applied_at) VALUES (?, ?, ?)",
			m.version, m.name, time.Now().UnixMilli()); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migration %s commit: %w", m.name, err)
		}
	}
	return nil
}

func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var v int
	err := s.read.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&v)
	return v, err
}

func diagnose(path string, err error) error {
	msg := err.Error()
	hint := ""
	switch {
	case strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(msg, "database is locked"):
		hint = "another process holds a lock on the database; close other Pulse instances or set storage.path to a different file"
	case strings.Contains(msg, "SQLITE_CANTOPEN") || strings.Contains(msg, "unable to open"):
		hint = "check that the directory exists and is writable"
	case strings.Contains(msg, "SQLITE_NOTADB") || strings.Contains(msg, "not a database"):
		hint = "the file is not a SQLite database; move it away or set storage.path"
	case strings.Contains(msg, "SQLITE_CORRUPT") || strings.Contains(msg, "malformed"):
		hint = "the database looks corrupted; move it away to start fresh"
	case strings.Contains(msg, "readonly") || strings.Contains(msg, "permission denied"):
		hint = "the database file or directory is read-only"
	}
	if hint != "" {
		return fmt.Errorf("storage %s: %w (hint: %s)", path, err, hint)
	}
	return fmt.Errorf("storage %s: %w", path, err)
}
