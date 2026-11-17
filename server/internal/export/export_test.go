package export

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MonkeyDevBara/dietsis/server/internal/store"
)

type env struct {
	db   *store.DB
	a, b string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	ua, _ := db.UpsertGoogleUser(ctx, "sa", "a@x", "A", "", time.Now())
	ub, _ := db.UpsertGoogleUser(ctx, "sb", "b@x", "B", "", time.Now())
	return &env{db: db, a: ua.ID, b: ub.ID}
}

func (e *env) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := e.db.Write.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func (e *env) export(t *testing.T, uid, dataset string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Write(context.Background(), &buf, e.db.Read, uid, dataset); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func parse(t *testing.T, out string) [][]string {
	t.Helper()
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(out, "\uFEFF"))).ReadAll()
	if err != nil {
		t.Fatalf("output is not valid CSV: %v\n%s", err, out)
	}
	return rows
}

func insertLog(e *env, t *testing.T, uid, id, date, name string, qty float64, deleted any) {
	e.exec(t, `INSERT INTO food_logs (user_id, id, date, meal, food_id, name, serving_label, quantity, kcal, protein_g, carbs_g, fat_g, version, created_at, updated_at, deleted_at)
		VALUES (?, ?, ?, 'lunch', NULL, ?, '1 porsi', ?, 195, 4, 42, 0.5, 1, 1, 1, ?)`, uid, id, date, name, qty, deleted)
}

func TestFormatBOMCRLFAndHeader(t *testing.T) {
	e := newEnv(t)
	out := e.export(t, e.a, "weights")
	if !strings.HasPrefix(out, "\uFEFF") {
		t.Fatal("missing UTF-8 BOM (Excel needs it)")
	}
	if out != "\uFEFFtanggal,berat_kg,pinggang_cm\r\n" {
		t.Fatalf("empty dataset must be just the header with CRLF, got %q", out)
	}
}

func TestFoodLogsExportTotalsOrderAndDeletedExcluded(t *testing.T) {
	e := newEnv(t)
	insertLog(e, t, e.a, "b", "2026-11-17", "Nasi putih", 1.5, nil)
	insertLog(e, t, e.a, "a", "2026-11-16", "Telur", 1, nil)
	insertLog(e, t, e.a, "c", "2026-11-17", "Dihapus", 1, 123)
	rows := parse(t, e.export(t, e.a, "food_logs"))
	if len(rows) != 3 {
		t.Fatalf("rows = %d (header + 2 active): %v", len(rows), rows)
	}
	if rows[1][0] != "2026-11-16" || rows[2][2] != "Nasi putih" {
		t.Fatalf("ordered by date: %v", rows)
	}
	// 1.5 portions of 195 kcal / 4 / 42 / 0.5 → 293 / 6 / 63 / 0.8 (same rounding as the API).
	if got := rows[2][9:]; strings.Join(got, ",") != "293,6,63,0.8" {
		t.Fatalf("totals = %v", got)
	}
}

func TestExportOnlyContainsTheRequestingUsersRows(t *testing.T) {
	e := newEnv(t)
	insertLog(e, t, e.a, "a", "2026-11-17", "Punya A", 1, nil)
	insertLog(e, t, e.b, "a", "2026-11-17", "Punya B", 1, nil)
	e.exec(t, `INSERT INTO weights (user_id, date, weight_kg, version, updated_at) VALUES (?, '2026-11-17', 70, 1, 1), (?, '2026-11-17', 55, 1, 1)`, e.a, e.b)
	for _, ds := range []string{"food_logs", "weights"} {
		out := e.export(t, e.a, ds)
		if strings.Contains(out, "Punya B") || strings.Contains(out, ",55,") {
			t.Fatalf("%s export leaked user B's data: %s", ds, out)
		}
	}
}

func TestSpreadsheetFormulasAreNeutralised(t *testing.T) {
	e := newEnv(t)
	for i, name := range []string{`=HYPERLINK("http://evil","x")`, "+1+1", "-2+3", "@SUM(A1)", "\tcmd"} {
		insertLog(e, t, e.a, string(rune('a'+i)), "2026-11-17", name, 1, nil)
	}
	rows := parse(t, e.export(t, e.a, "food_logs"))
	for _, r := range rows[1:] {
		if !strings.HasPrefix(r[2], "'") {
			t.Errorf("name %q is not neutralised", r[2])
		}
	}
	// Numbers are not text: a negative number must stay a number.
	e.exec(t, `INSERT INTO days (user_id, date, water_glasses, steps, note, version, updated_at) VALUES (?, '2026-11-17', 3, 5200, '=cmd()', 0, 1)`, e.a)
	r := parse(t, e.export(t, e.a, "days"))[1]
	if r[1] != "3" || r[2] != "5200" || r[3] != "'=cmd()" {
		t.Fatalf("days row = %v", r)
	}
	if Safe("-5") != "'-5" || Safe("5") != "5" || Safe("") != "" {
		t.Fatal("Safe misbehaves")
	}
}

func TestQuotingOfSpecialCharacters(t *testing.T) {
	e := newEnv(t)
	insertLog(e, t, e.a, "a", "2026-11-17", "Nasi, \"special\"\nbaris dua", 1, nil)
	rows := parse(t, e.export(t, e.a, "food_logs"))
	if rows[1][2] != "Nasi, \"special\"\nbaris dua" {
		t.Fatalf("round-trip failed: %q", rows[1][2])
	}
}

func TestOtherDatasets(t *testing.T) {
	e := newEnv(t)
	e.exec(t, `INSERT INTO weights (user_id, date, weight_kg, waist_cm, version, updated_at) VALUES (?, '2026-11-10', 73, 84, 1, 1), (?, '2026-11-17', 72.4, NULL, 1, 1)`, e.a, e.a)
	e.exec(t, `INSERT INTO custom_foods (user_id, id, name, serving_label, serving_grams, kcal, protein_g, carbs_g, fat_g, version, created_at, updated_at) VALUES (?, 'x', 'Smoothie', '1 gelas', NULL, 180, 6, 30, 3, 1, 1, 1)`, e.a)
	e.exec(t, `INSERT INTO profiles (user_id, display_name, gender, age, height_cm, weight_kg, goal_weight_kg, activity_level, calorie_target, protein_target_g, water_target_glasses, created_at, updated_at) VALUES (?, 'Rina', 'female', 30, 160, 70, 60, 'light', 1650, 96, 9, 1, 1)`, e.a)

	if r := parse(t, e.export(t, e.a, "weights")); r[1][2] != "84" || r[2][1] != "72.4" || r[2][2] != "" {
		t.Fatalf("weights: %v", r)
	}
	if r := parse(t, e.export(t, e.a, "custom_foods")); r[1][2] != "" || r[1][3] != "180" {
		t.Fatalf("custom_foods: %v", r)
	}
	if r := parse(t, e.export(t, e.a, "profile")); r[1][0] != "Rina" || r[1][7] != "1650" {
		t.Fatalf("profile: %v", r)
	}
	if r := parse(t, e.export(t, e.b, "profile")); len(r) != 1 {
		t.Fatalf("user without a profile gets only the header: %v", r)
	}
}

func TestUnknownDatasetIsRejectedAndAllKnownOnesWork(t *testing.T) {
	e := newEnv(t)
	var buf bytes.Buffer
	err := Write(context.Background(), &buf, e.db.Read, e.a, "users; DROP TABLE users")
	if !errors.Is(err, ErrUnknownDataset) {
		t.Fatalf("got %v", err)
	}
	for _, name := range Datasets {
		var b bytes.Buffer
		if err := Write(context.Background(), &b, e.db.Read, e.a, name); err != nil {
			t.Errorf("dataset %s: %v", name, err)
		}
	}
}
