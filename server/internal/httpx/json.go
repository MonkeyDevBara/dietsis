// Package httpx contains the HTTP layer: routing, middleware, JSON envelopes and handlers.
package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// ErrorBody is the single error shape returned by every endpoint (see docs/API.md).
type ErrorBody struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Details map[string]string `json:"details,omitempty"`
	// Current is the server's state of the entity on a 409 conflict (JSON null when it does not exist).
	Current json.RawMessage `json:"current,omitempty"`
}

type errorEnvelope struct {
	Error ErrorBody `json:"error"`
}

type dataEnvelope struct {
	Data any `json:"data"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("write json response", "err", err)
	}
}

// WriteData writes a success response wrapped as {"data": ...}.
func WriteData(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, dataEnvelope{Data: data})
}

// WriteError writes {"error": {...}}. Messages must be safe to show to end users;
// internal details belong in server logs only.
func WriteError(w http.ResponseWriter, status int, body ErrorBody) {
	writeJSON(w, status, errorEnvelope{Error: body})
}

type listEnvelope struct {
	Data       any     `json:"data"`
	NextCursor *string `json:"next_cursor"`
}

// WriteList writes a paginated list: {"data": [...], "next_cursor": "..."|null}.
func WriteList(w http.ResponseWriter, data any, nextCursor string) {
	env := listEnvelope{Data: data}
	if nextCursor != "" {
		env.NextCursor = &nextCursor
	}
	writeJSON(w, http.StatusOK, env)
}
