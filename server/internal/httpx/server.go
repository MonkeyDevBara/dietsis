package httpx

import (
	"net/http"
)

// Deps carries everything the handlers need. Grows as milestones add the store, auth and sync engine.
type Deps struct {
	Version string
}

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// NewHandler builds the API router. Paths use Go 1.22+ method/wildcard patterns, so no router dependency is needed.
func NewHandler(d Deps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		WriteData(w, http.StatusOK, healthResponse{Status: "ok", Version: d.Version})
	})

	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotFound, ErrorBody{Code: "not_found", Message: "Endpoint tidak ditemukan."})
	})

	return mux
}
