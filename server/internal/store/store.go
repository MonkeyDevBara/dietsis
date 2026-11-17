// Package store owns the SQLite database: connection setup, migrations and (from M4) repositories.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" driver
)

// DB holds two pools over the same WAL database file.
//
//   - Write has a single connection and every transaction starts with BEGIN IMMEDIATE, so writers are
//     serialized in-process and never hit SQLITE_BUSY from a read→write lock upgrade.
//   - Read is query_only: readers run concurrently with the writer (WAL) and cannot modify data even
//     through a bug.
type DB struct {
	Read  *sql.DB
	Write *sql.DB
	path  string
}

// pragmas applied to every new connection (docs/DATABASE.md → Konfigurasi koneksi).
var basePragmas = []string{
	"busy_timeout(5000)",
	"journal_mode(WAL)",
	"synchronous(NORMAL)",
	"foreign_keys(ON)",
	"temp_store(MEMORY)",
	"cache_size(-16000)",
}

func dsn(path string, extra ...string) string {
	q := url.Values{}
	for _, p := range append(append([]string{}, basePragmas...), extra...) {
		q.Add("_pragma", p)
	}
	return (&url.URL{Scheme: "file", Opaque: filepath.ToSlash(path), RawQuery: q.Encode()}).String()
}

// Open creates the data directory (0700) if needed and opens both pools. It does not migrate.
func Open(ctx context.Context, path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	w, err := sql.Open("sqlite", dsn(path)+"&_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("open write pool: %w", err)
	}
	w.SetMaxOpenConns(1)
	w.SetMaxIdleConns(1)

	r, err := sql.Open("sqlite", dsn(path, "query_only(ON)"))
	if err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("open read pool: %w", err)
	}
	r.SetMaxOpenConns(4)
	r.SetMaxIdleConns(4)

	db := &DB{Read: r, Write: w, path: path}
	// Ping the writer first: it creates the file and switches it to WAL before readers connect.
	if err := db.Ping(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	// Owner-only permissions on the database and its WAL sidecars (health data).
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Chmod(path+suffix, 0o600); err != nil && !os.IsNotExist(err) {
			_ = db.Close()
			return nil, fmt.Errorf("chmod database file: %w", err)
		}
	}
	return db, nil
}

// Ping verifies both pools can reach the database.
func (d *DB) Ping(ctx context.Context) error {
	if err := d.Write.PingContext(ctx); err != nil {
		return fmt.Errorf("ping write pool: %w", err)
	}
	if err := d.Read.PingContext(ctx); err != nil {
		return fmt.Errorf("ping read pool: %w", err)
	}
	return nil
}

func (d *DB) Path() string { return d.path }

func (d *DB) Close() error {
	rerr := d.Read.Close()
	werr := d.Write.Close()
	if werr != nil {
		return werr
	}
	return rerr
}

// WithTx runs fn in a write transaction (BEGIN IMMEDIATE) and commits if fn returns nil.
func (d *DB) WithTx(ctx context.Context, fn func(*sql.Tx) error) (err error) {
	tx, err := d.Write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
