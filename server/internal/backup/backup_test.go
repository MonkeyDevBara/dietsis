package backup

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MonkeyDevBara/dietsis/server/internal/store"
)

var t0 = time.Date(2026, 11, 17, 2, 30, 0, 0, time.UTC)

func newDB(t *testing.T) *store.DB {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "data", "dietsis.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return db
}

func seed(t *testing.T, db *store.DB, n int) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.UpsertGoogleUser(ctx, "sub", "a@x.com", "Rina", "", t0); err != nil {
		t.Fatal(err)
	}
	var uid string
	if err := db.Read.QueryRow(`SELECT id FROM users`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	for i := range n {
		if _, err := db.Write.Exec(`INSERT INTO food_logs (user_id, id, date, meal, name, serving_label, quantity, kcal, protein_g, carbs_g, fat_g, version, created_at, updated_at)
			VALUES (?, ?, '2026-11-17', 'lunch', ?, '1', 1, 100, 1, 1, 1, 1, 1, 1)`, uid, fmt.Sprintf("log-%05d", i), fmt.Sprintf("Makanan %d", i)); err != nil {
			t.Fatal(err)
		}
	}
}

func count(t *testing.T, path, table string) int {
	t.Helper()
	d, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("%s: %v", table, err)
	}
	return n
}

func TestCreateProducesVerifiedPrivateGzip(t *testing.T) {
	db := newDB(t)
	seed(t, db, 50)
	dir := filepath.Join(t.TempDir(), "backups")
	path, err := Create(context.Background(), db, dir, t0)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "dietsis-20261117-023000.db.gz" {
		t.Fatalf("name = %s", filepath.Base(path))
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode = %v, want 0600", fi.Mode().Perm())
	}
	if di, _ := os.Stat(dir); di.Mode().Perm() != 0o700 {
		t.Fatalf("backup dir mode = %v, want 0700", di.Mode().Perm())
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(left) != 0 {
		t.Fatalf("temporary files left behind: %v", left)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.partial")); len(left) != 0 {
		t.Fatalf("partial files left behind: %v", left)
	}
}

func TestRestoreRoundTripGivesIdenticalData(t *testing.T) {
	db := newDB(t)
	seed(t, db, 200)
	dir := t.TempDir()
	bk, err := Create(context.Background(), db, dir, t0)
	if err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(t.TempDir(), "restored", "dietsis.db")
	prev, err := Restore(context.Background(), bk, target, t0)
	if err != nil {
		t.Fatal(err)
	}
	if prev != "" {
		t.Fatalf("no previous database expected, got %s", prev)
	}
	if n := count(t, target, "food_logs"); n != 200 {
		t.Fatalf("restored food_logs = %d", n)
	}
	// The restored file opens as a normal Dietsis database and is migration-consistent.
	rdb, err := store.Open(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	defer rdb.Close()
	if err := rdb.Migrate(context.Background()); err != nil {
		t.Fatalf("restored database must pass the migration checksum check: %v", err)
	}
	var name string
	if err := rdb.Read.QueryRow(`SELECT name FROM food_logs WHERE id = 'log-00007'`).Scan(&name); err != nil || name != "Makanan 7" {
		t.Fatalf("row content differs: %q %v", name, err)
	}
	if fi, _ := os.Stat(target); fi.Mode().Perm() != 0o600 {
		t.Fatalf("restored mode = %v", fi.Mode().Perm())
	}
}

func TestRestoreKeepsThePreviousDatabaseAndRemovesStaleWAL(t *testing.T) {
	db := newDB(t)
	seed(t, db, 5)
	bk, err := Create(context.Background(), db, t.TempDir(), t0)
	if err != nil {
		t.Fatal(err)
	}
	// An existing "live" database with 1 row and leftover WAL/SHM files.
	live := newDB(t)
	seed(t, live, 1)
	path := live.Path()
	_ = live.Close()
	for _, s := range []string{"-wal", "-shm"} {
		if err := os.WriteFile(path+s, []byte("stale"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	prev, err := Restore(context.Background(), bk, path, t0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prev, ".pre-restore-20261117-023000") {
		t.Fatalf("previous = %q", prev)
	}
	if n := count(t, prev, "food_logs"); n != 1 {
		t.Fatalf("the previous database must be preserved intact, got %d rows", n)
	}
	if n := count(t, path, "food_logs"); n != 5 {
		t.Fatalf("restored rows = %d", n)
	}
	for _, s := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(path + s); err == nil {
			t.Fatalf("stale %s file must be removed", s)
		}
	}
}

func TestBrokenBackupsAreRejectedAndTheLiveDatabaseIsUntouched(t *testing.T) {
	good := newDB(t)
	seed(t, good, 3)
	goodBackup, err := Create(context.Background(), good, t.TempDir(), t0)
	if err != nil {
		t.Fatal(err)
	}
	goodBytes, _ := os.ReadFile(goodBackup)

	live := newDB(t)
	seed(t, live, 9)
	livePath := live.Path()
	_ = live.Close()
	before, _ := os.ReadFile(livePath)

	gz := func(b []byte) []byte {
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		_, _ = w.Write(b)
		_ = w.Close()
		return buf.Bytes()
	}
	// A valid SQLite file that is not a Dietsis database.
	other := filepath.Join(t.TempDir(), "other.db")
	od, _ := sql.Open("sqlite", "file:"+other)
	_, _ = od.Exec(`CREATE TABLE unrelated (x INTEGER)`)
	_ = od.Close()
	otherBytes, _ := os.ReadFile(other)

	// A real backup with a corrupted page in the middle.
	zr, _ := gzip.NewReader(bytes.NewReader(goodBytes))
	var dbBytes bytes.Buffer
	_, _ = dbBytes.ReadFrom(zr)
	corrupt := append([]byte(nil), dbBytes.Bytes()...)
	for i := len(corrupt) / 2; i < len(corrupt)/2+4096 && i < len(corrupt); i++ {
		corrupt[i] = 0xFF
	}

	cases := map[string][]byte{
		"not gzip":            []byte("hello"),
		"empty":               {},
		"truncated gzip":      goodBytes[:len(goodBytes)/2],
		"gzip of garbage":     gz([]byte("this is not a database")),
		"foreign sqlite file": gz(otherBytes),
		"corrupted pages":     gz(corrupt),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			bad := filepath.Join(t.TempDir(), "bad.db.gz")
			if err := os.WriteFile(bad, content, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Restore(context.Background(), bad, livePath, t0); err == nil {
				t.Fatal("a broken backup must be rejected")
			}
			after, _ := os.ReadFile(livePath)
			if !bytes.Equal(before, after) {
				t.Fatal("the live database was modified by a failed restore")
			}
			if _, err := os.Stat(livePath + ".restore.tmp"); err == nil {
				t.Fatal("temporary restore file left behind")
			}
		})
	}
	if _, err := Restore(context.Background(), filepath.Join(t.TempDir(), "missing.db.gz"), livePath, t0); err == nil {
		t.Fatal("missing file must fail")
	}
}

func TestBackupIsConsistentWhileTheServerKeepsWriting(t *testing.T) {
	db := newDB(t)
	seed(t, db, 20)
	var uid string
	_ = db.Read.QueryRow(`SELECT id FROM users`).Scan(&uid)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_ = db.WithTx(context.Background(), func(tx *sql.Tx) error {
				_, err := tx.Exec(`INSERT INTO food_logs (user_id, id, date, meal, name, serving_label, quantity, kcal, protein_g, carbs_g, fat_g, version, created_at, updated_at)
					VALUES (?, ?, '2026-11-17', 'lunch', 'x', '1', 1, 1, 0, 0, 0, 1, 1, 1)`, uid, fmt.Sprintf("live-%06d", i))
				return err
			})
		}
	}()

	dir := t.TempDir()
	var paths []string
	for i := range 5 {
		p, err := Create(context.Background(), db, dir, t0.Add(time.Duration(i)*time.Second))
		if err != nil {
			close(stop)
			t.Fatalf("backup %d under write load: %v", i, err)
		}
		paths = append(paths, p)
	}
	close(stop)
	wg.Wait()

	for _, p := range paths {
		target := filepath.Join(t.TempDir(), "r.db")
		if _, err := Restore(context.Background(), p, target, t0); err != nil {
			t.Fatalf("%s did not restore: %v", filepath.Base(p), err)
		}
		if n := count(t, target, "food_logs"); n < 20 {
			t.Fatalf("snapshot lost rows: %d", n)
		}
	}
}

func TestPruneKeepsTheNewestAndIgnoresOtherFiles(t *testing.T) {
	dir := t.TempDir()
	for i := range 6 {
		name := fmt.Sprintf("dietsis-2026111%d-023000.db.gz", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, other := range []string{"notes.txt", "dietsis-latest.db", "dietsis-x.db.gz.partial"} {
		_ = os.WriteFile(filepath.Join(dir, other), []byte("keep me"), 0o600)
	}
	removed, err := Prune(dir, 3)
	if err != nil || len(removed) != 3 || removed[0] != "dietsis-20261110-023000.db.gz" {
		t.Fatalf("removed = %v, err = %v", removed, err)
	}
	names, _ := List(dir)
	if len(names) != 3 || names[0] != "dietsis-20261113-023000.db.gz" {
		t.Fatalf("kept = %v", names)
	}
	for _, other := range []string{"notes.txt", "dietsis-latest.db", "dietsis-x.db.gz.partial"} {
		if _, err := os.Stat(filepath.Join(dir, other)); err != nil {
			t.Fatalf("%s must not be touched by retention", other)
		}
	}
	if removed, _ := Prune(dir, 3); len(removed) != 0 {
		t.Fatal("nothing left to prune")
	}
	if _, err := Prune(filepath.Join(dir, "missing"), 3); err != nil {
		t.Fatalf("a missing directory is not an error: %v", err)
	}
}
