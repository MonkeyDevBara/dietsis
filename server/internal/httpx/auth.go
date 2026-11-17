package httpx

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/MonkeyDevBara/dietsis/server/internal/auth"
	"github.com/MonkeyDevBara/dietsis/server/internal/store"
)

type userJSON struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Picture   string `json:"picture"`
	CreatedAt int64  `json:"created_at"`
}

type sessionJSON struct {
	User       userJSON `json:"user"`
	HasProfile bool     `json:"has_profile"`
	DataEpoch  int64    `json:"data_epoch"`
}

func (a *authKit) sessionResponse(w http.ResponseWriter, r *http.Request, u store.User) {
	has, err := a.users.UserHasProfile(r.Context(), u.ID)
	if err != nil {
		slog.Error("profile lookup failed", "request_id", RequestID(r.Context()), "err", err)
		internalError(w)
		return
	}
	WriteData(w, http.StatusOK, sessionJSON{
		User:       userJSON{ID: u.ID, Email: u.Email, Name: u.Name, Picture: u.Picture, CreatedAt: u.CreatedAt},
		HasProfile: has,
		DataEpoch:  u.DataEpoch,
	})
}

// POST /auth/google: exchange a Google ID token for a session cookie.
func (a *authKit) login(w http.ResponseWriter, r *http.Request) {
	if a.verifier == nil {
		WriteError(w, http.StatusServiceUnavailable, ErrorBody{Code: "unavailable", Message: "Login Google belum dikonfigurasi di server."})
		return
	}
	var body struct {
		Credential string `json:"credential"`
	}
	if !decodeJSON(w, r, &body, false) {
		return
	}
	if body.Credential == "" {
		WriteError(w, http.StatusBadRequest, ErrorBody{Code: "bad_request", Message: "Isi permintaan tidak valid."})
		return
	}

	claims, err := a.verifier.Verify(r.Context(), body.Credential)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidToken) {
			// The reason stays in the log; the client only learns that login failed.
			slog.Warn("login rejected", "request_id", RequestID(r.Context()), "ip", ClientIP(r.Context()), "reason", err.Error())
			unauthenticatedLogin(w)
			return
		}
		slog.Error("login verification unavailable", "request_id", RequestID(r.Context()), "err", err)
		WriteError(w, http.StatusServiceUnavailable, ErrorBody{Code: "unavailable", Message: "Verifikasi Google sedang tidak tersedia. Coba lagi nanti."})
		return
	}

	user, err := a.users.UpsertGoogleUser(r.Context(), claims.Sub, claims.Email, claims.Name, claims.Picture, time.Now())
	if err != nil {
		slog.Error("upsert user failed", "request_id", RequestID(r.Context()), "err", err)
		internalError(w)
		return
	}
	// Drop any session this browser already had, then issue a fresh token (never reuse one).
	if old := a.sessions.Token(r); old != "" {
		_ = a.sessions.Revoke(r.Context(), old)
	}
	token, expires, err := a.sessions.Create(r.Context(), user.ID, r.UserAgent())
	if err != nil {
		slog.Error("create session failed", "request_id", RequestID(r.Context()), "err", err)
		internalError(w)
		return
	}
	a.sessions.SetCookie(w, token, expires)
	a.sessionResponse(w, r, user)
}

func unauthenticatedLogin(w http.ResponseWriter) {
	WriteError(w, http.StatusUnauthorized, ErrorBody{Code: "unauthenticated", Message: "Login gagal. Coba masuk lagi."})
}

// GET /auth/me: who am I (also slides the session).
func (a *authKit) me(w http.ResponseWriter, r *http.Request) {
	u, _ := CurrentUser(r.Context())
	a.sessionResponse(w, r, u)
}

// POST /auth/logout: always 204 and always clears the cookie, even without a valid session.
func (a *authKit) logout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AllDevices bool `json:"all_devices"`
	}
	if !decodeJSON(w, r, &body, true) {
		return
	}
	if token := a.sessions.Token(r); token != "" {
		if body.AllDevices {
			if u, _, err := a.sessions.Authenticate(r.Context(), token); err == nil {
				if err := a.sessions.RevokeAll(r.Context(), u.ID); err != nil {
					slog.Error("revoke all sessions failed", "request_id", RequestID(r.Context()), "err", err)
					internalError(w)
					return
				}
			}
		}
		if err := a.sessions.Revoke(r.Context(), token); err != nil {
			slog.Error("revoke session failed", "request_id", RequestID(r.Context()), "err", err)
			internalError(w)
			return
		}
	}
	a.sessions.ClearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}
