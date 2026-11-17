package httpx

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/MonkeyDevBara/dietsis/server/internal/auth"
	dsync "github.com/MonkeyDevBara/dietsis/server/internal/sync"
)

// Deps carries everything the handlers need. Grows as milestones add auth and the sync engine.
type Deps struct {
	Version string
	// Ping checks that the database is reachable (store.DB.Ping). Required.
	Ping func(context.Context) error
	// TrustedProxies lists the peers allowed to set X-Forwarded-For (Nginx on loopback).
	TrustedProxies []netip.Prefix
	// Auth enables /auth/* and CSRF protection; nil registers only health (used by narrow tests).
	Auth *AuthDeps
}

// AuthDeps wires authentication. Verifier may be nil when Google login is not configured
// (development without a client id): /auth/google then answers 503.
type AuthDeps struct {
	Sessions     *auth.Sessions
	Verifier     auth.IDTokenVerifier
	Users        userStore
	PublicOrigin string
	Limits       Limits
	// Data enables the business API (profile, food logs, sync, ...). ReadDB is the read pool used
	// by CSV export. Both nil registers only the auth routes.
	Data   *dsync.Engine
	ReadDB *sql.DB
}

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// router registers routes with the default body limit; handleLimit overrides it (sync push, M4).
type router struct{ mux *http.ServeMux }

func (rt router) handle(pattern string, h http.HandlerFunc) {
	rt.handleLimit(pattern, DefaultBodyLimit, h)
}

func (rt router) handleLimit(pattern string, max int64, h http.HandlerFunc) {
	rt.mux.Handle(pattern, limitBody(max, h))
}

// NewHandler builds the API router wrapped in the middleware chain. Patterns use Go 1.22+
// method/wildcard syntax, so no router dependency is needed.
func NewHandler(d Deps) http.Handler {
	if d.Ping == nil {
		panic("httpx: Deps.Ping is required")
	}
	rt := router{mux: http.NewServeMux()}
	general := func(h http.Handler) http.Handler { return h }
	if d.Auth != nil {
		general = func(h http.Handler) http.Handler { return rateLimitIP(d.Auth.Limits.general, h) }
	}

	healthHandler := func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := d.Ping(ctx); err != nil {
			slog.Error("health check failed", "request_id", RequestID(r.Context()), "err", err)
			WriteError(w, http.StatusServiceUnavailable, ErrorBody{Code: "unavailable", Message: "Layanan belum siap."})
			return
		}
		WriteData(w, http.StatusOK, healthResponse{Status: "ok", Version: d.Version})
	}
	rt.mux.Handle("GET /api/v1/health", limitBody(DefaultBodyLimit, general(http.HandlerFunc(healthHandler))))

	rt.handle("/api/", func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotFound, ErrorBody{Code: "not_found", Message: "Endpoint tidak ditemukan."})
	})

	var root http.Handler = rt.mux
	if d.Auth != nil {
		kit := &authKit{sessions: d.Auth.Sessions, verifier: d.Auth.Verifier, users: d.Auth.Users, limits: d.Auth.Limits, origin: d.Auth.PublicOrigin}
		rt.mux.Handle("POST /api/v1/auth/google", limitBody(DefaultBodyLimit, rateLimitIP(kit.limits.login, http.HandlerFunc(kit.login))))
		rt.mux.Handle("GET /api/v1/auth/me", kit.authenticate(http.HandlerFunc(kit.me)))
		rt.mux.Handle("POST /api/v1/auth/logout", limitBody(DefaultBodyLimit, general(http.HandlerFunc(kit.logout))))
		if d.Auth.Data != nil {
			registerAPI(rt.mux, kit, d.Auth.Data, d.Auth.ReadDB)
		}
		root = csrfGuard(kit.origin, rt.mux)
	}

	return chain(root, d.TrustedProxies)
}
