package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MonkeyDevBara/dietsis/server/internal/store"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newSessions(t *testing.T, secure bool) (*Sessions, *store.DB, *clock, store.User) {
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
	clk := &clock{t: time.UnixMilli(1_800_000_000_000)}
	u, err := db.UpsertGoogleUser(ctx, "sub", "a@x.com", "A", "", clk.t)
	if err != nil {
		t.Fatal(err)
	}
	return &Sessions{Store: db, TTL: 10 * 24 * time.Hour, Secure: secure, Now: clk.now}, db, clk, u
}

func TestCreateAuthenticateRevoke(t *testing.T) {
	s, _, _, u := newSessions(t, true)
	ctx := context.Background()
	tok, _, err := s.Create(ctx, u.ID, "UA")
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != tokenLen {
		t.Fatalf("token length = %d", len(tok))
	}
	got, _, err := s.Authenticate(ctx, tok)
	if err != nil || got.ID != u.ID {
		t.Fatalf("authenticate = %+v, %v", got, err)
	}
	if err := s.Revoke(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Authenticate(ctx, tok); err != ErrNoSession {
		t.Fatalf("revoked token must fail, got %v", err)
	}
}

func TestTokensAreUniqueAndOnlyHashesAreStored(t *testing.T) {
	s, db, _, u := newSessions(t, true)
	ctx := context.Background()
	seen := map[string]bool{}
	var last string
	for range 20 {
		tok, _, err := s.Create(ctx, u.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		if seen[tok] {
			t.Fatal("token reused across logins")
		}
		seen[tok], last = true, tok
	}
	// The raw token must not appear anywhere in the database file contents.
	var blob []byte
	if err := db.Read.QueryRow(`SELECT token_hash FROM sessions LIMIT 1`).Scan(&blob); err != nil {
		t.Fatal(err)
	}
	if len(blob) != 32 || strings.Contains(string(blob), last) {
		t.Fatal("sessions.token_hash must be a 32-byte SHA-256, not the token")
	}
	var n int
	_ = db.Read.QueryRow(`SELECT count(*) FROM sessions WHERE CAST(token_hash AS TEXT) = ?`, last).Scan(&n)
	if n != 0 {
		t.Fatal("raw token found in sessions table")
	}
}

func TestMalformedTokensAreRejectedWithoutDBLookup(t *testing.T) {
	s, _, _, _ := newSessions(t, true)
	for _, tok := range []string{"", "short", strings.Repeat("a", 44), strings.Repeat("a", 42)} {
		if _, _, err := s.Authenticate(context.Background(), tok); err != ErrNoSession {
			t.Errorf("token %q: got %v", tok, err)
		}
	}
}

func TestExpiryAndSlidingRenewal(t *testing.T) {
	s, _, clk, u := newSessions(t, true)
	ctx := context.Background()
	tok, _, _ := s.Create(ctx, u.ID, "")

	// Before half of the TTL: no renewal.
	clk.t = clk.t.Add(4 * 24 * time.Hour)
	if _, refreshed, err := s.Authenticate(ctx, tok); err != nil || !refreshed.IsZero() {
		t.Fatalf("early use: refreshed=%v err=%v", refreshed, err)
	}
	// Past half: renewed to a full TTL from now.
	clk.t = clk.t.Add(2 * 24 * time.Hour) // day 6 of 10
	_, refreshed, err := s.Authenticate(ctx, tok)
	if err != nil || !refreshed.Equal(clk.t.Add(s.TTL)) {
		t.Fatalf("refreshed = %v, want %v (err %v)", refreshed, clk.t.Add(s.TTL), err)
	}
	// Still valid well beyond the original expiry (day 6 + 9 = day 15).
	clk.t = clk.t.Add(9 * 24 * time.Hour)
	if _, _, err := s.Authenticate(ctx, tok); err != nil {
		t.Fatalf("renewed session should still be valid: %v", err)
	}
	// And dies after a full idle TTL.
	clk.t = clk.t.Add(11 * 24 * time.Hour)
	if _, _, err := s.Authenticate(ctx, tok); err != ErrNoSession {
		t.Fatalf("idle session must expire, got %v", err)
	}
}

func TestRevokeAllKillsEverySessionOfTheUserOnly(t *testing.T) {
	s, db, clk, u := newSessions(t, true)
	ctx := context.Background()
	other, _ := db.UpsertGoogleUser(ctx, "sub2", "b@x.com", "B", "", clk.t)
	a1, _, _ := s.Create(ctx, u.ID, "")
	a2, _, _ := s.Create(ctx, u.ID, "")
	b1, _, _ := s.Create(ctx, other.ID, "")
	if err := s.RevokeAll(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{a1, a2} {
		if _, _, err := s.Authenticate(ctx, tok); err != ErrNoSession {
			t.Fatal("session of revoked user still valid")
		}
	}
	if _, _, err := s.Authenticate(ctx, b1); err != nil {
		t.Fatalf("other user's session must survive: %v", err)
	}
}

func TestCookieAttributes(t *testing.T) {
	for _, secure := range []bool{true, false} {
		s, _, clk, _ := newSessions(t, secure)
		rec := httptest.NewRecorder()
		s.SetCookie(rec, "tok", clk.t.Add(time.Hour))
		c := rec.Result().Cookies()[0]
		wantName := devCookieName
		if secure {
			wantName = secureCookieName
		}
		if c.Name != wantName || !c.HttpOnly || c.Secure != secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.Domain != "" {
			t.Fatalf("secure=%v: unexpected cookie %+v", secure, c)
		}
		if c.MaxAge != 3600 {
			t.Fatalf("MaxAge = %d", c.MaxAge)
		}

		rec = httptest.NewRecorder()
		s.ClearCookie(rec)
		if c := rec.Result().Cookies()[0]; c.MaxAge >= 0 || c.Value != "" || !c.HttpOnly {
			t.Fatalf("clear cookie wrong: %+v", c)
		}
	}
}

func TestTokenFromRequestUsesTheRightCookieName(t *testing.T) {
	s, _, _, _ := newSessions(t, true)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: devCookieName, Value: "dev-cookie-must-be-ignored-in-production"})
	if got := s.Token(req); got != "" {
		t.Fatalf("production must ignore the non-__Host cookie, got %q", got)
	}
	req.AddCookie(&http.Cookie{Name: secureCookieName, Value: "abc"})
	if got := s.Token(req); got != "abc" {
		t.Fatalf("Token = %q", got)
	}
}
