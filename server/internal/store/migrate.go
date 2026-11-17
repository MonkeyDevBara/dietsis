package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"time"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

var migrationName = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.sql$`)

type migration struct {
	version  int
	name     string
	sql      string
	checksum string
}

// Migrate applies the embedded migrations. See docs/DATABASE.md → Migrasi.
func (d *DB) Migrate(ctx context.Context) error {
	sub, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		return err
	}
	return d.MigrateFS(ctx, sub)
}

// MigrateFS applies *.sql files named NNNN_name.sql from fsys in version order.
//
// Rules: forward-only; each migration runs in one transaction together with its schema_migrations
// row (failure rolls back completely); a released migration must never change (checksum compared on
// every start); a database newer than this binary is refused rather than guessed at.
func (d *DB) MigrateFS(ctx context.Context, fsys fs.FS) error {
	migs, err := loadMigrations(fsys)
	if err != nil {
		return err
	}

	if _, err := d.Write.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER NOT NULL PRIMARY KEY,
		name       TEXT    NOT NULL,
		checksum   TEXT    NOT NULL,
		applied_at INTEGER NOT NULL
	) STRICT`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := d.appliedMigrations(ctx)
	if err != nil {
		return err
	}

	known := make(map[int]migration, len(migs))
	for _, m := range migs {
		known[m.version] = m
	}
	for v, a := range applied {
		m, ok := known[v]
		if !ok {
			return fmt.Errorf("database has migration %04d_%s that this binary does not know: refusing to run an older binary on a newer database", v, a.name)
		}
		if m.checksum != a.checksum {
			return fmt.Errorf("migration %04d_%s was modified after being applied (checksum mismatch); write a new migration instead", v, m.name)
		}
	}

	for _, m := range migs {
		if _, done := applied[m.version]; done {
			continue
		}
		if err := d.apply(ctx, m); err != nil {
			return fmt.Errorf("apply migration %04d_%s: %w", m.version, m.name, err)
		}
	}

	return d.CheckForeignKeys(ctx)
}

func (d *DB) apply(ctx context.Context, m migration) error {
	return d.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, name, checksum, applied_at) VALUES (?, ?, ?, ?)`,
			m.version, m.name, m.checksum, time.Now().UnixMilli())
		return err
	})
}

type appliedMigration struct{ name, checksum string }

func (d *DB) appliedMigrations(ctx context.Context) (map[int]appliedMigration, error) {
	rows, err := d.Write.QueryContext(ctx, `SELECT version, name, checksum FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()
	out := map[int]appliedMigration{}
	for rows.Next() {
		var v int
		var a appliedMigration
		if err := rows.Scan(&v, &a.name, &a.checksum); err != nil {
			return nil, err
		}
		out[v] = a
	}
	return out, rows.Err()
}

func loadMigrations(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var migs []migration
	seen := map[int]string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := migrationName.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf("migration file %q does not match NNNN_name.sql", e.Name())
		}
		version, _ := strconv.Atoi(m[1])
		if prev, dup := seen[version]; dup {
			return nil, fmt.Errorf("migrations %q and %q share version %d", prev, e.Name(), version)
		}
		seen[version] = e.Name()
		raw, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(raw)
		migs = append(migs, migration{version: version, name: m[2], sql: string(raw), checksum: hex.EncodeToString(sum[:])})
	}
	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })
	for i, m := range migs {
		if m.version != i+1 {
			return nil, fmt.Errorf("migration versions must be contiguous from 0001; missing %04d before %04d_%s", i+1, m.version, m.name)
		}
	}
	return migs, nil
}

// CheckForeignKeys fails if any row violates a foreign key (PRAGMA foreign_key_check must be empty).
func (d *DB) CheckForeignKeys(ctx context.Context) error {
	rows, err := d.Write.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("foreign_key_check: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		var table, parent sql.NullString
		var rowid, fkid sql.NullInt64
		_ = rows.Scan(&table, &rowid, &parent, &fkid)
		return fmt.Errorf("foreign key violation in table %q (parent %q)", table.String, parent.String)
	}
	return rows.Err()
}

// CheckIntegrity runs PRAGMA integrity_check and returns an error unless it reports "ok".
func (d *DB) CheckIntegrity(ctx context.Context) error {
	rows, err := d.Write.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		return fmt.Errorf("integrity_check: %w", err)
	}
	defer rows.Close()
	var problems []error
	for rows.Next() {
		var msg string
		if err := rows.Scan(&msg); err != nil {
			return err
		}
		if msg != "ok" {
			problems = append(problems, errors.New(msg))
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return errors.Join(problems...)
}
