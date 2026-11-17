// Package validate holds the input limits (docs/DECISIONS.md D-012) and small helpers to check them.
// The TypeScript twin is src/lib/validation.ts; keep the two in step.
package validate

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Errors maps a field name to a user-facing message (Indonesian, shown in the UI).
type Errors map[string]string

func (e Errors) Empty() bool { return len(e) == 0 }

// Add records the first message for a field.
func (e Errors) Add(field, msg string) {
	if msg == "" {
		return
	}
	if _, exists := e[field]; !exists {
		e[field] = msg
	}
}

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// Range checks min <= v <= max (and whole numbers when integer).
func (e Errors) Range(field string, v, min, max float64, integer bool) {
	switch {
	case math.IsNaN(v) || math.IsInf(v, 0):
		e.Add(field, "Wajib diisi angka")
	case integer && v != math.Trunc(v):
		e.Add(field, "Harus bilangan bulat")
	case v < min || v > max:
		e.Add(field, fmt.Sprintf("Harus antara %s dan %s", num(min), num(max)))
	}
}

// Text validates a trimmed string by rune count and forbids control characters.
// multiline allows \n, \r and \t (notes). It returns the trimmed value.
func (e Errors) Text(field, s string, min, max int, multiline bool) string {
	t := strings.TrimSpace(s)
	n := utf8.RuneCountInString(t)
	switch {
	case !utf8.ValidString(t):
		e.Add(field, "Mengandung karakter tidak valid")
	case n < min:
		e.Add(field, "Wajib diisi")
	case n > max:
		e.Add(field, fmt.Sprintf("Maksimal %d karakter", max))
	default:
		for _, r := range t {
			if unicode.IsControl(r) && !(multiline && (r == '\n' || r == '\r' || r == '\t')) {
				e.Add(field, "Mengandung karakter tidak valid")
				break
			}
		}
	}
	return t
}

// Oneof checks membership in an allowed set.
func (e Errors) Oneof(field, v string, allowed ...string) {
	for _, a := range allowed {
		if v == a {
			return
		}
	}
	e.Add(field, "Pilih salah satu")
}

const (
	minDate = "2000-01-01"
	dateFmt = "2006-01-02"
)

// Date checks a real calendar date in YYYY-MM-DD between 2000-01-01 and tomorrow (UTC), which
// covers every timezone up to UTC+14 (docs/DECISIONS.md D-013).
func (e Errors) Date(field, s string, now time.Time) {
	if !ValidDate(s, now) {
		e.Add(field, "Tanggal tidak valid")
	}
}

func ValidDate(s string, now time.Time) bool {
	if len(s) != len(dateFmt) {
		return false
	}
	if _, err := time.Parse(dateFmt, s); err != nil {
		return false
	}
	return s >= minDate && s <= now.UTC().AddDate(0, 0, 1).Format(dateFmt)
}

// Quantity checks 0.1..50 with at most two decimals. The tolerance avoids rejecting values like
// 1.15 whose float product (114.99999999999999) is not exactly integral.
func (e Errors) Quantity(field string, q float64) {
	e.Range(field, q, 0.1, 50, false)
	if _, bad := e[field]; bad {
		return
	}
	if math.Abs(q*100-math.Round(q*100)) > 1e-9 {
		e.Add(field, "Maksimal 2 angka desimal")
	}
}

// Whole converts a validated whole-number float to int.
func Whole(v float64) int { return int(math.Round(v)) }
