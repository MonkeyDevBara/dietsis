package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/MonkeyDevBara/dietsis/server/internal/store"
)

// ErrNoSession means the request has no valid session (missing, unknown, expired or revoked).
var ErrNoSession = errors.New("no valid session")

// SessionStore is the persistence the manager needs; *store.DB implements it.
type SessionStore interface {
	CreateSession(ctx context.Context, tokenHash []byte, userID string, now, expires time.Time, userAgent string) error
	LookupSession(ctx context.Context, tokenHash []byte, now time.Time) (store.User, store.Session, error)
	ExtendSession(ctx context.Context, tokenHash []byte, now, expires time.Time) error
	DeleteSession(ctx context.Context, tokenHash []byte) error
	DeleteUserSessions(ctx context.Context, userID string) (int64, error)
}

const (
	tokenBytes = 32
	// Raw token is base64url(32 bytes) = 43 chars; anything else cannot be ours.
	tokenLen = 43

	// "__Host-" pins the cookie to this exact host over HTTPS: no Domain, Path=/, Secure required.
	secureCookieName = "__Host-dietsis_session"
	devCookieName    = "dietsis_session"
)

// Sessions issues, validates and revokes opaque session tokens (docs/SECURITY.md → Sesi).
// The cookie holds a random token; the database holds only its SHA-256, so a leaked database or
// backup does not yield usable tokens.
type Sessions struct {
	Store  SessionStore
	TTL    time.Duration
	Secure bool
	Now    func() time.Time
}

func (s *Sessions) CookieName() string {
	if s.Secure {
		return secureCookieName
	}
	return devCookieName
}

func hashToken(raw string) []byte {
	h := sha256.Sum256([]byte(raw))
	return h[:]
}

// Create starts a new session with a fresh token. A token is never reused across logins, so a
// session id planted before login (fixation) is worthless.
func (s *Sessions) Create(ctx context.Context, userID, userAgent string) (token string, expires time.Time, err error) {
	var b [tokenBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", time.Time{}, err
	}
	token = base64.RawURLEncoding.EncodeToString(b[:])
	now := s.Now()
	expires = now.Add(s.TTL)
	if err := s.Store.CreateSession(ctx, hashToken(token), userID, now, expires, userAgent); err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

// Authenticate resolves a raw cookie token to its user. When more than half of the TTL has elapsed
// the session slides forward; the new expiry is returned so the caller can refresh the cookie.
func (s *Sessions) Authenticate(ctx context.Context, token string) (user store.User, refreshed time.Time, err error) {
	if len(token) != tokenLen {
		return store.User{}, time.Time{}, ErrNoSession
	}
	hash := hashToken(token)
	now := s.Now()
	user, sess, err := s.Store.LookupSession(ctx, hash, now)
	if errors.Is(err, store.ErrNotFound) {
		return store.User{}, time.Time{}, ErrNoSession
	}
	if err != nil {
		return store.User{}, time.Time{}, err
	}
	if sess.ExpiresAt.Sub(now) < s.TTL/2 {
		next := now.Add(s.TTL)
		if err := s.Store.ExtendSession(ctx, hash, now, next); err != nil {
			return store.User{}, time.Time{}, err
		}
		return user, next, nil
	}
	return user, time.Time{}, nil
}

func (s *Sessions) Revoke(ctx context.Context, token string) error {
	if len(token) != tokenLen {
		return nil
	}
	return s.Store.DeleteSession(ctx, hashToken(token))
}

func (s *Sessions) RevokeAll(ctx context.Context, userID string) error {
	_, err := s.Store.DeleteUserSessions(ctx, userID)
	return err
}

// Token returns the session token from the request cookie.
func (s *Sessions) Token(r *http.Request) string {
	c, err := r.Cookie(s.CookieName())
	if err != nil {
		return ""
	}
	return c.Value
}

func (s *Sessions) SetCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.CookieName(),
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(expires.Sub(s.Now()).Seconds()),
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *Sessions) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.CookieName(),
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: http.SameSiteStrictMode,
	})
}
