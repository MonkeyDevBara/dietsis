// Package export streams a user's data as CSV (docs/DECISIONS.md D-019): UTF-8 with BOM, comma
// separated, CRLF line ends, one dataset per file, and a guard against spreadsheet formula injection.
package export

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/MonkeyDevBara/dietsis/server/internal/nutrition"
)

var ErrUnknownDataset = errors.New("unknown dataset")

// Datasets lists the valid dataset names.
var Datasets = []string{"food_logs", "weights", "days", "custom_foods", "profile"}

// Safe neutralizes cells a spreadsheet would evaluate as a formula: values starting with
// = + - @ TAB or CR get a leading apostrophe. Only text cells go through it; numbers are written
// as-is (a negative number is not a formula).
func Safe(s string) string {
	if s != "" {
		switch s[0] {
		case '=', '+', '-', '@', '\t', '\r':
			return "'" + s
		}
	}
	return s
}

func fnum(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func cell(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return Safe(x)
	case float64:
		return fnum(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case sql.Null[float64]:
		if !x.Valid {
			return ""
		}
		return fnum(x.V)
	case sql.Null[int64]:
		if !x.Valid {
			return ""
		}
		return strconv.FormatInt(x.V, 10)
	}
	panic(fmt.Sprintf("export: unsupported cell type %T", v))
}

type writer struct {
	cw *csv.Writer
}

func newWriter(w io.Writer) (*writer, error) {
	if _, err := io.WriteString(w, "\uFEFF"); err != nil {
		return nil, err
	}
	cw := csv.NewWriter(w)
	cw.UseCRLF = true
	return &writer{cw: cw}, nil
}

func (w *writer) header(cols ...string) error { return w.cw.Write(cols) }

func (w *writer) row(vals ...any) error {
	out := make([]string, len(vals))
	for i, v := range vals {
		out[i] = cell(v)
	}
	return w.cw.Write(out)
}

func (w *writer) finish() error {
	w.cw.Flush()
	return w.cw.Error()
}

// Write streams one dataset for userID from q. Every query is scoped by user_id.
func Write(ctx context.Context, w io.Writer, q *sql.DB, userID, dataset string) error {
	cw, err := newWriter(w)
	if err != nil {
		return err
	}
	switch dataset {
	case "food_logs":
		err = foodLogs(ctx, cw, q, userID)
	case "weights":
		err = simple(ctx, cw, q, userID, []string{"tanggal", "berat_kg", "pinggang_cm"},
			`SELECT date, weight_kg, waist_cm FROM weights WHERE user_id = ? ORDER BY date`, 3)
	case "days":
		err = simple(ctx, cw, q, userID, []string{"tanggal", "air_gelas", "langkah", "catatan"},
			`SELECT date, water_glasses, steps, note FROM days WHERE user_id = ? ORDER BY date`, 4)
	case "custom_foods":
		err = simple(ctx, cw, q, userID, []string{"nama", "porsi", "berat_porsi_g", "kkal", "protein_g", "karbo_g", "lemak_g"},
			`SELECT name, serving_label, serving_grams, kcal, protein_g, carbs_g, fat_g FROM custom_foods WHERE user_id = ? ORDER BY name, id`, 7)
	case "profile":
		err = simple(ctx, cw, q, userID, []string{"nama", "gender", "usia", "tinggi_cm", "berat_kg", "target_berat_kg", "aktivitas", "target_kalori", "target_protein_g", "target_air_gelas"},
			`SELECT display_name, gender, age, height_cm, weight_kg, goal_weight_kg, activity_level, calorie_target, protein_target_g, water_target_glasses FROM profiles WHERE user_id = ?`, 10)
	default:
		return ErrUnknownDataset
	}
	if err != nil {
		return err
	}
	return cw.finish()
}

// simple runs a query returning n columns and writes each row as-is (typed by Scan targets).
func simple(ctx context.Context, w *writer, q *sql.DB, uid string, header []string, query string, n int) error {
	if err := w.header(header...); err != nil {
		return err
	}
	rows, err := q.QueryContext(ctx, query, uid)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		vals := make([]any, n)
		ptrs := make([]any, n)
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		out := make([]any, n)
		for i, v := range vals {
			switch x := v.(type) {
			case []byte:
				out[i] = string(x)
			default:
				out[i] = x // nil, int64, float64, string
			}
		}
		if err := w.row(out...); err != nil {
			return err
		}
	}
	return rows.Err()
}

func foodLogs(ctx context.Context, w *writer, q *sql.DB, uid string) error {
	if err := w.header("tanggal", "waktu_makan", "makanan", "porsi", "jumlah_porsi", "kkal_per_porsi", "protein_g_per_porsi", "karbo_g_per_porsi", "lemak_g_per_porsi",
		"total_kkal", "total_protein_g", "total_karbo_g", "total_lemak_g"); err != nil {
		return err
	}
	rows, err := q.QueryContext(ctx, `SELECT date, meal, name, serving_label, quantity, kcal, protein_g, carbs_g, fat_g
		FROM food_logs WHERE user_id = ? AND deleted_at IS NULL ORDER BY date, created_at, id`, uid)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var date, meal, name, label string
		var qty, kcal, p, c, f float64
		if err := rows.Scan(&date, &meal, &name, &label, &qty, &kcal, &p, &c, &f); err != nil {
			return err
		}
		t := nutrition.EntryTotals(nutrition.Nutrients{Kcal: kcal, ProteinG: p, CarbsG: c, FatG: f}, qty)
		if err := w.row(date, meal, name, label, qty, kcal, p, c, f, t.Kcal, t.ProteinG, t.CarbsG, t.FatG); err != nil {
			return err
		}
	}
	return rows.Err()
}

// Valid reports whether dataset is a known dataset name. Handlers check this before any bytes are
// written, so an unknown name can still be answered with a proper 400.
func Valid(dataset string) bool {
	for _, d := range Datasets {
		if d == dataset {
			return true
		}
	}
	return false
}
