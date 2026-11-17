package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func okPing(context.Context) error { return nil }

func newTestHandler(t *testing.T, d Deps) http.Handler {
	t.Helper()
	if d.Ping == nil {
		d.Ping = okPing
	}
	return NewHandler(d)
}

func get(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestHealthOK(t *testing.T) {
	rec := get(newTestHandler(t, Deps{Version: "test"}), http.MethodGet, "/api/v1/health")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Data healthResponse `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Status != "ok" || body.Data.Version != "test" {
		t.Fatalf("body = %+v", body.Data)
	}
}

func TestHealthReports503WhenDatabaseDown(t *testing.T) {
	h := newTestHandler(t, Deps{Ping: func(context.Context) error { return errors.New("disk I/O error: /secret/path") }})
	rec := get(h, http.MethodGet, "/api/v1/health")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("internal error detail leaked to client: %s", rec.Body.String())
	}
	var body errorEnvelope
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body.Error.Code != "unavailable" {
		t.Fatalf("want unavailable error, got %v (%v)", body, err)
	}
}

func TestUnknownAPIRouteReturnsJSONError(t *testing.T) {
	rec := get(newTestHandler(t, Deps{}), http.MethodGet, "/api/v1/nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	var body errorEnvelope
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body.Error.Code != "not_found" {
		t.Fatalf("want JSON not_found, got %q (%v)", rec.Body.String(), err)
	}
}

func TestEveryResponseCarriesSecurityHeadersAndRequestID(t *testing.T) {
	h := newTestHandler(t, Deps{})
	seen := map[string]bool{}
	for _, path := range []string{"/api/v1/health", "/api/v1/nope"} {
		rec := get(h, http.MethodGet, path)
		want := map[string]string{
			"X-Content-Type-Options":  "nosniff",
			"X-Frame-Options":         "DENY",
			"Referrer-Policy":         "same-origin",
			"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
			"Cache-Control":           "no-store",
		}
		for k, v := range want {
			if got := rec.Header().Get(k); got != v {
				t.Errorf("%s %s = %q, want %q", path, k, got, v)
			}
		}
		id := rec.Header().Get("X-Request-ID")
		if len(id) != 16 || seen[id] {
			t.Errorf("%s: request id %q must be 16 hex chars and unique", path, id)
		}
		seen[id] = true
	}
}

func TestInboundRequestIDIsIgnored(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.Header.Set("X-Request-ID", "evil\nfake-log-line")
	rec := httptest.NewRecorder()
	newTestHandler(t, Deps{}).ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Request-ID"); strings.Contains(got, "evil") {
		t.Fatalf("inbound request id was echoed: %q", got)
	}
}

func TestPanicBecomesGenericJSON500AndIsLogged(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	h := recoverMiddleware(requestIDMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("database password is hunter2")
	})))
	rec := get(h, http.MethodGet, "/x")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatalf("panic value leaked to client: %s", rec.Body.String())
	}
	if !strings.Contains(logs.String(), "hunter2") || !strings.Contains(logs.String(), "request_id=") {
		t.Fatalf("panic must be logged server-side with the request id: %s", logs.String())
	}
}

func TestAccessLogRecordsRoutePatternNotRawPathOrQuery(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	get(newTestHandler(t, Deps{}), http.MethodGet, "/api/v1/health?token=s3cret")
	out := logs.String()
	if !strings.Contains(out, `route="GET /api/v1/health"`) || !strings.Contains(out, "status=200") {
		t.Fatalf("missing route/status in access log: %s", out)
	}
	if strings.Contains(out, "s3cret") || strings.Contains(out, "token=") {
		t.Fatalf("query string leaked into log: %s", out)
	}
}

func TestBodyLimitRejectsOversizedReads(t *testing.T) {
	var readErr error
	h := limitBody(10, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("a", 11)))
	h.ServeHTTP(httptest.NewRecorder(), req)
	var tooBig *http.MaxBytesError
	if !errors.As(readErr, &tooBig) {
		t.Fatalf("want *http.MaxBytesError, got %v", readErr)
	}

	readErr = nil
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader("0123456789")))
	if readErr != nil {
		t.Fatalf("body exactly at the limit must be accepted, got %v", readErr)
	}
}

func TestResolveClientIP(t *testing.T) {
	loopback := []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32"), netip.MustParsePrefix("::1/128")}
	cases := []struct {
		name   string
		remote string
		xff    []string
		want   string
	}{
		{"direct client, no proxy header", "203.0.113.9:5000", nil, "203.0.113.9"},
		{"untrusted peer cannot spoof via header", "203.0.113.9:5000", []string{"1.2.3.4"}, "203.0.113.9"},
		{"trusted proxy passes real client", "127.0.0.1:41000", []string{"198.51.100.7"}, "198.51.100.7"},
		{"client-prepended entries are ignored (rightmost untrusted wins)", "127.0.0.1:41000", []string{"6.6.6.6, 198.51.100.7"}, "198.51.100.7"},
		{"multiple header lines", "127.0.0.1:41000", []string{"6.6.6.6", "198.51.100.7"}, "198.51.100.7"},
		{"trusted hops are skipped", "127.0.0.1:41000", []string{"198.51.100.7, 127.0.0.1"}, "198.51.100.7"},
		{"trusted proxy without header", "127.0.0.1:41000", nil, "127.0.0.1"},
		{"malformed header falls back to proxy", "127.0.0.1:41000", []string{"not-an-ip"}, "127.0.0.1"},
		{"ipv6 peer", "[::1]:41000", []string{"2001:db8::5"}, "2001:db8::5"},
		{"ipv4-mapped ipv6 peer is unmapped", "[::ffff:127.0.0.1]:41000", []string{"198.51.100.7"}, "198.51.100.7"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveClientIP(c.remote, c.xff, loopback); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestClientIPAvailableToHandlers(t *testing.T) {
	var got string
	h := clientIPMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = ClientIP(r.Context()) }),
		[]netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if got != "198.51.100.7" {
		t.Fatalf("ClientIP = %q", got)
	}
}
