package httpx

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/MonkeyDevBara/dietsis/server/internal/auth"
	"github.com/MonkeyDevBara/dietsis/server/internal/limit"
	"github.com/MonkeyDevBara/dietsis/server/internal/store"
)

// ---- rate limiting ----

func tooManyRequests(w http.ResponseWriter, retry time.Duration) {
	secs := max(1, int(math.Ceil(retry.Seconds())))
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	WriteError(w, http.StatusTooManyRequests, ErrorBody{Code: "rate_limited", Message: "Terlalu banyak permintaan. Coba lagi sebentar."})
}

// rateLimitIP limits by resolved client IP (see clientIPMiddleware).
func rateLimitIP(l *limit.Limiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, retry := l.Allow(ClientIP(r.Context())); !ok {
			tooManyRequests(w, retry)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---- CSRF ----

// csrfGuard protects state-changing requests (docs/DECISIONS.md D-007). Cookie sessions are
// SameSite=Strict already; this adds two independent checks:
//
//  1. Origin must equal the app origin; when the browser sends no Origin, Sec-Fetch-Site must be
//     same-origin. Requests with neither are refused: every browser sends one of them on POST.
//  2. A request body must be application/json, which a cross-site HTML form cannot produce without
//     a CORS preflight (and this API never allows one).
//
// Safe methods are never state-changing in this API and pass through.
func csrfGuard(publicOrigin string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			if origin != publicOrigin {
				csrfFailed(w)
				return
			}
		} else if r.Header.Get("Sec-Fetch-Site") != "same-origin" {
			csrfFailed(w)
			return
		}
		if r.ContentLength != 0 && !isJSON(r) {
			WriteError(w, http.StatusUnsupportedMediaType, ErrorBody{Code: "unsupported_media_type", Message: "Gunakan Content-Type: application/json."})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func csrfFailed(w http.ResponseWriter) {
	WriteError(w, http.StatusForbidden, ErrorBody{Code: "csrf_failed", Message: "Permintaan ditolak."})
}

// ---- authentication ----

const userKey ctxKey = 100

// CurrentUser returns the authenticated user. Identity comes only from the verified session, never
// from request bodies, query strings or paths.
func CurrentUser(ctx context.Context) (store.User, bool) {
	u, ok := ctx.Value(userKey).(store.User)
	return u, ok
}

func unauthenticated(w http.ResponseWriter) {
	WriteError(w, http.StatusUnauthorized, ErrorBody{Code: "unauthenticated", Message: "Silakan masuk terlebih dahulu."})
}

// authenticate resolves the session cookie. On a missing or invalid session it answers 401 (and
// clears a stale cookie); repeated failures from one IP are throttled to blunt token guessing.
func (a *authKit) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := a.sessions.Token(r)
		user, refreshed, err := a.sessions.Authenticate(r.Context(), token)
		switch {
		case err == nil:
		case errors.Is(err, auth.ErrNoSession):
			if ok, retry := a.limits.authFail.Allow(ClientIP(r.Context())); !ok {
				tooManyRequests(w, retry)
				return
			}
			if token != "" {
				a.sessions.ClearCookie(w)
			}
			unauthenticated(w)
			return
		default:
			slog.Error("session lookup failed", "request_id", RequestID(r.Context()), "err", err)
			internalError(w)
			return
		}
		if !refreshed.IsZero() {
			a.sessions.SetCookie(w, token, refreshed)
		}
		if ok, retry := a.limits.user.Allow(user.ID); !ok {
			tooManyRequests(w, retry)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, user)))
	})
}

func internalError(w http.ResponseWriter) {
	WriteError(w, http.StatusInternalServerError, ErrorBody{Code: "internal", Message: "Terjadi kesalahan pada server. Coba lagi nanti."})
}

// Limits groups the rate limiters (docs/SECURITY.md → Rate limiting). Zero value is not usable;
// use DefaultLimits.
type Limits struct {
	authFail *limit.Limiter // failed session checks, per IP
	login    *limit.Limiter // POST /auth/google, per IP
	general  *limit.Limiter // unauthenticated endpoints, per IP
	user     *limit.Limiter // authenticated endpoints, per user
	push     *limit.Limiter // POST /sync/push, per user (M4)
}

func DefaultLimits() Limits {
	return Limits{
		authFail: limit.New(30, 10),
		login:    limit.New(10, 5),
		general:  limit.New(60, 20),
		user:     limit.New(300, 60),
		push:     limit.New(60, 10),
	}
}

// NewLimits builds custom limiters (tests use tiny ones).
func NewLimits(authFail, login, general, user, push *limit.Limiter) Limits {
	return Limits{authFail: authFail, login: login, general: general, user: user, push: push}
}

type authKit struct {
	sessions *auth.Sessions
	verifier auth.IDTokenVerifier // nil when Google login is not configured
	users    userStore
	limits   Limits
	origin   string
}

type userStore interface {
	UpsertGoogleUser(ctx context.Context, sub, email, name, picture string, now time.Time) (store.User, error)
	UserHasProfile(ctx context.Context, userID string) (bool, error)
}
