// Package store owns the SQLite database: connection setup, migrations and the single-writer transaction helper.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure Go driver, keeps the binary static
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Store is one database file. Writes go through one connection (BEGIN IMMEDIATE); reads use their own pool.
type Store struct {
	db *sql.DB // single writer
	ro *sql.DB // concurrent readers
}

func dsn(path, extra string) string {
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "synchronous(NORMAL)")
	if extra != "" {
		k, v, _ := strings.Cut(extra, "=")
		q.Add(k, v)
	}
	return "file:" + path + "?" + q.Encode()
}

// Open opens (and creates) the database at path.
func Open(path string) (*Store, error) {
	w, err := sql.Open("sqlite", dsn(path, "_txlock=immediate"))
	if err != nil {
		return nil, fmt.Errorf("open writer: %w", err)
	}
	w.SetMaxOpenConns(1)
	if err := w.Ping(); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	r, err := sql.Open("sqlite", dsn(path, "_pragma=query_only(1)"))
	if err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("open reader: %w", err)
	}
	r.SetMaxOpenConns(8)
	return &Store{db: w, ro: r}, nil
}

func (s *Store) Close() error { return errors.Join(s.ro.Close(), s.db.Close()) }

// Read returns the read pool. Never write through it.
func (s *Store) Read() *sql.DB { return s.ro }

// Tx runs fn in a BEGIN IMMEDIATE transaction on the single writer; it commits unless fn fails.
func (s *Store) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit; releases the writer if fn panics
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// PendingMigrations counts embedded migrations the database has not applied. It only reads: it creates nothing.
func (s *Store) PendingMigrations(ctx context.Context) (int, error) {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return 0, err
	}
	var applied int
	var exists int
	if err := s.ro.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'`).Scan(&exists); err != nil {
		return 0, fmt.Errorf("check migrations table: %w", err)
	}
	if exists == 1 {
		if err := s.ro.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&applied); err != nil {
			return 0, fmt.Errorf("count applied migrations: %w", err)
		}
	}
	return len(entries) - applied, nil
}

// Migrate applies every embedded migration that has not run yet. Safe to call repeatedly.
func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL) STRICT`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		version, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %s: name must start with a number: %w", name, err)
		}
		body, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		err = s.Tx(ctx, func(tx *sql.Tx) error {
			var done int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE version = ?`, version).Scan(&done); err != nil {
				return err
			}
			if done > 0 {
				return nil
			}
			if _, err := tx.ExecContext(ctx, string(body)); err != nil {
				return fmt.Errorf("apply: %w", err)
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,?)`, version, name, time.Now().UTC().Format(time.RFC3339))
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}
