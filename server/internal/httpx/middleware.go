package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"runtime/debug"
	"strings"
	"time"
)

type ctxKey int

const (
	requestIDKey ctxKey = iota
	clientIPKey
)

// RequestID returns the id assigned to the request ("" outside the middleware chain).
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// ClientIP returns the resolved client address ("" outside the middleware chain).
func ClientIP(ctx context.Context) string {
	ip, _ := ctx.Value(clientIPKey).(string)
	return ip
}

// Middleware order (outermost first): recover → request id → client ip → security headers → access log.
// Recover is outermost so a panic anywhere below still produces a JSON 500 and a log line.
func chain(h http.Handler, trusted []netip.Prefix) http.Handler {
	h = accessLog(h)
	h = securityHeaders(h)
	h = clientIPMiddleware(h, trusted)
	h = requestIDMiddleware(h)
	return recoverMiddleware(h)
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always generated here: an inbound X-Request-ID is attacker-controlled and would end up in logs.
		var b [8]byte
		_, _ = rand.Read(b[:])
		id := hex.EncodeToString(b[:])
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

func clientIPMiddleware(next http.Handler, trusted []netip.Prefix) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := resolveClientIP(r.RemoteAddr, r.Header.Values("X-Forwarded-For"), trusted)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey, ip)))
	})
}

// resolveClientIP trusts X-Forwarded-For only when the TCP peer is a trusted proxy. The header is
// walked right to left and the first address that is not itself a trusted proxy wins, so entries a
// client prepended cannot spoof its address. Without a trusted peer the TCP peer address is used.
func resolveClientIP(remoteAddr string, xff []string, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	peer = peer.Unmap()
	if !inPrefixes(peer, trusted) {
		return peer.String()
	}

	var hops []string
	for _, v := range xff {
		hops = append(hops, strings.Split(v, ",")...)
	}
	for i := len(hops) - 1; i >= 0; i-- {
		addr, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return peer.String() // malformed header from a trusted proxy: fall back to the proxy itself
		}
		addr = addr.Unmap()
		if !inPrefixes(addr, trusted) {
			return addr.String()
		}
	}
	return peer.String()
}

func inPrefixes(a netip.Addr, prefixes []netip.Prefix) bool {
	for _, p := range prefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// statusRecorder captures the status and size for the access log. Unwrap keeps
// http.ResponseController features (Flush, deadlines) working.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wrote {
		s.WriteHeader(http.StatusOK)
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// accessLog logs one structured line per request. It records the matched route pattern (not the raw
// path, so ids and dates never fan out log cardinality) and never logs query strings, headers,
// cookies or bodies.
func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		level := slog.LevelInfo
		if rec.status >= 500 {
			level = slog.LevelError
		}
		slog.LogAttrs(r.Context(), level, "request",
			slog.String("request_id", RequestID(r.Context())),
			slog.String("method", r.Method),
			slog.String("route", route),
			slog.Int("status", rec.status),
			slog.Int("bytes", rec.bytes),
			slog.Duration("duration", time.Since(start)),
			slog.String("ip", ClientIP(r.Context())),
		)
	})
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			p := recover()
			if p == nil {
				return
			}
			if p == http.ErrAbortHandler { //nolint:errorlint // sentinel compared by identity per net/http docs
				panic(p)
			}
			slog.Error("panic in handler", "request_id", RequestID(r.Context()), "panic", p, "stack", string(debug.Stack()))
			// Best effort: if the handler already started the response, net/http logs a superfluous WriteHeader.
			WriteError(w, http.StatusInternalServerError, ErrorBody{Code: "internal", Message: "Terjadi kesalahan pada server. Coba lagi nanti."})
		}()
		next.ServeHTTP(w, r)
	})
}

// DefaultBodyLimit applies to every JSON endpoint except those that declare a larger limit.
const DefaultBodyLimit int64 = 64 << 10

// limitBody caps the request body; reads past the cap fail with *http.MaxBytesError, which JSON
// decoding (M4) maps to 413 payload_too_large.
func limitBody(max int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, max)
		next.ServeHTTP(w, r)
	})
}
