package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Session is a login. Only the SHA-256 of the cookie token is stored (docs/SECURITY.md).
type Session struct {
	ExpiresAt  time.Time
	LastSeenAt time.Time
}

const maxUserAgentLen = 200

func (d *DB) CreateSession(ctx context.Context, tokenHash []byte, userID string, now, expires time.Time, userAgent string) error {
	if r := []rune(userAgent); len(r) > maxUserAgentLen {
		userAgent = string(r[:maxUserAgentLen])
	}
	_, err := d.Write.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, created_at, last_seen_at, expires_at, user_agent) VALUES (?, ?, ?, ?, ?, ?)`,
		tokenHash, userID, now.UnixMilli(), now.UnixMilli(), expires.UnixMilli(), userAgent)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// LookupSession returns the user and session for a token hash, or ErrNotFound if the session does
// not exist or has expired at `now`.
func (d *DB) LookupSession(ctx context.Context, tokenHash []byte, now time.Time) (User, Session, error) {
	var (
		u         User
		s         Session
		exp, seen int64
	)
	err := d.Read.QueryRowContext(ctx,
		`SELECT u.id, u.google_sub, u.email, u.name, u.picture, u.data_epoch, u.created_at, s.expires_at, s.last_seen_at
		   FROM sessions s JOIN users u ON u.id = s.user_id
		  WHERE s.token_hash = ? AND s.expires_at > ?`, tokenHash, now.UnixMilli()).
		Scan(&u.ID, &u.GoogleSub, &u.Email, &u.Name, &u.Picture, &u.DataEpoch, &u.CreatedAt, &exp, &seen)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, Session{}, ErrNotFound
	}
	if err != nil {
		return User{}, Session{}, fmt.Errorf("lookup session: %w", err)
	}
	s.ExpiresAt, s.LastSeenAt = time.UnixMilli(exp), time.UnixMilli(seen)
	return u, s, nil
}

// ExtendSession slides the expiry forward (never backward).
func (d *DB) ExtendSession(ctx context.Context, tokenHash []byte, now, expires time.Time) error {
	_, err := d.Write.ExecContext(ctx,
		`UPDATE sessions SET last_seen_at = ?, expires_at = max(expires_at, ?) WHERE token_hash = ?`,
		now.UnixMilli(), expires.UnixMilli(), tokenHash)
	if err != nil {
		return fmt.Errorf("extend session: %w", err)
	}
	return nil
}

func (d *DB) DeleteSession(ctx context.Context, tokenHash []byte) error {
	if _, err := d.Write.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteUserSessions revokes every session of a user ("logout on all devices").
func (d *DB) DeleteUserSessions(ctx context.Context, userID string) (int64, error) {
	res, err := d.Write.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	if err != nil {
		return 0, fmt.Errorf("delete user sessions: %w", err)
	}
	return res.RowsAffected()
}

func (d *DB) PurgeExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	res, err := d.Write.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("purge sessions: %w", err)
	}
	return res.RowsAffected()
}
