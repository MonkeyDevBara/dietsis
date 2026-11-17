package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"time"
)

// Deps carries everything the handlers need. Grows as milestones add auth and the sync engine.
type Deps struct {
	Version string
	// Ping checks that the database is reachable (store.DB.Ping). Required.
	Ping func(context.Context) error
	// TrustedProxies lists the peers allowed to set X-Forwarded-For (Nginx on loopback).
	TrustedProxies []netip.Prefix
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

	rt.handle("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := d.Ping(ctx); err != nil {
			slog.Error("health check failed", "request_id", RequestID(r.Context()), "err", err)
			WriteError(w, http.StatusServiceUnavailable, ErrorBody{Code: "unavailable", Message: "Layanan belum siap."})
			return
		}
		WriteData(w, http.StatusOK, healthResponse{Status: "ok", Version: d.Version})
	})

	rt.handle("/api/", func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotFound, ErrorBody{Code: "not_found", Message: "Endpoint tidak ditemukan."})
	})

	return chain(rt.mux, d.TrustedProxies)
}
