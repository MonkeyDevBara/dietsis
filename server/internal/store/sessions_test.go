package store

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

var t0 = time.Unix(1_700_000_000, 0)

func TestUpsertGoogleUserCreatesThenUpdatesBySub(t *testing.T) {
	db := migrated(t)
	ctx := context.Background()

	a, err := db.UpsertGoogleUser(ctx, "sub-1", "old@x.com", "Rina", "p1", t0)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "" || a.DataEpoch != 1 || a.CreatedAt != t0.UnixMilli() {
		t.Fatalf("unexpected new user %+v", a)
	}
	// Email changed at Google: same account, refreshed fields, created_at preserved.
	b, err := db.UpsertGoogleUser(ctx, "sub-1", "new@x.com", "Rina S", "p2", t0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if b.ID != a.ID || b.Email != "new@x.com" || b.Name != "Rina S" || b.CreatedAt != a.CreatedAt {
		t.Fatalf("upsert should update in place: %+v vs %+v", a, b)
	}
	// Same email but different sub is a different person.
	c, err := db.UpsertGoogleUser(ctx, "sub-2", "new@x.com", "Other", "", t0)
	if err != nil {
		t.Fatal(err)
	}
	if c.ID == a.ID {
		t.Fatal("different sub must create a different user")
	}
	if n := scalar[int](t, db.Read, `SELECT count(*) FROM users`); n != 2 {
		t.Fatalf("users = %d", n)
	}
}

func TestUserHasProfile(t *testing.T) {
	db := migrated(t)
	ctx := context.Background()
	u, _ := db.UpsertGoogleUser(ctx, "s", "a@x", "A", "", t0)
	if has, err := db.UserHasProfile(ctx, u.ID); err != nil || has {
		t.Fatalf("has = %v, err = %v; want false", has, err)
	}
	mustExec(t, db, `INSERT INTO profiles (user_id, display_name, gender, age, height_cm, weight_kg, goal_weight_kg, activity_level, calorie_target, protein_target_g, water_target_glasses, created_at, updated_at)
		VALUES (?, 'A', 'female', 30, 160, 70, 60, 'light', 1500, 90, 8, 1, 1)`, u.ID)
	if has, err := db.UserHasProfile(ctx, u.ID); err != nil || !has {
		t.Fatalf("has = %v, err = %v; want true", has, err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	db := migrated(t)
	ctx := context.Background()
	u, _ := db.UpsertGoogleUser(ctx, "s", "a@x", "A", "", t0)
	hash := bytes.Repeat([]byte{1}, 32)

	if err := db.CreateSession(ctx, hash, u.ID, t0, t0.Add(time.Hour), "UA"); err != nil {
		t.Fatal(err)
	}
	got, sess, err := db.LookupSession(ctx, hash, t0.Add(time.Minute))
	if err != nil || got.ID != u.ID || got.Email != "a@x" {
		t.Fatalf("lookup = %+v, %v", got, err)
	}
	if !sess.ExpiresAt.Equal(t0.Add(time.Hour).Truncate(time.Millisecond)) {
		t.Fatalf("expiry = %v", sess.ExpiresAt)
	}

	// Expired exactly at expires_at.
	if _, _, err := db.LookupSession(ctx, hash, t0.Add(time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired session must be ErrNotFound, got %v", err)
	}
	// Unknown token.
	if _, _, err := db.LookupSession(ctx, bytes.Repeat([]byte{9}, 32), t0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown token must be ErrNotFound, got %v", err)
	}

	// Sliding extension only moves forward.
	if err := db.ExtendSession(ctx, hash, t0.Add(30*time.Minute), t0.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := db.ExtendSession(ctx, hash, t0.Add(31*time.Minute), t0.Add(90*time.Minute)); err != nil {
		t.Fatal(err)
	}
	_, sess, _ = db.LookupSession(ctx, hash, t0.Add(time.Hour+time.Minute))
	if !sess.ExpiresAt.Equal(t0.Add(2 * time.Hour)) {
		t.Fatalf("expiry must not move backward, got %v", sess.ExpiresAt)
	}

	if err := db.DeleteSession(ctx, hash); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.LookupSession(ctx, hash, t0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked session must be gone, got %v", err)
	}
}

func TestRevokeAllAndPurge(t *testing.T) {
	db := migrated(t)
	ctx := context.Background()
	u1, _ := db.UpsertGoogleUser(ctx, "s1", "a@x", "A", "", t0)
	u2, _ := db.UpsertGoogleUser(ctx, "s2", "b@x", "B", "", t0)
	h := func(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }
	for _, s := range []struct {
		hash byte
		user string
		exp  time.Duration
	}{{1, u1.ID, time.Hour}, {2, u1.ID, time.Hour}, {3, u2.ID, time.Hour}, {4, u2.ID, -time.Minute}} {
		if err := db.CreateSession(ctx, h(s.hash), s.user, t0, t0.Add(s.exp), ""); err != nil {
			t.Fatal(err)
		}
	}

	n, err := db.DeleteUserSessions(ctx, u1.ID)
	if err != nil || n != 2 {
		t.Fatalf("revoked %d (%v), want 2", n, err)
	}
	if _, _, err := db.LookupSession(ctx, h(3), t0); err != nil {
		t.Fatalf("other user's session must survive: %v", err)
	}

	n, err = db.PurgeExpiredSessions(ctx, t0)
	if err != nil || n != 1 {
		t.Fatalf("purged %d (%v), want 1", n, err)
	}
}

func TestDeletingUserCascadesSessions(t *testing.T) {
	db := migrated(t)
	ctx := context.Background()
	u, _ := db.UpsertGoogleUser(ctx, "s", "a@x", "A", "", t0)
	_ = db.CreateSession(ctx, bytes.Repeat([]byte{1}, 32), u.ID, t0, t0.Add(time.Hour), "")
	mustExec(t, db, `DELETE FROM users WHERE id = ?`, u.ID)
	if n := scalar[int](t, db.Read, `SELECT count(*) FROM sessions`); n != 0 {
		t.Fatalf("sessions left after user delete: %d", n)
	}
}

func TestLongUserAgentIsTruncated(t *testing.T) {
	db := migrated(t)
	ctx := context.Background()
	u, _ := db.UpsertGoogleUser(ctx, "s", "a@x", "A", "", t0)
	if err := db.CreateSession(ctx, bytes.Repeat([]byte{1}, 32), u.ID, t0, t0.Add(time.Hour), strings.Repeat("é", 5000)); err != nil {
		t.Fatal(err)
	}
	if n := scalar[int](t, db.Read, `SELECT length(user_agent) FROM sessions`); n != maxUserAgentLen {
		t.Fatalf("stored user agent length = %d chars, want %d", n, maxUserAgentLen)
	}
}
