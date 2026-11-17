package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound is returned when a row does not exist (or is not visible to the caller).
var ErrNotFound = errors.New("store: not found")

type User struct {
	ID        string
	GoogleSub string
	Email     string
	Name      string
	Picture   string
	DataEpoch int64
	CreatedAt int64 // epoch ms
}

// NewID returns a random RFC 4122 v4 UUID (lowercase, canonical form).
func NewID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[:8], h[8:12], h[12:16], h[16:20], h[20:]), nil
}

// UpsertGoogleUser creates the account for a Google `sub` or refreshes its email/name/picture.
// Identity is the stable `sub`; the email can change (docs/DECISIONS.md D-005).
func (d *DB) UpsertGoogleUser(ctx context.Context, sub, email, name, picture string, now time.Time) (User, error) {
	var u User
	err := d.WithTx(ctx, func(tx *sql.Tx) error {
		ms := now.UnixMilli()
		err := tx.QueryRowContext(ctx, `SELECT id, created_at, data_epoch FROM users WHERE google_sub = ?`, sub).Scan(&u.ID, &u.CreatedAt, &u.DataEpoch)
		switch {
		case err == nil:
			_, err = tx.ExecContext(ctx, `UPDATE users SET email = ?, name = ?, picture = ?, updated_at = ? WHERE id = ?`, email, name, picture, ms, u.ID)
			if err != nil {
				return err
			}
		case errors.Is(err, sql.ErrNoRows):
			if u.ID, err = NewID(); err != nil {
				return err
			}
			u.CreatedAt, u.DataEpoch = ms, 1
			if _, err = tx.ExecContext(ctx,
				`INSERT INTO users (id, google_sub, email, name, picture, data_epoch, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
				u.ID, sub, email, name, picture, ms, ms); err != nil {
				return err
			}
		default:
			return err
		}
		u.GoogleSub, u.Email, u.Name, u.Picture = sub, email, name, picture
		return nil
	})
	if err != nil {
		return User{}, fmt.Errorf("upsert user: %w", err)
	}
	return u, nil
}

// UserHasProfile reports whether the user finished onboarding.
func (d *DB) UserHasProfile(ctx context.Context, userID string) (bool, error) {
	var one int
	err := d.Read.QueryRowContext(ctx, `SELECT 1 FROM profiles WHERE user_id = ?`, userID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
