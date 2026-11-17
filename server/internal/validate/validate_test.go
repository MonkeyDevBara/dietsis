package validate

import (
	"math"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

func TestRange(t *testing.T) {
	cases := []struct {
		v       float64
		integer bool
		bad     bool
	}{
		{18, true, false}, {17, true, true}, {100, true, false}, {101, true, true},
		{30.5, true, true}, {30.5, false, false}, {math.NaN(), false, true}, {math.Inf(1), false, true},
	}
	for _, c := range cases {
		e := Errors{}
		e.Range("age", c.v, 18, 100, c.integer)
		if (len(e) > 0) != c.bad {
			t.Errorf("Range(%v, integer=%v): got errors %v, want bad=%v", c.v, c.integer, e, c.bad)
		}
	}
}

func TestText(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		min, max  int
		multiline bool
		bad       bool
	}{
		{"ok", "Nasi putih", 1, 80, false, false},
		{"blank after trim", "   ", 1, 80, false, true},
		{"too long counts runes not bytes", strings.Repeat("é", 80), 1, 80, false, false},
		{"one rune too long", strings.Repeat("é", 81), 1, 80, false, true},
		{"NUL", "a\x00b", 1, 80, false, true},
		{"tab in single line", "a\tb", 1, 80, false, true},
		{"newline in single line", "a\nb", 1, 80, false, true},
		{"newline in note", "a\nb", 0, 500, true, false},
		{"escape in note", "a\x1bb", 0, 500, true, true},
		{"C1 control", "a\u0085b", 1, 80, false, true},
		{"invalid utf8", "a\xffb", 1, 80, false, true},
		{"empty allowed when min 0", "", 0, 500, true, false},
	}
	for _, c := range cases {
		e := Errors{}
		e.Text("f", c.in, c.min, c.max, c.multiline)
		if (len(e) > 0) != c.bad {
			t.Errorf("%s: errors %v, want bad=%v", c.name, e, c.bad)
		}
	}
	e := Errors{}
	if got := e.Text("f", "  Rina  ", 1, 60, false); got != "Rina" {
		t.Errorf("Text must return the trimmed value, got %q", got)
	}
}

func TestDate(t *testing.T) {
	cases := map[string]bool{
		"2026-11-17": true, "2026-11-18": true /* tomorrow UTC: UTC+14 devices */, "2026-11-19": false,
		"2000-01-01": true, "1999-12-31": false, "2026-02-30": false, "2024-02-29": true, "2026-02-29": false, "2028-02-29": false, /* future */
		"2026-1-01": false, "": false, "2026-11-17T00:00:00Z": false, "२०२६-११-१७": false,
	}
	for in, want := range cases {
		if got := ValidDate(in, now); got != want {
			t.Errorf("ValidDate(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestQuantity(t *testing.T) {
	cases := map[float64]bool{0.1: false, 0.5: false, 1: false, 1.15: false, 1.25: false, 2.675: true, 50: false, 50.01: true, 0.05: true, 0: true, -1: true, 1.005: true}
	for q, bad := range cases {
		e := Errors{}
		e.Quantity("q", q)
		if (len(e) > 0) != bad {
			t.Errorf("Quantity(%v): errors %v, want bad=%v", q, e, bad)
		}
	}
}

func TestOneofAndFirstErrorWins(t *testing.T) {
	e := Errors{}
	e.Oneof("meal", "brunch", "breakfast", "lunch")
	if e["meal"] == "" {
		t.Fatal("brunch must be rejected")
	}
	e.Add("meal", "second")
	if e["meal"] == "second" {
		t.Fatal("first message must win")
	}
}
