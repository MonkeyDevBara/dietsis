package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
)

func openTemp(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "data", "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func migrated(t *testing.T) *DB {
	t.Helper()
	db := openTemp(t)
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

func scalar[T any](t *testing.T, q *sql.DB, query string, args ...any) T {
	t.Helper()
	var v T
	if err := q.QueryRow(query, args...).Scan(&v); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return v
}

func TestOpenAppliesPragmasToBothPools(t *testing.T) {
	db := openTemp(t)
	for name, pool := range map[string]*sql.DB{"write": db.Write, "read": db.Read} {
		if got := scalar[string](t, pool, "PRAGMA journal_mode"); got != "wal" {
			t.Errorf("%s journal_mode = %q", name, got)
		}
		if got := scalar[int](t, pool, "PRAGMA foreign_keys"); got != 1 {
			t.Errorf("%s foreign_keys = %d", name, got)
		}
		if got := scalar[int](t, pool, "PRAGMA busy_timeout"); got != 5000 {
			t.Errorf("%s busy_timeout = %d", name, got)
		}
		if got := scalar[int](t, pool, "PRAGMA synchronous"); got != 1 { // 1 = NORMAL
			t.Errorf("%s synchronous = %d", name, got)
		}
	}
	if got := scalar[int](t, db.Read, "PRAGMA query_only"); got != 1 {
		t.Errorf("read pool must be query_only, got %d", got)
	}
}

func TestOpenRestrictsFilePermissions(t *testing.T) {
	db := openTemp(t)
	fi, err := os.Stat(db.Path())
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("db file mode = %v, want 0600", fi.Mode().Perm())
	}
	di, _ := os.Stat(filepath.Dir(db.Path()))
	if di.Mode().Perm() != 0o700 {
		t.Errorf("data dir mode = %v, want 0700", di.Mode().Perm())
	}
}

func TestReadPoolCannotWrite(t *testing.T) {
	db := migrated(t)
	_, err := db.Read.Exec(`INSERT INTO users (id, google_sub, email, created_at, updated_at) VALUES ('u','s','e',1,1)`)
	if err == nil || !strings.Contains(err.Error(), "readonly") {
		t.Fatalf("read pool write should fail as readonly, got %v", err)
	}
}

func TestMigrateAppliesSchemaAndIsIdempotent(t *testing.T) {
	db := migrated(t)
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if n := scalar[int](t, db.Read, "SELECT count(*) FROM schema_migrations"); n != 1 {
		t.Fatalf("schema_migrations rows = %d, want 1", n)
	}
	for _, table := range []string{"users", "sessions", "profiles", "favorites", "days", "food_logs", "custom_foods", "weights", "sync_operations", "change_log"} {
		if n := scalar[int](t, db.Read, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table); n != 1 {
			t.Errorf("table %s missing", table)
		}
	}
	if err := db.CheckIntegrity(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationFailureRollsBackCompletely(t *testing.T) {
	db := openTemp(t)
	fsys := fstest.MapFS{
		"0001_ok.sql":  {Data: []byte(`CREATE TABLE a (id INTEGER PRIMARY KEY) STRICT;`)},
		"0002_bad.sql": {Data: []byte(`CREATE TABLE b (id INTEGER PRIMARY KEY) STRICT; INSERT INTO nonexistent VALUES (1);`)},
	}
	err := db.MigrateFS(context.Background(), fsys)
	if err == nil || !strings.Contains(err.Error(), "0002_bad") {
		t.Fatalf("want failure naming 0002_bad, got %v", err)
	}
	if n := scalar[int](t, db.Read, "SELECT count(*) FROM sqlite_master WHERE name='b'"); n != 0 {
		t.Error("table b from the failed migration must be rolled back")
	}
	if n := scalar[int](t, db.Read, "SELECT count(*) FROM sqlite_master WHERE name='a'"); n != 1 {
		t.Error("table a from the successful migration must remain")
	}
	if n := scalar[int](t, db.Read, "SELECT count(*) FROM schema_migrations"); n != 1 {
		t.Errorf("only 0001 should be recorded, got %d rows", n)
	}
}

func TestMigrateRejectsModifiedAppliedMigration(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	if err := db.MigrateFS(ctx, fstest.MapFS{"0001_a.sql": {Data: []byte(`CREATE TABLE a (id INTEGER PRIMARY KEY) STRICT;`)}}); err != nil {
		t.Fatal(err)
	}
	err := db.MigrateFS(ctx, fstest.MapFS{"0001_a.sql": {Data: []byte(`CREATE TABLE a (id INTEGER PRIMARY KEY, extra TEXT) STRICT;`)}})
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("want checksum error, got %v", err)
	}
}

func TestMigrateRefusesNewerDatabase(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	two := fstest.MapFS{
		"0001_a.sql": {Data: []byte(`CREATE TABLE a (id INTEGER PRIMARY KEY) STRICT;`)},
		"0002_b.sql": {Data: []byte(`CREATE TABLE b (id INTEGER PRIMARY KEY) STRICT;`)},
	}
	if err := db.MigrateFS(ctx, two); err != nil {
		t.Fatal(err)
	}
	err := db.MigrateFS(ctx, fstest.MapFS{"0001_a.sql": two["0001_a.sql"]})
	if err == nil || !strings.Contains(err.Error(), "newer database") {
		t.Fatalf("want refusal for newer database, got %v", err)
	}
}

func TestLoadMigrationsValidatesNames(t *testing.T) {
	cases := map[string]fstest.MapFS{
		"bad name":   {"init.sql": {Data: []byte("")}},
		"gap":        {"0001_a.sql": {Data: []byte("")}, "0003_c.sql": {Data: []byte("")}},
		"not from 1": {"0002_b.sql": {Data: []byte("")}},
	}
	for name, fsys := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := loadMigrations(fsys); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestForeignKeysAreEnforcedAndCascade(t *testing.T) {
	db := migrated(t)
	ctx := context.Background()

	_, err := db.Write.ExecContext(ctx, `INSERT INTO weights (user_id, date, weight_kg, updated_at) VALUES ('ghost','2026-11-17',70,1)`)
	if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("orphan insert must fail with FK error, got %v", err)
	}

	mustExec(t, db, `INSERT INTO users (id, google_sub, email, created_at, updated_at) VALUES ('u1','s1','a@x',1,1)`)
	mustExec(t, db, `INSERT INTO weights (user_id, date, weight_kg, updated_at) VALUES ('u1','2026-11-17',70,1)`)
	mustExec(t, db, `DELETE FROM users WHERE id='u1'`)
	if n := scalar[int](t, db.Read, `SELECT count(*) FROM weights`); n != 0 {
		t.Fatalf("weights must cascade-delete with the user, %d left", n)
	}
}

func TestOneWeightPerUserPerDateButSameDateForOtherUser(t *testing.T) {
	db := migrated(t)
	mustExec(t, db, `INSERT INTO users (id, google_sub, email, created_at, updated_at) VALUES ('u1','s1','a@x',1,1), ('u2','s2','b@x',1,1)`)
	mustExec(t, db, `INSERT INTO weights (user_id, date, weight_kg, updated_at) VALUES ('u1','2026-11-17',70,1)`)
	mustExec(t, db, `INSERT INTO weights (user_id, date, weight_kg, updated_at) VALUES ('u2','2026-11-17',65,1)`)
	_, err := db.Write.Exec(`INSERT INTO weights (user_id, date, weight_kg, updated_at) VALUES ('u1','2026-11-17',71,1)`)
	if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("duplicate (user, date) must fail, got %v", err)
	}
}

func TestStrictTablesRejectWrongTypes(t *testing.T) {
	db := migrated(t)
	mustExec(t, db, `INSERT INTO users (id, google_sub, email, created_at, updated_at) VALUES ('u1','s1','a@x',1,1)`)
	_, err := db.Write.Exec(`INSERT INTO weights (user_id, date, weight_kg, updated_at) VALUES ('u1','2026-11-17','heavy',1)`)
	if err == nil {
		t.Fatal("STRICT table must reject text in a REAL column")
	}
}

// The water increment statement from docs/DATABASE.md: one atomic UPSERT, safe under concurrency.
const waterUpsert = `INSERT INTO days (user_id, date, water_glasses, updated_at) VALUES (?1, ?2, max(0, min(100, ?3)), ?4)
ON CONFLICT (user_id, date) DO UPDATE
SET water_glasses = max(0, min(100, water_glasses + ?3)), updated_at = ?4`

func TestConcurrentWaterIncrementsAreNotLost(t *testing.T) {
	db := migrated(t)
	mustExec(t, db, `INSERT INTO users (id, google_sub, email, created_at, updated_at) VALUES ('u1','s1','a@x',1,1)`)

	const workers, perWorker = 8, 10
	var wg sync.WaitGroup
	errs := make(chan error, workers*perWorker)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range perWorker {
				err := db.WithTx(context.Background(), func(tx *sql.Tx) error {
					_, err := tx.Exec(waterUpsert, "u1", "2026-11-17", 1, 1)
					return err
				})
				if err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("increment failed: %v", err)
	}
	if got := scalar[int](t, db.Read, `SELECT water_glasses FROM days WHERE user_id='u1'`); got != workers*perWorker {
		t.Fatalf("water_glasses = %d, want %d", got, workers*perWorker)
	}
}

func TestWaterIncrementClampsAtBounds(t *testing.T) {
	db := migrated(t)
	mustExec(t, db, `INSERT INTO users (id, google_sub, email, created_at, updated_at) VALUES ('u1','s1','a@x',1,1)`)
	for _, step := range []struct{ delta, want int }{{-5, 0}, {20, 20}, {20, 40}, {20, 60}, {20, 80}, {20, 100}, {20, 100}, {-1, 99}} {
		mustExec(t, db, waterUpsert, "u1", "2026-11-17", step.delta, 1)
		if got := scalar[int](t, db.Read, `SELECT water_glasses FROM days WHERE user_id='u1'`); got != step.want {
			t.Fatalf("after delta %d: got %d, want %d", step.delta, got, step.want)
		}
	}
}

func TestWithTxRollsBackOnError(t *testing.T) {
	db := migrated(t)
	err := db.WithTx(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO users (id, google_sub, email, created_at, updated_at) VALUES ('u1','s1','a@x',1,1)`); err != nil {
			return err
		}
		return fmt.Errorf("boom")
	})
	if err == nil {
		t.Fatal("want error")
	}
	if n := scalar[int](t, db.Read, `SELECT count(*) FROM users`); n != 0 {
		t.Fatalf("rolled-back insert is visible: %d rows", n)
	}
}

// Documented indexes must actually serve the hot queries (docs/DATABASE.md → Index).
func TestHotQueriesUseIndexes(t *testing.T) {
	db := migrated(t)
	cases := []struct {
		name, query, wantIndex string
	}{
		{"diary by date range", `SELECT * FROM food_logs WHERE user_id=? AND date BETWEEN ? AND ? AND deleted_at IS NULL`, "food_logs_user_date_idx"},
		{"recent foods", `SELECT * FROM food_logs WHERE user_id=? AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 15`, "food_logs_user_recent_idx"},
		{"sync pull", `SELECT seq, entity, entity_key FROM change_log WHERE user_id=? AND seq > ? ORDER BY seq LIMIT 500`, "change_log_user_seq_idx"},
		{"soft-delete purge", `SELECT user_id, id FROM food_logs WHERE deleted_at IS NOT NULL AND deleted_at < ?`, "food_logs_deleted_idx"},
		{"session expiry purge", `SELECT token_hash FROM sessions WHERE expires_at < ?`, "sessions_expires_idx"},
		{"logout all devices", `SELECT token_hash FROM sessions WHERE user_id=?`, "sessions_user_idx"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n := strings.Count(c.query, "?")
			args := make([]any, n)
			for i := range args {
				args[i] = 1
			}
			rows, err := db.Read.Query("EXPLAIN QUERY PLAN "+c.query, args...)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var plan []string
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatal(err)
				}
				plan = append(plan, detail)
			}
			joined := strings.Join(plan, "; ")
			if !strings.Contains(joined, c.wantIndex) {
				t.Fatalf("plan %q does not use %s", joined, c.wantIndex)
			}
		})
	}
}

func mustExec(t *testing.T, db *DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Write.Exec(query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}
