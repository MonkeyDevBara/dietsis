package sync

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/MonkeyDevBara/dietsis/server/internal/store"
)

type Engine struct {
	db  *store.DB
	now func() time.Time
}

func NewEngine(db *store.DB) *Engine { return &Engine{db: db, now: time.Now} }

// EpochMismatchError means the client's data_epoch is stale: the user deleted all data from another
// device, so this client must reset instead of pushing (docs/DECISIONS.md D-016).
type EpochMismatchError struct{ Current int64 }

func (e *EpochMismatchError) Error() string {
	return fmt.Sprintf("data epoch mismatch: server is at %d", e.Current)
}

var opIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ValidOpID reports whether s may be used as an op id / Idempotency-Key.
func ValidOpID(s string) bool { return opIDRe.MatchString(s) }

// opCtx is what an op handler works with: one open write transaction for one user.
type opCtx struct {
	ctx   context.Context
	tx    *sql.Tx
	uid   string
	now   time.Time
	nowMs int64
}

type handler func(c *opCtx, raw json.RawMessage) (Result, error)

func (e *Engine) handlers() map[string]handler {
	return map[string]handler{
		OpProfilePut:       e.profilePut,
		OpFavoriteSet:      e.favoriteSet,
		OpDayWater:         e.dayWater,
		OpDayUpdate:        e.dayUpdate,
		OpFoodLogCreate:    e.foodLogCreate,
		OpFoodLogUpdate:    e.foodLogUpdate,
		OpFoodLogDelete:    e.foodLogDelete,
		OpFoodLogRestore:   e.foodLogRestore,
		OpCustomFoodCreate: e.customFoodCreate,
		OpWeightPut:        e.weightPut,
		OpWeightDelete:     e.weightDelete,
	}
}

func rejected(code, msg string, details map[string]string) Result {
	return Result{Status: StatusRejected, Error: &ErrorInfo{Code: code, Message: msg, Details: details}}
}

// Apply runs one op for user uid in a single BEGIN IMMEDIATE transaction:
//
//	epoch check → idempotency ledger lookup → apply → change_log → ledger insert → COMMIT
//
// The effect and its ledger row commit together, so a retry after a lost response replays the
// stored result and never applies twice; a crash before COMMIT leaves nothing behind.
// expectEpoch (sync push only) makes the op fail with *EpochMismatchError when stale.
// A non-nil error means an internal failure (the transaction was rolled back).
func (e *Engine) Apply(ctx context.Context, uid string, op Op, expectEpoch *int64) (Result, error) {
	if op.ID != "" && !ValidOpID(op.ID) {
		r := rejected(CodeBadRequest, "op_id tidak valid.", nil)
		r.OpID = op.ID
		return r, nil
	}
	h, known := e.handlers()[op.Type]

	var res Result
	err := e.db.WithTx(ctx, func(tx *sql.Tx) error {
		now := e.now()
		c := &opCtx{ctx: ctx, tx: tx, uid: uid, now: now, nowMs: now.UnixMilli()}

		var epoch int64
		if err := tx.QueryRowContext(ctx, `SELECT data_epoch FROM users WHERE id = ?`, uid).Scan(&epoch); err != nil {
			return fmt.Errorf("read epoch: %w", err)
		}
		if expectEpoch != nil && *expectEpoch != epoch {
			return &EpochMismatchError{Current: epoch}
		}

		if op.ID != "" {
			replay, found, err := e.ledgerLookup(c, op)
			if err != nil {
				return err
			}
			if found {
				res = replay
				return nil
			}
		}

		var err error
		switch {
		case !known:
			res = rejected(CodeBadRequest, "Jenis operasi tidak dikenal.", map[string]string{"type": op.Type})
		default:
			res, err = h(c, op.Payload)
			if err != nil {
				return err
			}
		}
		res.OpID = op.ID

		if op.ID != "" {
			return e.ledgerInsert(c, op, res)
		}
		return nil
	})
	if err != nil {
		var em *EpochMismatchError
		if errors.As(err, &em) {
			return Result{}, err
		}
		return Result{}, fmt.Errorf("apply %s: %w", op.Type, err)
	}
	return res, nil
}

func (e *Engine) ledgerLookup(c *opCtx, op Op) (Result, bool, error) {
	var storedType, blob string
	err := c.tx.QueryRowContext(c.ctx, `SELECT op_type, result FROM sync_operations WHERE user_id = ? AND op_id = ?`, c.uid, op.ID).Scan(&storedType, &blob)
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, fmt.Errorf("ledger lookup: %w", err)
	}
	if storedType != op.Type {
		// The same key was used for a different operation: never answer with someone else's result.
		r := rejected(CodeBadRequest, "Kunci idempotensi ini sudah dipakai untuk operasi lain.", nil)
		r.OpID = op.ID
		return r, true, nil
	}
	var r Result
	if err := json.Unmarshal([]byte(blob), &r); err != nil {
		return Result{}, false, fmt.Errorf("decode ledger result: %w", err)
	}
	r.OpID, r.Replayed = op.ID, true
	return r, true, nil
}

func (e *Engine) ledgerInsert(c *opCtx, op Op, res Result) error {
	blob, err := json.Marshal(res)
	if err != nil {
		return err
	}
	_, err = c.tx.ExecContext(c.ctx,
		`INSERT INTO sync_operations (user_id, op_id, op_type, status, result, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		c.uid, op.ID, op.Type, res.Status, string(blob), c.nowMs)
	if err != nil {
		return fmt.Errorf("ledger insert: %w", err)
	}
	return nil
}

// Marshal builds an Op from a typed payload.
func Marshal(opID, typ string, payload any) (Op, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return Op{}, err
	}
	return Op{ID: opID, Type: typ, Payload: b}, nil
}

// strictDecode decodes exactly one JSON object, rejecting unknown fields and trailing data.
func strictDecode(raw json.RawMessage, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if _, err := dec.Token(); err == nil {
		return errors.New("trailing data")
	}
	return nil
}

func badPayload() Result {
	return rejected(CodeBadRequest, "Payload operasi tidak valid.", nil)
}

func invalidFields(fields map[string]string) Result {
	return rejected(CodeValidation, "Data tidak valid.", fields)
}

// touch records that an entity changed. The previous row for the same key is removed so the
// table keeps one row per entity and any older cursor stays valid (docs/DATABASE.md).
func (c *opCtx) touch(entity, key string) error {
	if _, err := c.tx.ExecContext(c.ctx, `DELETE FROM change_log WHERE user_id = ? AND entity = ? AND entity_key = ?`, c.uid, entity, key); err != nil {
		return fmt.Errorf("change_log delete: %w", err)
	}
	if _, err := c.tx.ExecContext(c.ctx, `INSERT INTO change_log (user_id, entity, entity_key, changed_at) VALUES (?, ?, ?, ?)`, c.uid, entity, key, c.nowMs); err != nil {
		return fmt.Errorf("change_log insert: %w", err)
	}
	return nil
}

// result builds an applied/conflict result carrying the entity's current state.
func (c *opCtx) result(status, entity, key string) (Result, error) {
	st, err := loadState(c.ctx, c.tx, c.uid, entity, key)
	if err != nil {
		return Result{}, err
	}
	return Result{Status: status, Entity: entity, Key: key, State: st}, nil
}

// stale reports whether a client's base_version no longer matches the server. A nil base means
// "write unconditionally"; base 0 means "I believe it does not exist yet".
func stale(base *int64, exists bool, current int64) bool {
	if base == nil {
		return false
	}
	if !exists {
		return *base != 0
	}
	return *base != current
}
