// Package backup creates, verifies, prunes and restores SQLite snapshots (docs/DEPLOYMENT.md).
//
// A backup is a consistent snapshot made with VACUUM INTO while the server keeps running. It is
// integrity-checked before it is kept and stored gzip-compressed with owner-only permissions.
// A restore verifies the backup first and swaps the database file atomically, keeping the old one.
package backup

import (
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MonkeyDevBara/dietsis/server/internal/store"
)

const (
	filePrefix = "dietsis-"
	fileSuffix = ".db.gz"
	timeLayout = "20060102-150405"
)

// Create writes a verified, compressed snapshot of db into dir and returns its path.
func Create(ctx context.Context, db *store.DB, dir string, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}
	stamp := now.UTC().Format(timeLayout)
	raw := filepath.Join(dir, filePrefix+stamp+".db.tmp")
	final := filepath.Join(dir, filePrefix+stamp+fileSuffix)
	_ = os.Remove(raw)
	defer os.Remove(raw)

	// VACUUM INTO refuses to overwrite and runs outside a transaction on one connection.
	if _, err := db.Write.ExecContext(ctx, `VACUUM INTO ?`, raw); err != nil {
		return "", fmt.Errorf("snapshot database: %w", err)
	}
	if err := Verify(ctx, raw); err != nil {
		return "", fmt.Errorf("snapshot failed verification: %w", err)
	}
	if err := gzipFile(raw, final); err != nil {
		return "", err
	}
	return final, nil
}

// Prune keeps the newest `keep` backups in dir and deletes the rest. Names sort chronologically.
func Prune(dir string, keep int) ([]string, error) {
	names, err := List(dir)
	if err != nil {
		return nil, err
	}
	if len(names) <= keep {
		return nil, nil
	}
	var removed []string
	for _, n := range names[:len(names)-keep] {
		if err := os.Remove(filepath.Join(dir, n)); err != nil {
			return removed, err
		}
		removed = append(removed, n)
	}
	return removed, nil
}

// List returns backup file names in dir, oldest first. Only files this package would create.
func List(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.HasPrefix(n, filePrefix) && strings.HasSuffix(n, fileSuffix) {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names, nil
}

// Verify opens a database file read-only and checks that it is intact and is a Dietsis database.
func Verify(ctx context.Context, path string) error {
	dsn := (&url.URL{Scheme: "file", Opaque: filepath.ToSlash(path), RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	var verdict string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&verdict); err != nil {
		return fmt.Errorf("integrity_check: %w", err)
	}
	if verdict != "ok" {
		return fmt.Errorf("integrity_check: %s", verdict)
	}
	rows, err := db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("foreign_key_check: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("foreign_key_check: violations found")
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, table := range []string{"users", "schema_migrations", "food_logs"} {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name = ?`, table).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("not a Dietsis database: table %q is missing", table)
		}
	}
	return nil
}

// Restore replaces the database at dbPath with the contents of a backup file.
//
// The server must be stopped: restoring under a running server would leave it holding the old
// file. The backup is decompressed next to the target, verified, and only then renamed into place;
// the previous database is kept as <db>.pre-restore-<timestamp>. If anything fails, the current
// database is left untouched.
func Restore(ctx context.Context, backupPath, dbPath string, now time.Time) (previous string, err error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	tmp := dbPath + ".restore.tmp"
	_ = os.Remove(tmp)
	defer os.Remove(tmp)

	if err := gunzipFile(backupPath, tmp); err != nil {
		return "", fmt.Errorf("read backup: %w", err)
	}
	if err := Verify(ctx, tmp); err != nil {
		return "", fmt.Errorf("backup rejected: %w", err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return "", err
	}

	if _, statErr := os.Stat(dbPath); statErr == nil {
		previous = fmt.Sprintf("%s.pre-restore-%s", dbPath, now.UTC().Format(timeLayout))
		if err := os.Rename(dbPath, previous); err != nil {
			return "", fmt.Errorf("keep current database: %w", err)
		}
	}
	// Stale WAL/SHM files belong to the old database and would corrupt the restored one.
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = os.Remove(dbPath + suffix)
	}
	if err := os.Rename(tmp, dbPath); err != nil {
		if previous != "" {
			_ = os.Rename(previous, dbPath) // put the old database back
		}
		return "", fmt.Errorf("install restored database: %w", err)
	}
	return previous, nil
}

func gzipFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".partial"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()
	zw := gzip.NewWriter(out)
	if _, err = io.Copy(zw, in); err != nil {
		_ = out.Close()
		return err
	}
	if err = zw.Close(); err != nil {
		_ = out.Close()
		return err
	}
	if err = out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func gunzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	zr, err := gzip.NewReader(in)
	if err != nil {
		return err
	}
	defer zr.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	// A backup is a database file; refuse absurd sizes (decompression bomb) instead of filling the disk.
	const maxSize = 8 << 30
	n, err := io.Copy(out, io.LimitReader(zr, maxSize+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n > maxSize {
		return errors.New("backup is implausibly large")
	}
	return nil
}
