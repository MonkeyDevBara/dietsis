package sync

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrBadCursor means a pagination cursor was not produced by this server (or was tampered with).
var ErrBadCursor = errors.New("invalid cursor")

func encodeCursor(v any) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string, dst any) error {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || json.Unmarshal(b, dst) != nil {
		return ErrBadCursor
	}
	return nil
}

func (e *Engine) Epoch(ctx context.Context, uid string) (int64, error) {
	var epoch int64
	if err := e.db.Read.QueryRowContext(ctx, `SELECT data_epoch FROM users WHERE id = ?`, uid).Scan(&epoch); err != nil {
		return 0, fmt.Errorf("read epoch: %w", err)
	}
	return epoch, nil
}

// ---- single reads ----

func (e *Engine) GetProfile(ctx context.Context, uid string) (*Profile, error) {
	return loadProfile(ctx, e.db.Read, uid)
}

// GetDay returns the stored day or defaults for a day that was never written (version 0).
func (e *Engine) GetDay(ctx context.Context, uid, date string) (Day, error) {
	d, err := loadDay(ctx, e.db.Read, uid, date)
	if err != nil {
		return Day{}, err
	}
	if d == nil {
		return Day{Date: date}, nil
	}
	return *d, nil
}

// ---- lists ----

func (e *Engine) ListFavorites(ctx context.Context, uid string) ([]Favorite, error) {
	return listFavorites(ctx, e.db.Read, uid)
}

func listFavorites(ctx context.Context, q queryer, uid string) ([]Favorite, error) {
	rows, err := q.QueryContext(ctx, `SELECT food_id, created_at FROM favorites WHERE user_id = ? ORDER BY created_at DESC, food_id`, uid)
	if err != nil {
		return nil, fmt.Errorf("list favorites: %w", err)
	}
	defer rows.Close()
	out := []Favorite{}
	for rows.Next() {
		var f Favorite
		if err := rows.Scan(&f.FoodID, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (e *Engine) ListDays(ctx context.Context, uid, from, to string) ([]Day, error) {
	rows, err := e.db.Read.QueryContext(ctx, `SELECT `+dayCols+` FROM days WHERE user_id = ? AND date BETWEEN ? AND ? ORDER BY date`, uid, from, to)
	if err != nil {
		return nil, fmt.Errorf("list days: %w", err)
	}
	defer rows.Close()
	out := []Day{}
	for rows.Next() {
		d, err := scanDay(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

type FoodLogQuery struct {
	From, To       string
	Limit          int // already clamped by the caller
	Cursor         string
	IncludeDeleted bool
}

type foodLogCursor struct {
	Date    string `json:"d"`
	Created int64  `json:"c"`
	ID      string `json:"i"`
}

// ListFoodLogs pages through a date range ordered by (date, created_at, id).
func (e *Engine) ListFoodLogs(ctx context.Context, uid string, q FoodLogQuery) ([]FoodLog, string, error) {
	query := `SELECT ` + foodLogCols + ` FROM food_logs WHERE user_id = ? AND date BETWEEN ? AND ?`
	args := []any{uid, q.From, q.To}
	if !q.IncludeDeleted {
		query += ` AND deleted_at IS NULL`
	}
	if q.Cursor != "" {
		var c foodLogCursor
		if err := decodeCursor(q.Cursor, &c); err != nil {
			return nil, "", err
		}
		query += ` AND (date, created_at, id) > (?, ?, ?)`
		args = append(args, c.Date, c.Created, c.ID)
	}
	query += ` ORDER BY date, created_at, id LIMIT ?`
	args = append(args, q.Limit+1)

	rows, err := e.db.Read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list food logs: %w", err)
	}
	defer rows.Close()
	out := []FoodLog{}
	for rows.Next() {
		l, err := scanFoodLog(rows)
		if err != nil {
			return nil, "", err
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > q.Limit {
		out = out[:q.Limit]
		last := out[len(out)-1]
		next = encodeCursor(foodLogCursor{Date: last.Date, Created: last.CreatedAt, ID: last.ID})
	}
	return out, next, nil
}

type nameCursor struct {
	Name string `json:"n"`
	ID   string `json:"i"`
}

func (e *Engine) ListCustomFoods(ctx context.Context, uid string, limit int, cursor string) ([]CustomFood, string, error) {
	query := `SELECT ` + customFoodCols + ` FROM custom_foods WHERE user_id = ?`
	args := []any{uid}
	if cursor != "" {
		var c nameCursor
		if err := decodeCursor(cursor, &c); err != nil {
			return nil, "", err
		}
		query += ` AND (name, id) > (?, ?)`
		args = append(args, c.Name, c.ID)
	}
	query += ` ORDER BY name, id LIMIT ?`
	args = append(args, limit+1)

	rows, err := e.db.Read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list custom foods: %w", err)
	}
	defer rows.Close()
	out := []CustomFood{}
	for rows.Next() {
		f, err := scanCustomFood(rows)
		if err != nil {
			return nil, "", err
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		next = encodeCursor(nameCursor{Name: last.Name, ID: last.ID})
	}
	return out, next, nil
}

type dateCursor struct {
	Date string `json:"d"`
}

// ListWeights pages by date ascending. from/to are optional ("" = unbounded).
func (e *Engine) ListWeights(ctx context.Context, uid, from, to string, limit int, cursor string) ([]Weight, string, error) {
	query := `SELECT ` + weightCols + ` FROM weights WHERE user_id = ?`
	args := []any{uid}
	if from != "" {
		query += ` AND date >= ?`
		args = append(args, from)
	}
	if to != "" {
		query += ` AND date <= ?`
		args = append(args, to)
	}
	if cursor != "" {
		var c dateCursor
		if err := decodeCursor(cursor, &c); err != nil {
			return nil, "", err
		}
		query += ` AND date > ?`
		args = append(args, c.Date)
	}
	query += ` ORDER BY date LIMIT ?`
	args = append(args, limit+1)

	rows, err := e.db.Read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list weights: %w", err)
	}
	defer rows.Close()
	out := []Weight{}
	for rows.Next() {
		w, err := scanWeight(rows)
		if err != nil {
			return nil, "", err
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = encodeCursor(dateCursor{Date: out[len(out)-1].Date})
	}
	return out, next, nil
}

// ---- pull ----

type Change struct {
	Seq    int64           `json:"seq"`
	Entity string          `json:"entity"`
	Key    string          `json:"key"`
	State  json.RawMessage `json:"state"`
}

type PullResult struct {
	DataEpoch int64    `json:"data_epoch"`
	Changes   []Change `json:"changes"`
	Cursor    int64    `json:"cursor"`
	HasMore   bool     `json:"has_more"`
}

// Pull returns the current state of every entity changed after `since`, oldest change first.
// change_log keeps one row per entity, so a full sync (since=0) is just the final state of
// everything and an old cursor is always valid.
func (e *Engine) Pull(ctx context.Context, uid string, since int64, limit int) (PullResult, error) {
	epoch, err := e.Epoch(ctx, uid)
	if err != nil {
		return PullResult{}, err
	}
	rows, err := e.db.Read.QueryContext(ctx,
		`SELECT seq, entity, entity_key FROM change_log WHERE user_id = ? AND seq > ? ORDER BY seq LIMIT ?`, uid, since, limit+1)
	if err != nil {
		return PullResult{}, fmt.Errorf("pull: %w", err)
	}
	var changes []Change
	for rows.Next() {
		var c Change
		if err := rows.Scan(&c.Seq, &c.Entity, &c.Key); err != nil {
			_ = rows.Close()
			return PullResult{}, err
		}
		changes = append(changes, c)
	}
	if err := rows.Close(); err != nil {
		return PullResult{}, err
	}
	if err := rows.Err(); err != nil {
		return PullResult{}, err
	}

	res := PullResult{DataEpoch: epoch, Cursor: since, Changes: []Change{}}
	if len(changes) > limit {
		changes, res.HasMore = changes[:limit], true
	}
	for i := range changes {
		st, err := loadState(ctx, e.db.Read, uid, changes[i].Entity, changes[i].Key)
		if err != nil {
			return PullResult{}, err
		}
		changes[i].State = st
		res.Cursor = changes[i].Seq
	}
	if changes != nil {
		res.Changes = changes
	}
	return res, nil
}

// ---- favorites replace (PUT /favorites) ----

// ReplaceFavorites makes the favorite set equal to ids in one transaction, logging each change.
func (e *Engine) ReplaceFavorites(ctx context.Context, uid string, ids []string) ([]Favorite, error) {
	var out []Favorite
	err := e.db.WithTx(ctx, func(tx *sql.Tx) error {
		now := e.now()
		c := &opCtx{ctx: ctx, tx: tx, uid: uid, now: now, nowMs: now.UnixMilli()}
		current, err := listFavorites(ctx, tx, uid)
		if err != nil {
			return err
		}
		want := make(map[string]bool, len(ids))
		for _, id := range ids {
			want[id] = true
		}
		have := make(map[string]bool, len(current))
		for _, f := range current {
			have[f.FoodID] = true
			if !want[f.FoodID] {
				if _, err := tx.ExecContext(ctx, `DELETE FROM favorites WHERE user_id = ? AND food_id = ?`, uid, f.FoodID); err != nil {
					return err
				}
				if err := c.touch(EntityFavorite, f.FoodID); err != nil {
					return err
				}
			}
		}
		for _, id := range ids {
			if have[id] {
				continue
			}
			have[id] = true // tolerate duplicates in the request
			if _, err := tx.ExecContext(ctx, `INSERT INTO favorites (user_id, food_id, created_at) VALUES (?, ?, ?)`, uid, id, c.nowMs); err != nil {
				return err
			}
			if err := c.touch(EntityFavorite, id); err != nil {
				return err
			}
		}
		out, err = listFavorites(ctx, tx, uid)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("replace favorites: %w", err)
	}
	return out, nil
}

// ValidFoodID reports whether s is a well-formed favorite/food id.
func ValidFoodID(s string) bool { return foodIDRe.MatchString(s) }

// MaxFavorites is the per-user favorite limit.
const MaxFavorites = maxFavorites

// ---- delete all data ----

// DeleteAllData removes everything the user stored except the account and sessions, and bumps
// data_epoch so devices that were offline reset instead of resurrecting the data (D-016).
func (e *Engine) DeleteAllData(ctx context.Context, uid string) (int64, error) {
	var epoch int64
	err := e.db.WithTx(ctx, func(tx *sql.Tx) error {
		for _, table := range []string{"profiles", "favorites", "days", "food_logs", "custom_foods", "weights", "sync_operations", "change_log"} {
			// Table names come from this fixed list, never from input.
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE user_id = ?`, uid); err != nil {
				return fmt.Errorf("delete %s: %w", table, err)
			}
		}
		res, err := tx.ExecContext(ctx, `UPDATE users SET data_epoch = data_epoch + 1, updated_at = ? WHERE id = ?`, e.now().UnixMilli(), uid)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return sql.ErrNoRows
		}
		return tx.QueryRowContext(ctx, `SELECT data_epoch FROM users WHERE id = ?`, uid).Scan(&epoch)
	})
	if err != nil {
		return 0, fmt.Errorf("delete all data: %w", err)
	}
	return epoch, nil
}

// ---- retention ----

const (
	SoftDeleteRetention = 30 * 24 * time.Hour
	LedgerRetention     = 90 * 24 * time.Hour
)

type PurgeStats struct{ FoodLogs, Operations int64 }

// Purge applies the retention policy (docs/DATABASE.md → Retensi). The change_log row of a purged
// food log stays: pull then reports state null and clients drop their copy.
func (e *Engine) Purge(ctx context.Context, now time.Time) (PurgeStats, error) {
	var st PurgeStats
	err := e.db.WithTx(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, `DELETE FROM food_logs WHERE deleted_at IS NOT NULL AND deleted_at < ?`, now.Add(-SoftDeleteRetention).UnixMilli())
		if err != nil {
			return err
		}
		st.FoodLogs, _ = r.RowsAffected()
		r, err = tx.ExecContext(ctx, `DELETE FROM sync_operations WHERE created_at < ?`, now.Add(-LedgerRetention).UnixMilli())
		if err != nil {
			return err
		}
		st.Operations, _ = r.RowsAffected()
		return nil
	})
	if err != nil {
		return PurgeStats{}, fmt.Errorf("purge: %w", err)
	}
	_, _ = e.db.Write.ExecContext(ctx, `PRAGMA optimize`)
	return st, nil
}
