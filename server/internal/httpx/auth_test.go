package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MonkeyDevBara/dietsis/server/internal/auth"
	"github.com/MonkeyDevBara/dietsis/server/internal/limit"
	"github.com/MonkeyDevBara/dietsis/server/internal/store"
	dsync "github.com/MonkeyDevBara/dietsis/server/internal/sync"
)

const testOrigin = "https://dietsis.example"

type fakeVerifier struct {
	claims map[string]auth.Claims
	err    error // returned for unknown tokens when set (default: ErrInvalidToken)
}

func (f fakeVerifier) Verify(_ context.Context, tok string) (auth.Claims, error) {
	if c, ok := f.claims[tok]; ok {
		return c, nil
	}
	if f.err != nil {
		return auth.Claims{}, f.err
	}
	return auth.Claims{}, fmt.Errorf("%w: test reason with secret-detail", auth.ErrInvalidToken)
}

type env struct {
	t        *testing.T
	h        http.Handler
	db       *store.DB
	sessions *auth.Sessions
	now      *time.Time
}

type envOpts struct {
	limits   *Limits
	verifier auth.IDTokenVerifier
	noVerify bool
}

func newEnv(t *testing.T, o envOpts) *env {
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
	now := time.Now()
	e := &env{t: t, db: db, now: &now}
	e.sessions = &auth.Sessions{Store: db, TTL: 24 * time.Hour, Secure: true, Now: func() time.Time { return *e.now }}

	var verifier auth.IDTokenVerifier = o.verifier
	if verifier == nil && !o.noVerify {
		verifier = fakeVerifier{claims: map[string]auth.Claims{
			"tok-rina": {Sub: "sub-rina", Email: "rina@x.com", EmailVerified: true, Name: "Rina", Picture: "http://p/r"},
			"tok-budi": {Sub: "sub-budi", Email: "budi@x.com", EmailVerified: true, Name: "Budi"},
		}}
	}
	limits := DefaultLimits()
	if o.limits != nil {
		limits = *o.limits
	}
	e.h = NewHandler(Deps{
		Ping:    db.Ping,
		Version: "t",
		Auth:    &AuthDeps{Sessions: e.sessions, Verifier: verifier, Users: db, PublicOrigin: testOrigin, Limits: limits, Data: dsync.NewEngine(db), ReadDB: db.Read},
	})
	return e
}

type reqOpt func(*http.Request)

func withCookie(c *http.Cookie) reqOpt { return func(r *http.Request) { r.AddCookie(c) } }
func withHeader(k, v string) reqOpt    { return func(r *http.Request) { r.Header.Set(k, v) } }
func withoutHeader(k string) reqOpt    { return func(r *http.Request) { r.Header.Del(k) } }

// do sends a same-origin JSON request by default (what the real frontend sends).
func (e *env) do(method, path, body string, opts ...reqOpt) *httptest.ResponseRecorder {
	var rd *strings.Reader
	if body != "" {
		rd = strings.NewReader(body)
	} else {
		rd = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, rd)
	req.RemoteAddr = "203.0.113.5:4000"
	if method != http.MethodGet {
		req.Header.Set("Origin", testOrigin)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	for _, o := range opts {
		o(req)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func (e *env) login(cred string) (*httptest.ResponseRecorder, *http.Cookie) {
	rec := e.do(http.MethodPost, "/api/v1/auth/google", fmt.Sprintf(`{"credential":%q}`, cred))
	return rec, sessionCookie(rec)
}

func sessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if strings.HasSuffix(c.Name, "dietsis_session") {
			return c
		}
	}
	return nil
}

func errCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var b errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("not an error envelope: %q", rec.Body.String())
	}
	return b.Error.Code
}

func TestLoginCreatesAccountAndSecureSessionCookie(t *testing.T) {
	e := newEnv(t, envOpts{})
	rec, c := e.login("tok-rina")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if c == nil || c.Name != "__Host-dietsis_session" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.Domain != "" {
		t.Fatalf("bad session cookie: %+v", c)
	}
	var body struct {
		Data sessionJSON `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.User.Email != "rina@x.com" || body.Data.User.ID == "" || body.Data.HasProfile || body.Data.DataEpoch != 1 {
		t.Fatalf("unexpected body: %+v", body.Data)
	}
	if strings.Contains(rec.Body.String(), c.Value) {
		t.Fatal("session token must never appear in a response body")
	}
}

func TestMeReturnsUserAndRequiresSession(t *testing.T) {
	e := newEnv(t, envOpts{})
	if rec := e.do(http.MethodGet, "/api/v1/auth/me", ""); rec.Code != http.StatusUnauthorized || errCode(t, rec) != "unauthenticated" {
		t.Fatalf("anonymous /me = %d", rec.Code)
	}
	_, c := e.login("tok-rina")
	rec := e.do(http.MethodGet, "/api/v1/auth/me", "", withCookie(c))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "rina@x.com") {
		t.Fatalf("/me = %d %s", rec.Code, rec.Body)
	}
}

func TestMeReportsHasProfile(t *testing.T) {
	e := newEnv(t, envOpts{})
	_, c := e.login("tok-rina")
	var uid string
	_ = e.db.Read.QueryRow(`SELECT id FROM users`).Scan(&uid)
	if _, err := e.db.Write.Exec(`INSERT INTO profiles (user_id, display_name, gender, age, height_cm, weight_kg, goal_weight_kg, activity_level, calorie_target, protein_target_g, water_target_glasses, created_at, updated_at)
		VALUES (?, 'R', 'female', 30, 160, 70, 60, 'light', 1500, 90, 8, 1, 1)`, uid); err != nil {
		t.Fatal(err)
	}
	rec := e.do(http.MethodGet, "/api/v1/auth/me", "", withCookie(c))
	if !strings.Contains(rec.Body.String(), `"has_profile":true`) {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestLoginFailuresAreGenericAndSetNoCookie(t *testing.T) {
	e := newEnv(t, envOpts{})
	rec, c := e.login("forged-token")
	if rec.Code != http.StatusUnauthorized || errCode(t, rec) != "unauthenticated" || c != nil {
		t.Fatalf("status %d cookie %v", rec.Code, c)
	}
	if strings.Contains(rec.Body.String(), "secret-detail") {
		t.Fatalf("verification reason leaked to client: %s", rec.Body)
	}
}

func TestLoginWhenGoogleKeysUnavailableIs503NotAnAuthFailure(t *testing.T) {
	e := newEnv(t, envOpts{verifier: fakeVerifier{err: errors.New("jwks fetch: connection refused")}})
	rec, _ := e.login("anything")
	if rec.Code != http.StatusServiceUnavailable || strings.Contains(rec.Body.String(), "refused") {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
}

func TestLoginWithoutConfiguredGoogleIs503(t *testing.T) {
	e := newEnv(t, envOpts{noVerify: true})
	rec, _ := e.login("x")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestLoginRequestValidation(t *testing.T) {
	cases := map[string]struct {
		body string
		ct   string
		want int
	}{
		"empty body":         {"", "application/json", http.StatusBadRequest},
		"empty credential":   {`{"credential":""}`, "application/json", http.StatusBadRequest},
		"unknown field":      {`{"credential":"tok-rina","user_id":"admin"}`, "application/json", http.StatusBadRequest},
		"trailing data":      {`{"credential":"tok-rina"}{"x":1}`, "application/json", http.StatusBadRequest},
		"malformed":          {`{"credential":`, "application/json", http.StatusBadRequest},
		"wrong content type": {`{"credential":"tok-rina"}`, "text/plain", http.StatusUnsupportedMediaType},
		"form encoded":       {`credential=tok-rina`, "application/x-www-form-urlencoded", http.StatusUnsupportedMediaType},
		"oversized":          {`{"credential":"` + strings.Repeat("a", 70<<10) + `"}`, "application/json", http.StatusRequestEntityTooLarge},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, envOpts{}) // fresh limiter budget per case
			rec := e.do(http.MethodPost, "/api/v1/auth/google", c.body, withHeader("Content-Type", c.ct))
			if rec.Code != c.want {
				t.Fatalf("status %d, want %d (%s)", rec.Code, c.want, rec.Body)
			}
			if sessionCookie(rec) != nil {
				t.Fatal("no cookie may be set on failure")
			}
		})
	}
}

func TestCSRFRules(t *testing.T) {
	e := newEnv(t, envOpts{})
	_, c := e.login("tok-rina")
	logout := func(opts ...reqOpt) *httptest.ResponseRecorder {
		return e.do(http.MethodPost, "/api/v1/auth/logout", "", append([]reqOpt{withCookie(c)}, opts...)...)
	}
	cases := []struct {
		name string
		opts []reqOpt
		want int
	}{
		{"foreign origin", []reqOpt{withHeader("Origin", "https://evil.example")}, http.StatusForbidden},
		{"origin with different port", []reqOpt{withHeader("Origin", testOrigin+":8443")}, http.StatusForbidden},
		{"origin null (sandboxed iframe)", []reqOpt{withHeader("Origin", "null")}, http.StatusForbidden},
		{"no origin, cross-site fetch metadata", []reqOpt{withoutHeader("Origin"), withHeader("Sec-Fetch-Site", "cross-site")}, http.StatusForbidden},
		{"no origin, same-site (sibling subdomain)", []reqOpt{withoutHeader("Origin"), withHeader("Sec-Fetch-Site", "same-site")}, http.StatusForbidden},
		{"no origin and no fetch metadata", []reqOpt{withoutHeader("Origin")}, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rec := logout(tc.opts...); rec.Code != tc.want || errCode(t, rec) != "csrf_failed" {
				t.Fatalf("status %d: %s", rec.Code, rec.Body)
			}
		})
	}
	// None of the rejected attempts may have revoked the session.
	if rec := e.do(http.MethodGet, "/api/v1/auth/me", "", withCookie(c)); rec.Code != http.StatusOK {
		t.Fatalf("session was affected by blocked CSRF attempts: %d", rec.Code)
	}
	// Legit: no Origin but same-origin fetch metadata (e.g. some same-origin navigations).
	if rec := logout(withoutHeader("Origin"), withHeader("Sec-Fetch-Site", "same-origin")); rec.Code != http.StatusNoContent {
		t.Fatalf("same-origin fetch metadata should pass, got %d", rec.Code)
	}
}

func TestCSRFAppliesToLoginToo(t *testing.T) {
	e := newEnv(t, envOpts{})
	rec := e.do(http.MethodPost, "/api/v1/auth/google", `{"credential":"tok-rina"}`, withHeader("Origin", "https://evil.example"))
	if rec.Code != http.StatusForbidden || sessionCookie(rec) != nil {
		t.Fatalf("login CSRF must be blocked: %d", rec.Code)
	}
}

func TestLogoutRevokesSessionAndClearsCookie(t *testing.T) {
	e := newEnv(t, envOpts{})
	_, c := e.login("tok-rina")
	rec := e.do(http.MethodPost, "/api/v1/auth/logout", "", withCookie(c))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d", rec.Code)
	}
	cleared := sessionCookie(rec)
	if cleared == nil || cleared.MaxAge >= 0 || cleared.Value != "" {
		t.Fatalf("cookie not cleared: %+v", cleared)
	}
	// The old token is dead server-side: replaying it fails (session revocation, not just cookie removal).
	if rec := e.do(http.MethodGet, "/api/v1/auth/me", "", withCookie(c)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token still works: %d", rec.Code)
	}
}

func TestLogoutIsIdempotentWithoutSession(t *testing.T) {
	e := newEnv(t, envOpts{})
	if rec := e.do(http.MethodPost, "/api/v1/auth/logout", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestLogoutAllDevicesRevokesOtherSessionsButNotOtherUsers(t *testing.T) {
	e := newEnv(t, envOpts{})
	_, phone := e.login("tok-rina")
	_, laptop := e.login("tok-rina")
	_, budi := e.login("tok-budi")
	if phone.Value == laptop.Value {
		t.Fatal("each login must get its own token")
	}
	rec := e.do(http.MethodPost, "/api/v1/auth/logout", `{"all_devices":true}`, withCookie(phone))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d", rec.Code)
	}
	for name, c := range map[string]*http.Cookie{"phone": phone, "laptop": laptop} {
		if rec := e.do(http.MethodGet, "/api/v1/auth/me", "", withCookie(c)); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s session survived logout-all: %d", name, rec.Code)
		}
	}
	if rec := e.do(http.MethodGet, "/api/v1/auth/me", "", withCookie(budi)); rec.Code != http.StatusOK {
		t.Fatalf("another user's session must be untouched: %d", rec.Code)
	}
}

func TestSessionsAreIsolatedPerUser(t *testing.T) {
	e := newEnv(t, envOpts{})
	_, rina := e.login("tok-rina")
	_, budi := e.login("tok-budi")
	for c, want := range map[*http.Cookie]string{rina: "rina@x.com", budi: "budi@x.com"} {
		rec := e.do(http.MethodGet, "/api/v1/auth/me", "", withCookie(c))
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("session returned the wrong user: %s", rec.Body)
		}
	}
}

func TestRepeatedLoginSameGoogleAccountKeepsOneUser(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.login("tok-rina")
	e.login("tok-rina")
	var n int
	_ = e.db.Read.QueryRow(`SELECT count(*) FROM users`).Scan(&n)
	if n != 1 {
		t.Fatalf("users = %d", n)
	}
}

func TestLoginReplacesTheBrowsersPreviousSession(t *testing.T) {
	e := newEnv(t, envOpts{})
	_, old := e.login("tok-rina")
	rec := e.do(http.MethodPost, "/api/v1/auth/google", `{"credential":"tok-rina"}`, withCookie(old))
	fresh := sessionCookie(rec)
	if fresh == nil || fresh.Value == old.Value {
		t.Fatal("login must issue a new token (no fixation)")
	}
	if rec := e.do(http.MethodGet, "/api/v1/auth/me", "", withCookie(old)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("previous token should be revoked, got %d", rec.Code)
	}
}

func TestExpiredSessionIsRejectedAndCookieCleared(t *testing.T) {
	e := newEnv(t, envOpts{})
	_, c := e.login("tok-rina")
	*e.now = e.now.Add(25 * time.Hour) // TTL is 24h
	rec := e.do(http.MethodGet, "/api/v1/auth/me", "", withCookie(c))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", rec.Code)
	}
	if cl := sessionCookie(rec); cl == nil || cl.MaxAge >= 0 {
		t.Fatal("stale cookie should be cleared")
	}
}

func TestSessionCookieIsRefreshedWhenSliding(t *testing.T) {
	e := newEnv(t, envOpts{})
	_, c := e.login("tok-rina")
	*e.now = e.now.Add(13 * time.Hour) // past half of the 24h TTL
	rec := e.do(http.MethodGet, "/api/v1/auth/me", "", withCookie(c))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if r := sessionCookie(rec); r == nil || r.Value != c.Value || r.MaxAge < 23*3600 {
		t.Fatalf("expected refreshed cookie with a full TTL, got %+v", r)
	}
}

func TestGarbageCookiesAreRejected(t *testing.T) {
	e := newEnv(t, envOpts{})
	for _, v := range []string{"", "x", strings.Repeat("A", 43), strings.Repeat("A", 500), "'; DROP TABLE sessions;--"} {
		rec := e.do(http.MethodGet, "/api/v1/auth/me", "", withCookie(&http.Cookie{Name: "__Host-dietsis_session", Value: v}))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("cookie %q: status %d", v, rec.Code)
		}
	}
	var n int
	_ = e.db.Read.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='sessions'`).Scan(&n)
	if n != 1 {
		t.Fatal("sessions table must still exist")
	}
}

func TestLoginRateLimitPerIP(t *testing.T) {
	l := DefaultLimits()
	l.login = limit.New(60, 3)
	e := newEnv(t, envOpts{limits: &l})
	for i := range 3 {
		if rec, _ := e.login("forged"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status %d", i+1, rec.Code)
		}
	}
	rec, _ := e.login("forged")
	if rec.Code != http.StatusTooManyRequests || errCode(t, rec) != "rate_limited" || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("status %d retry-after %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	// A different client IP is not affected.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/google", bytes.NewReader([]byte(`{"credential":"tok-rina"}`)))
	req.RemoteAddr = "198.51.100.77:1"
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("Content-Type", "application/json")
	out := httptest.NewRecorder()
	e.h.ServeHTTP(out, req)
	if out.Code != http.StatusOK {
		t.Fatalf("other IP should still log in, got %d", out.Code)
	}
}

func TestSessionGuessingIsThrottledPerIP(t *testing.T) {
	l := DefaultLimits()
	l.authFail = limit.New(60, 2)
	e := newEnv(t, envOpts{limits: &l})
	for range 2 {
		if rec := e.do(http.MethodGet, "/api/v1/auth/me", ""); rec.Code != http.StatusUnauthorized {
			t.Fatalf("status %d", rec.Code)
		}
	}
	if rec := e.do(http.MethodGet, "/api/v1/auth/me", ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("repeated failed auth should be throttled, got %d", rec.Code)
	}
}

func TestAuthenticatedUserRateLimitIsPerUser(t *testing.T) {
	l := DefaultLimits()
	l.user = limit.New(60, 2)
	e := newEnv(t, envOpts{limits: &l})
	_, rina := e.login("tok-rina")
	_, budi := e.login("tok-budi")
	for range 2 {
		if rec := e.do(http.MethodGet, "/api/v1/auth/me", "", withCookie(rina)); rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
	}
	if rec := e.do(http.MethodGet, "/api/v1/auth/me", "", withCookie(rina)); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("rina should be limited, got %d", rec.Code)
	}
	if rec := e.do(http.MethodGet, "/api/v1/auth/me", "", withCookie(budi)); rec.Code != http.StatusOK {
		t.Fatalf("budi must not share rina's budget, got %d", rec.Code)
	}
}

func TestSpoofedForwardedForDoesNotBypassIPLimit(t *testing.T) {
	l := DefaultLimits()
	l.login = limit.New(60, 2)
	e := newEnv(t, envOpts{limits: &l})
	for i := range 4 {
		e.login("forged")
		_ = i
	}
	// Same TCP peer (untrusted), rotating X-Forwarded-For: must still be limited.
	rec := e.do(http.MethodPost, "/api/v1/auth/google", `{"credential":"forged"}`, withHeader("X-Forwarded-For", "9.9.9.9"))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("X-Forwarded-For from an untrusted peer must be ignored, got %d", rec.Code)
	}
}

func TestHealthRemainsPublicAndAPIErrorsCarrySecurityHeaders(t *testing.T) {
	e := newEnv(t, envOpts{})
	rec := e.do(http.MethodGet, "/api/v1/health", "")
	if rec.Code != http.StatusOK || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("status %d headers %v", rec.Code, rec.Header())
	}
}
