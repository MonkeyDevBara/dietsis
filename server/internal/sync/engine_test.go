package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	gosync "sync"
	"testing"
	"time"

	"github.com/MonkeyDevBara/dietsis/server/internal/store"
)

const (
	logA = "11111111-1111-4111-8111-111111111111"
	logB = "22222222-2222-4222-8222-222222222222"
	logC = "33333333-3333-4333-8333-333333333333"
)

var baseTime = time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)

type fixture struct {
	t    *testing.T
	e    *Engine
	db   *store.DB
	a, b string // user ids
	now  time.Time
	seq  int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, db: db, now: baseTime}
	f.e = NewEngine(db)
	f.e.now = func() time.Time { return f.now }
	ua, _ := db.UpsertGoogleUser(ctx, "sa", "a@x.com", "A", "", f.now)
	ub, _ := db.UpsertGoogleUser(ctx, "sb", "b@x.com", "B", "", f.now)
	f.a, f.b = ua.ID, ub.ID
	return f
}

// do applies an op with a fresh op id.
func (f *fixture) do(uid, typ string, payload any) Result {
	f.t.Helper()
	f.seq++
	return f.doID(uid, fmt.Sprintf("op-%d", f.seq), typ, payload)
}

func (f *fixture) doID(uid, opID, typ string, payload any) Result {
	f.t.Helper()
	op, err := Marshal(opID, typ, payload)
	if err != nil {
		f.t.Fatal(err)
	}
	res, err := f.e.Apply(context.Background(), uid, op, nil)
	if err != nil {
		f.t.Fatalf("Apply %s: %v", typ, err)
	}
	return res
}

func state[T any](t *testing.T, r Result) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(r.State, &v); err != nil {
		t.Fatalf("decode state %s: %v", r.State, err)
	}
	return v
}

func (f *fixture) count(query string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.db.Read.QueryRow(query, args...).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func m(kv ...any) map[string]any {
	out := map[string]any{}
	for i := 0; i < len(kv); i += 2 {
		out[kv[i].(string)] = kv[i+1]
	}
	return out
}

func logPayload(id string, extra ...any) map[string]any {
	p := m("id", id, "date", "2026-11-17", "meal", "lunch", "food_id", "b:nasi-putih", "name", "Nasi putih",
		"serving_label", "1 porsi (150 g)", "serving_grams", 150, "quantity", 1.5,
		"kcal", 195, "protein_g", 4, "carbs_g", 42, "fat_g", 0.5)
	for i := 0; i < len(extra); i += 2 {
		p[extra[i].(string)] = extra[i+1]
	}
	return p
}

// ---- water ----

func TestWaterIsAtomicUnderConcurrentDevices(t *testing.T) {
	f := newFixture(t)
	var wg gosync.WaitGroup
	for d := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 10 {
				op, _ := Marshal(fmt.Sprintf("dev%d-op%d", d, i), OpDayWater, m("date", "2026-11-17", "delta", 1))
				if _, err := f.e.Apply(context.Background(), f.a, op, nil); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	day, _ := f.e.GetDay(context.Background(), f.a, "2026-11-17")
	if day.WaterGlasses != 80 {
		t.Fatalf("water = %d, want 80 (no lost updates)", day.WaterGlasses)
	}
	if day.Version != 0 {
		t.Fatalf("water must not bump the day version, got %d", day.Version)
	}
}

func TestWaterRetryIsNotCountedTwice(t *testing.T) {
	f := newFixture(t)
	first := f.doID(f.a, "water-1", OpDayWater, m("date", "2026-11-17", "delta", 1))
	// The response was lost; the client retries the same op, maybe several times.
	for range 3 {
		again := f.doID(f.a, "water-1", OpDayWater, m("date", "2026-11-17", "delta", 1))
		if !again.Replayed || again.Status != StatusApplied {
			t.Fatalf("retry should replay the stored result: %+v", again)
		}
		if string(again.State) != string(first.State) {
			t.Fatalf("replayed state differs: %s vs %s", again.State, first.State)
		}
	}
	day, _ := f.e.GetDay(context.Background(), f.a, "2026-11-17")
	if day.WaterGlasses != 1 {
		t.Fatalf("water = %d after retries, want 1", day.WaterGlasses)
	}
	if n := f.count(`SELECT count(*) FROM sync_operations WHERE user_id = ?`, f.a); n != 1 {
		t.Fatalf("ledger rows = %d, want 1", n)
	}
}

func TestWaterClampsAndNegativeDeltasWork(t *testing.T) {
	f := newFixture(t)
	steps := []struct {
		delta float64
		want  int
	}{{-5, 0}, {3, 3}, {-1, 2}, {-20, 0}, {20, 20}, {20, 40}, {20, 60}, {20, 80}, {20, 100}, {20, 100}, {-1, 99}}
	for _, s := range steps {
		r := f.do(f.a, OpDayWater, m("date", "2026-11-17", "delta", s.delta))
		if got := state[Day](t, r).WaterGlasses; got != s.want {
			t.Fatalf("after delta %v: %d, want %d", s.delta, got, s.want)
		}
	}
}

func TestWaterRejectsBadInput(t *testing.T) {
	f := newFixture(t)
	for name, p := range map[string]map[string]any{
		"zero delta":  m("date", "2026-11-17", "delta", 0),
		"too large":   m("date", "2026-11-17", "delta", 21),
		"fractional":  m("date", "2026-11-17", "delta", 0.5),
		"bad date":    m("date", "2026-02-30", "delta", 1),
		"far future":  m("date", "2030-01-01", "delta", 1),
		"before 2000": m("date", "1999-12-31", "delta", 1),
	} {
		if r := f.do(f.a, OpDayWater, p); r.Status != StatusRejected || r.Error.Code != CodeValidation {
			t.Errorf("%s: %+v", name, r)
		}
	}
	if n := f.count(`SELECT count(*) FROM days`); n != 0 {
		t.Fatalf("rejected ops wrote %d rows", n)
	}
}

// ---- ledger ----

func TestSameOpIDForDifferentOperationIsRefused(t *testing.T) {
	f := newFixture(t)
	f.doID(f.a, "key-1", OpDayWater, m("date", "2026-11-17", "delta", 1))
	r := f.doID(f.a, "key-1", OpWeightPut, m("date", "2026-11-17", "weight_kg", 70))
	if r.Status != StatusRejected {
		t.Fatalf("status = %s", r.Status)
	}
	if n := f.count(`SELECT count(*) FROM weights`); n != 0 {
		t.Fatal("the second operation must not have been applied")
	}
}

func TestOpIDsAreScopedPerUser(t *testing.T) {
	f := newFixture(t)
	f.doID(f.a, "shared", OpDayWater, m("date", "2026-11-17", "delta", 5))
	r := f.doID(f.b, "shared", OpDayWater, m("date", "2026-11-17", "delta", 2))
	if r.Replayed || state[Day](t, r).WaterGlasses != 2 {
		t.Fatalf("user B must not see user A's ledger entry: %+v", r)
	}
}

func TestMalformedOpsAreRejectedWithoutSideEffects(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if r, _ := f.e.Apply(ctx, f.a, Op{ID: "bad id with spaces", Type: OpDayWater, Payload: json.RawMessage(`{}`)}, nil); r.Status != StatusRejected {
		t.Fatalf("bad op id: %+v", r)
	}
	if r := f.do(f.a, "no.such.op", m()); r.Status != StatusRejected {
		t.Fatalf("unknown type: %+v", r)
	}
	if r := f.do(f.a, OpFoodLogCreate, logPayload(logA, "evil_field", 1)); r.Status != StatusRejected || r.Error.Code != CodeBadRequest {
		t.Fatalf("unknown payload field must be rejected: %+v", r)
	}
	if r := f.do(f.a, OpFoodLogCreate, logPayload(logA, "user_id", f.b)); r.Status != StatusRejected {
		t.Fatalf("user_id in payload must be rejected, not trusted: %+v", r)
	}
	if n := f.count(`SELECT count(*) FROM food_logs`); n != 0 {
		t.Fatal("rejected ops wrote data")
	}
}

// ---- food logs ----

func TestFoodLogCreateComputesTotalsAndIsIdempotent(t *testing.T) {
	f := newFixture(t)
	r := f.do(f.a, OpFoodLogCreate, logPayload(logA))
	if r.Status != StatusApplied || !r.Created {
		t.Fatalf("%+v", r)
	}
	l := state[FoodLog](t, r)
	if l.Version != 1 || l.Totals != (Nutrients{Kcal: 293, ProteinG: 6, CarbsG: 63, FatG: 0.8}) || l.Kcal != 195 {
		t.Fatalf("unexpected log %+v", l)
	}
	// Same id again (even with a different op id and different values): nothing changes.
	r2 := f.do(f.a, OpFoodLogCreate, logPayload(logA, "quantity", 9))
	if r2.Created || state[FoodLog](t, r2).Quantity != 1.5 || state[FoodLog](t, r2).Version != 1 {
		t.Fatalf("duplicate create must return the existing entry: %+v", state[FoodLog](t, r2))
	}
	if n := f.count(`SELECT count(*) FROM food_logs`); n != 1 {
		t.Fatalf("rows = %d", n)
	}
}

func TestFoodLogValidation(t *testing.T) {
	f := newFixture(t)
	cases := map[string]map[string]any{
		"bad meal":          logPayload(logA, "meal", "brunch"),
		"quantity too low":  logPayload(logA, "quantity", 0.05),
		"quantity decimals": logPayload(logA, "quantity", 1.234),
		"negative kcal":     logPayload(logA, "kcal", -1),
		"huge kcal":         logPayload(logA, "kcal", 5001),
		"bad uuid":          logPayload("not-a-uuid"),
		"uppercase uuid":    logPayload("AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA"),
		"blank name":        logPayload(logA, "name", "   "),
		"control char":      logPayload(logA, "name", "a\u0000b"),
		"bad date":          logPayload(logA, "date", "2026-13-01"),
		"bad food id":       logPayload(logA, "food_id", "x:evil"),
		"zero grams":        logPayload(logA, "serving_grams", 0),
	}
	for name, p := range cases {
		if r := f.do(f.a, OpFoodLogCreate, p); r.Status != StatusRejected || r.Error.Code != CodeValidation {
			t.Errorf("%s: %+v", name, r)
		}
	}
	if r := f.do(f.a, OpFoodLogCreate, logPayload(logB, "quantity", 1.15)); r.Status != StatusApplied {
		t.Fatalf("1.15 is a valid 2-decimal quantity: %+v", r)
	}
	if r := f.do(f.a, OpFoodLogCreate, logPayload(logC, "food_id", nil, "serving_grams", nil)); r.Status != StatusApplied {
		t.Fatalf("food_id and serving_grams may be null: %+v", r)
	}
}

func TestFoodLogUpdateVersionsAndConflicts(t *testing.T) {
	f := newFixture(t)
	f.do(f.a, OpFoodLogCreate, logPayload(logA))

	// Device 1 and device 2 both start from version 1.
	r1 := f.do(f.a, OpFoodLogUpdate, m("id", logA, "base_version", 1, "patch", m("quantity", 2)))
	if r1.Status != StatusApplied || state[FoodLog](t, r1).Version != 2 || state[FoodLog](t, r1).Totals.Kcal != 390 {
		t.Fatalf("first update: %+v", r1)
	}
	r2 := f.do(f.a, OpFoodLogUpdate, m("id", logA, "base_version", 1, "patch", m("meal", "dinner")))
	if r2.Status != StatusConflict {
		t.Fatalf("second device with a stale base must conflict: %+v", r2)
	}
	cur := state[FoodLog](t, r2)
	if cur.Quantity != 2 || cur.Meal != "lunch" || cur.Version != 2 {
		t.Fatalf("conflict must carry the winning server state, got %+v", cur)
	}
	got, _ := loadFoodLog(context.Background(), f.db.Read, f.a, logA)
	if got.Meal != "lunch" {
		t.Fatal("conflicting patch must not be applied")
	}

	// No base_version means unconditional write.
	r3 := f.do(f.a, OpFoodLogUpdate, m("id", logA, "patch", m("meal", "dinner")))
	if r3.Status != StatusApplied || state[FoodLog](t, r3).Version != 3 {
		t.Fatalf("unconditional update: %+v", r3)
	}
}

func TestFoodLogUpdateValidatesTheMergedResult(t *testing.T) {
	f := newFixture(t)
	f.do(f.a, OpFoodLogCreate, logPayload(logA))
	r := f.do(f.a, OpFoodLogUpdate, m("id", logA, "base_version", 1, "patch", m("quantity", 99)))
	if r.Status != StatusRejected || r.Error.Details["quantity"] == "" {
		t.Fatalf("%+v", r)
	}
	if got, _ := loadFoodLog(context.Background(), f.db.Read, f.a, logA); got.Version != 1 || got.Quantity != 1.5 {
		t.Fatal("rejected update changed the row")
	}
	if r := f.do(f.a, OpFoodLogUpdate, m("id", logA, "patch", m())); r.Status != StatusRejected {
		t.Fatalf("empty patch must be rejected: %+v", r)
	}
	// serving_grams can be explicitly cleared with null.
	r = f.do(f.a, OpFoodLogUpdate, m("id", logA, "patch", m("serving_grams", nil)))
	if r.Status != StatusApplied || state[FoodLog](t, r).ServingGrams != nil {
		t.Fatalf("null serving_grams: %+v", r)
	}
}

func TestFoodLogDeleteRestoreSemantics(t *testing.T) {
	f := newFixture(t)
	f.do(f.a, OpFoodLogCreate, logPayload(logA))

	d := f.do(f.a, OpFoodLogDelete, m("id", logA))
	l := state[FoodLog](t, d)
	if d.Status != StatusApplied || l.DeletedAt == nil || l.Version != 2 {
		t.Fatalf("delete: %+v", l)
	}
	if again := f.do(f.a, OpFoodLogDelete, m("id", logA)); state[FoodLog](t, again).Version != 2 {
		t.Fatal("deleting twice must be a no-op (no version bump)")
	}
	// Editing a deleted entry is a conflict: the delete wins until restored.
	if r := f.do(f.a, OpFoodLogUpdate, m("id", logA, "patch", m("quantity", 3))); r.Status != StatusConflict || state[FoodLog](t, r).DeletedAt == nil {
		t.Fatalf("update of deleted entry: %+v", r)
	}
	rs := f.do(f.a, OpFoodLogRestore, m("id", logA))
	if got := state[FoodLog](t, rs); got.DeletedAt != nil || got.Version != 3 {
		t.Fatalf("restore: %+v", got)
	}
	if again := f.do(f.a, OpFoodLogRestore, m("id", logA)); state[FoodLog](t, again).Version != 3 {
		t.Fatal("restoring an active entry must be a no-op")
	}
}

func TestMissingFoodLogResults(t *testing.T) {
	f := newFixture(t)
	if r := f.do(f.a, OpFoodLogDelete, m("id", logA)); r.Status != StatusApplied || string(r.State) != "null" {
		t.Fatalf("delete missing: %+v", r)
	}
	if r := f.do(f.a, OpFoodLogRestore, m("id", logA)); r.Status != StatusApplied || string(r.State) != "null" {
		t.Fatalf("restore missing: %+v", r)
	}
	if r := f.do(f.a, OpFoodLogUpdate, m("id", logA, "patch", m("quantity", 2))); r.Status != StatusConflict || string(r.State) != "null" {
		t.Fatalf("update missing: %+v", r)
	}
	if n := f.count(`SELECT count(*) FROM change_log`); n != 0 {
		t.Fatalf("no-op results must not touch the change log, got %d rows", n)
	}
}

// ---- authorization / IDOR ----

func TestUserCannotTouchAnotherUsersData(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.do(f.a, OpFoodLogCreate, logPayload(logA))
	f.do(f.a, OpWeightPut, m("date", "2026-11-17", "weight_kg", 70))
	f.do(f.a, OpCustomFoodCreate, m("id", logB, "name", "Smoothie", "serving_label", "1 gelas", "kcal", 180, "protein_g", 6, "carbs_g", 30, "fat_g", 3))

	// B attacks A's food log id through every op type.
	if r := f.do(f.b, OpFoodLogUpdate, m("id", logA, "patch", m("quantity", 50))); r.Status != StatusConflict || string(r.State) != "null" {
		t.Fatalf("update of A's log by B must look like 'not found': %+v", r)
	}
	if r := f.do(f.b, OpFoodLogDelete, m("id", logA)); string(r.State) != "null" {
		t.Fatalf("delete of A's log by B: %+v", r)
	}
	f.do(f.b, OpFoodLogRestore, m("id", logA))
	f.do(f.b, OpWeightDelete, m("date", "2026-11-17"))

	got, _ := loadFoodLog(ctx, f.db.Read, f.a, logA)
	if got == nil || got.Quantity != 1.5 || got.DeletedAt != nil || got.Version != 1 {
		t.Fatalf("A's food log was modified by B: %+v", got)
	}
	if w, _ := loadWeight(ctx, f.db.Read, f.a, "2026-11-17"); w == nil {
		t.Fatal("A's weight was deleted by B")
	}

	// B reusing A's id creates B's own independent entry (PK is (user_id, id)).
	rb := f.do(f.b, OpFoodLogCreate, logPayload(logA, "name", "Punya B"))
	if !rb.Created || state[FoodLog](t, rb).Name != "Punya B" {
		t.Fatalf("%+v", rb)
	}
	if got, _ := loadFoodLog(ctx, f.db.Read, f.a, logA); got.Name != "Nasi putih" {
		t.Fatal("A's entry was overwritten")
	}

	// Reads are scoped too.
	logsB, _, _ := f.e.ListFoodLogs(ctx, f.b, FoodLogQuery{From: "2026-11-01", To: "2026-11-30", Limit: 100})
	if len(logsB) != 1 || logsB[0].Name != "Punya B" {
		t.Fatalf("B's list leaks A's data: %+v", logsB)
	}
	if foods, _, _ := f.e.ListCustomFoods(ctx, f.b, 100, ""); len(foods) != 0 {
		t.Fatal("B sees A's custom foods")
	}
	if ws, _, _ := f.e.ListWeights(ctx, f.b, "", "", 100, ""); len(ws) != 0 {
		t.Fatal("B sees A's weights")
	}
	pull, _ := f.e.Pull(ctx, f.b, 0, 100)
	for _, c := range pull.Changes {
		if c.Entity == EntityWeight || c.Entity == EntityCustomFood {
			t.Fatalf("B's pull contains A's %s", c.Entity)
		}
	}
}

// ---- weights ----

func TestWeightOnePerDateWithVersions(t *testing.T) {
	f := newFixture(t)
	r1 := f.do(f.a, OpWeightPut, m("date", "2026-11-17", "weight_kg", 72.4, "waist_cm", 80, "base_version", 0))
	if r1.Status != StatusApplied || !r1.Created || state[Weight](t, r1).Version != 1 {
		t.Fatalf("%+v", r1)
	}
	// Another device also thinks the date is empty: conflict, with the server's entry.
	r2 := f.do(f.a, OpWeightPut, m("date", "2026-11-17", "weight_kg", 71, "base_version", 0))
	if r2.Status != StatusConflict || state[Weight](t, r2).WeightKG != 72.4 {
		t.Fatalf("%+v", r2)
	}
	r3 := f.do(f.a, OpWeightPut, m("date", "2026-11-17", "weight_kg", 71.9, "waist_cm", nil, "base_version", 1))
	if w := state[Weight](t, r3); r3.Status != StatusApplied || w.Version != 2 || w.WaistCM != nil {
		t.Fatalf("%+v", w)
	}
	if n := f.count(`SELECT count(*) FROM weights WHERE user_id = ?`, f.a); n != 1 {
		t.Fatalf("one entry per date, got %d", n)
	}
	// A different user may have the same date.
	if r := f.do(f.b, OpWeightPut, m("date", "2026-11-17", "weight_kg", 55)); r.Status != StatusApplied {
		t.Fatalf("%+v", r)
	}
	// Base version on a missing entry (other than 0) is a conflict.
	if r := f.do(f.a, OpWeightPut, m("date", "2026-11-10", "weight_kg", 70, "base_version", 3)); r.Status != StatusConflict || string(r.State) != "null" {
		t.Fatalf("%+v", r)
	}
}

func TestWeightValidationAndDelete(t *testing.T) {
	f := newFixture(t)
	for name, p := range map[string]map[string]any{
		"too light": m("date", "2026-11-17", "weight_kg", 29.9),
		"too heavy": m("date", "2026-11-17", "weight_kg", 300.1),
		"bad waist": m("date", "2026-11-17", "weight_kg", 70, "waist_cm", 20),
	} {
		if r := f.do(f.a, OpWeightPut, p); r.Status != StatusRejected {
			t.Errorf("%s: %+v", name, r)
		}
	}
	f.do(f.a, OpWeightPut, m("date", "2026-11-17", "weight_kg", 70))
	if r := f.do(f.a, OpWeightDelete, m("date", "2026-11-17", "base_version", 9)); r.Status != StatusConflict {
		t.Fatalf("stale delete: %+v", r)
	}
	if r := f.do(f.a, OpWeightDelete, m("date", "2026-11-17", "base_version", 1)); r.Status != StatusApplied || string(r.State) != "null" {
		t.Fatalf("delete: %+v", r)
	}
	if r := f.do(f.a, OpWeightDelete, m("date", "2026-11-17")); r.Status != StatusApplied {
		t.Fatalf("deleting a missing entry is idempotent: %+v", r)
	}
}

// ---- profile ----

func profilePayload(extra ...any) map[string]any {
	p := m("display_name", "  Rina  ", "gender", "female", "age", 30, "height_cm", 160, "weight_kg", 70, "goal_weight_kg", 60,
		"activity_level", "moderate", "calorie_target", 1, "protein_target_g", 1, "water_target_glasses", 1,
		"calorie_target_manual", false, "protein_target_manual", false, "water_target_manual", false)
	for i := 0; i < len(extra); i += 2 {
		p[extra[i].(string)] = extra[i+1]
	}
	return p
}

func TestProfileTargetsFollowFormulaUnlessManual(t *testing.T) {
	f := newFixture(t)
	r := f.do(f.a, OpProfilePut, profilePayload())
	p := state[Profile](t, r)
	// Shared vector: female 30y 160cm 70→60kg moderate = 1650 / 96 / 9. Client-sent values (1) are ignored.
	if r.Status != StatusApplied || p.DisplayName != "Rina" || p.CalorieTarget != 1650 || p.ProteinTargetG != 96 || p.WaterTargetGlasses != 9 || p.Version != 1 {
		t.Fatalf("%+v (%s)", p, r.State)
	}
	if p.Recommended != (Targets{CalorieTarget: 1650, ProteinTargetG: 96, WaterTargetGlasses: 9}) {
		t.Fatalf("recommended = %+v", p.Recommended)
	}

	r = f.do(f.a, OpProfilePut, profilePayload("calorie_target", 1900, "calorie_target_manual", true, "weight_kg", 68, "base_version", 1))
	p = state[Profile](t, r)
	if p.CalorieTarget != 1900 || !p.CalorieTargetManual || p.ProteinTargetG != 96 || p.WaterTargetGlasses != 9 || p.Version != 2 {
		t.Fatalf("manual calorie target must be kept, the others follow the formula: %+v", p)
	}
	// Recommended keeps tracking the body data even when the target is manual.
	if p.Recommended.CalorieTarget == 1900 {
		t.Fatal("recommended must come from the formula, not the manual value")
	}
}

func TestProfileValidationConflictAndMissingBase(t *testing.T) {
	f := newFixture(t)
	for name, p := range map[string]map[string]any{
		"too young":          profilePayload("age", 17),
		"bad gender":         profilePayload("gender", "other"),
		"bad activity":       profilePayload("activity_level", "extreme"),
		"short":              profilePayload("height_cm", 99),
		"manual below range": profilePayload("calorie_target_manual", true, "calorie_target", 799),
		"manual water zero":  profilePayload("water_target_manual", true, "water_target_glasses", 0),
		"blank name":         profilePayload("display_name", "  "),
	} {
		if r := f.do(f.a, OpProfilePut, p); r.Status != StatusRejected || r.Error.Code != CodeValidation {
			t.Errorf("%s: %+v", name, r)
		}
	}
	if r := f.do(f.a, OpProfilePut, profilePayload("base_version", 4)); r.Status != StatusConflict || string(r.State) != "null" {
		t.Fatalf("base_version on a missing profile: %+v", r)
	}
	f.do(f.a, OpProfilePut, profilePayload())
	if r := f.do(f.a, OpProfilePut, profilePayload("age", 31, "base_version", 7)); r.Status != StatusConflict || state[Profile](t, r).Age != 30 {
		t.Fatalf("stale profile write: %+v", r)
	}
	// Profiles are per user.
	if r := f.do(f.b, OpProfilePut, profilePayload("display_name", "Budi")); !r.Created || state[Profile](t, r).Version != 1 {
		t.Fatalf("%+v", r)
	}
}

// ---- favorites ----

func TestFavorites(t *testing.T) {
	f := newFixture(t)
	if r := f.do(f.a, OpFavoriteSet, m("food_id", "b:pisang", "favorite", true)); r.Status != StatusApplied || state[Favorite](t, r).FoodID != "b:pisang" {
		t.Fatalf("%+v", r)
	}
	before := f.count(`SELECT count(*) FROM change_log`)
	f.do(f.a, OpFavoriteSet, m("food_id", "b:pisang", "favorite", true)) // already a favorite
	if f.count(`SELECT count(*) FROM change_log`) != before || f.count(`SELECT max(seq) FROM change_log`) != 1 {
		t.Fatal("no-op favorite must not advance the change log")
	}
	if r := f.do(f.a, OpFavoriteSet, m("food_id", "b:pisang", "favorite", false)); string(r.State) != "null" {
		t.Fatalf("%+v", r)
	}
	for _, bad := range []string{"", "pisang", "b:UPPER", "c:not-a-uuid", "x:1", "b:" + string(make([]byte, 70))} {
		if r := f.do(f.a, OpFavoriteSet, m("food_id", bad, "favorite", true)); r.Status != StatusRejected {
			t.Errorf("food_id %q accepted", bad)
		}
	}
}

func TestFavoritesLimitAndReplace(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ids := make([]string, 0, MaxFavorites)
	for i := range MaxFavorites {
		ids = append(ids, fmt.Sprintf("b:food-%d", i))
	}
	if _, err := f.e.ReplaceFavorites(ctx, f.a, ids); err != nil {
		t.Fatal(err)
	}
	if r := f.do(f.a, OpFavoriteSet, m("food_id", "b:one-too-many", "favorite", true)); r.Status != StatusRejected {
		t.Fatalf("501st favorite accepted: %+v", r)
	}
	out, err := f.e.ReplaceFavorites(ctx, f.a, []string{"b:food-1", "b:new", "b:new"})
	if err != nil || len(out) != 2 {
		t.Fatalf("replace = %v, %v", out, err)
	}
	pull, _ := f.e.Pull(ctx, f.a, 0, 1000)
	removed := 0
	for _, c := range pull.Changes {
		if c.Entity == EntityFavorite && string(c.State) == "null" {
			removed++
		}
	}
	if removed != MaxFavorites-1 {
		t.Fatalf("removed favorites must be reported as null in pull, got %d", removed)
	}
}

// ---- days ----

func TestDayUpdate(t *testing.T) {
	f := newFixture(t)
	f.do(f.a, OpDayWater, m("date", "2026-11-17", "delta", 3))
	r := f.do(f.a, OpDayUpdate, m("date", "2026-11-17", "base_version", 0, "patch", m("steps", 5200, "note", "  Jalan pagi ")))
	d := state[Day](t, r)
	if r.Status != StatusApplied || d.Version != 1 || *d.Steps != 5200 || d.Note != "Jalan pagi" || d.WaterGlasses != 3 {
		t.Fatalf("%+v", d)
	}
	if r := f.do(f.a, OpDayUpdate, m("date", "2026-11-17", "base_version", 0, "patch", m("steps", 1))); r.Status != StatusConflict {
		t.Fatalf("stale day update: %+v", r)
	}
	// Water does not disturb the version used for conflict detection.
	f.do(f.a, OpDayWater, m("date", "2026-11-17", "delta", 1))
	r = f.do(f.a, OpDayUpdate, m("date", "2026-11-17", "base_version", 1, "patch", m("steps", nil)))
	if d := state[Day](t, r); r.Status != StatusApplied || d.Steps != nil || d.Note != "Jalan pagi" || d.WaterGlasses != 4 || d.Version != 2 {
		t.Fatalf("explicit null clears steps, other fields stay: %+v", d)
	}
	for name, p := range map[string]map[string]any{
		"empty patch":    m("date", "2026-11-17", "patch", m()),
		"negative steps": m("date", "2026-11-17", "patch", m("steps", -1)),
		"too many steps": m("date", "2026-11-17", "patch", m("steps", 200001)),
		"fractional":     m("date", "2026-11-17", "patch", m("steps", 1.5)),
		"note too long":  m("date", "2026-11-17", "patch", m("note", string(make([]rune, 0))+fmt.Sprintf("%0501d", 0))),
		"unknown patch":  m("date", "2026-11-17", "patch", m("water_glasses", 99)),
	} {
		if r := f.do(f.a, OpDayUpdate, p); r.Status != StatusRejected {
			t.Errorf("%s: %+v", name, r)
		}
	}
}

// ---- pull ----

func TestPullIsIncrementalAndCompacted(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.do(f.a, OpFoodLogCreate, logPayload(logA))
	f.do(f.a, OpWeightPut, m("date", "2026-11-17", "weight_kg", 70))
	f.do(f.a, OpFoodLogUpdate, m("id", logA, "patch", m("quantity", 2)))
	f.do(f.a, OpFoodLogUpdate, m("id", logA, "patch", m("quantity", 3)))

	full, err := f.e.Pull(ctx, f.a, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Changes) != 2 {
		t.Fatalf("change_log keeps one row per entity: got %d changes", len(full.Changes))
	}
	if full.Changes[0].Entity != EntityWeight || full.Changes[1].Entity != EntityFoodLog {
		t.Fatalf("ordered by last change: %+v", full.Changes)
	}
	var latest FoodLog
	_ = json.Unmarshal(full.Changes[1].State, &latest)
	if latest.Quantity != 3 || latest.Version != 3 || full.DataEpoch != 1 || full.HasMore {
		t.Fatalf("%+v", latest)
	}

	// An old cursor still works and returns only what changed after it.
	cursor := full.Cursor
	f.do(f.a, OpWeightDelete, m("date", "2026-11-17"))
	inc, _ := f.e.Pull(ctx, f.a, cursor, 100)
	if len(inc.Changes) != 1 || inc.Changes[0].Entity != EntityWeight || string(inc.Changes[0].State) != "null" {
		t.Fatalf("deleted weight must come through as state null: %+v", inc.Changes)
	}
	empty, _ := f.e.Pull(ctx, f.a, inc.Cursor, 100)
	if len(empty.Changes) != 0 || empty.Cursor != inc.Cursor {
		t.Fatalf("nothing new: %+v", empty)
	}
}

func TestPullPagination(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for i := range 5 {
		f.do(f.a, OpWeightPut, m("date", fmt.Sprintf("2026-11-%02d", 10+i), "weight_kg", 70))
	}
	var dates []string
	var cursor int64
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("pagination does not terminate")
		}
		res, err := f.e.Pull(ctx, f.a, cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range res.Changes {
			dates = append(dates, c.Key)
		}
		cursor = res.Cursor
		if !res.HasMore {
			break
		}
	}
	if len(dates) != 5 || dates[0] != "2026-11-10" || dates[4] != "2026-11-14" {
		t.Fatalf("paged pull = %v", dates)
	}
}

// ---- multi-device convergence ----

func TestTwoDevicesConverge(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.do(f.a, OpFoodLogCreate, logPayload(logA))

	// Device 2 (offline) saw version 1, device 1 deletes, device 2 edits when it reconnects.
	f.do(f.a, OpFoodLogDelete, m("id", logA))
	late := f.do(f.a, OpFoodLogUpdate, m("id", logA, "base_version", 1, "patch", m("quantity", 4)))
	if late.Status != StatusConflict || state[FoodLog](t, late).DeletedAt == nil {
		t.Fatalf("the offline edit of a deleted entry must conflict: %+v", late)
	}
	// Device 2 pulls and converges on the deleted state.
	pull, _ := f.e.Pull(ctx, f.a, 0, 100)
	var l FoodLog
	_ = json.Unmarshal(pull.Changes[0].State, &l)
	if l.DeletedAt == nil || l.Quantity != 1.5 {
		t.Fatalf("converged state: %+v", l)
	}
}

// ---- epoch / delete all ----

func TestDeleteAllDataIsolatedAndBumpsEpoch(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, uid := range []string{f.a, f.b} {
		f.do(uid, OpProfilePut, profilePayload())
		f.do(uid, OpFoodLogCreate, logPayload(logA))
		f.do(uid, OpWeightPut, m("date", "2026-11-17", "weight_kg", 70))
		f.do(uid, OpDayWater, m("date", "2026-11-17", "delta", 2))
		f.do(uid, OpFavoriteSet, m("food_id", "b:pisang", "favorite", true))
		f.do(uid, OpCustomFoodCreate, m("id", logB, "name", "X", "serving_label", "1", "kcal", 1, "protein_g", 0, "carbs_g", 0, "fat_g", 0))
	}
	epoch, err := f.e.DeleteAllData(ctx, f.a)
	if err != nil || epoch != 2 {
		t.Fatalf("epoch = %d, err = %v", epoch, err)
	}
	for _, table := range []string{"profiles", "favorites", "days", "food_logs", "custom_foods", "weights", "sync_operations", "change_log"} {
		if n := f.count(`SELECT count(*) FROM `+table+` WHERE user_id = ?`, f.a); n != 0 {
			t.Errorf("%s still has %d rows for the deleted user", table, n)
		}
		if n := f.count(`SELECT count(*) FROM `+table+` WHERE user_id = ?`, f.b); n == 0 {
			t.Errorf("%s of the other user was wiped", table)
		}
	}
	if n := f.count(`SELECT count(*) FROM users WHERE id = ?`, f.a); n != 1 {
		t.Fatal("the account itself must remain")
	}
	if e, _ := f.e.Epoch(ctx, f.b); e != 1 {
		t.Fatalf("other user's epoch changed: %d", e)
	}

	// A device still on epoch 1 must be refused and must not resurrect anything.
	stale := int64(1)
	op, _ := Marshal("late-op", OpFoodLogCreate, logPayload(logC))
	_, err = f.e.Apply(ctx, f.a, op, &stale)
	var em *EpochMismatchError
	if !errors.As(err, &em) || em.Current != 2 {
		t.Fatalf("want EpochMismatchError(2), got %v", err)
	}
	if n := f.count(`SELECT count(*) FROM food_logs WHERE user_id = ?`, f.a); n != 0 {
		t.Fatal("stale-epoch op was applied")
	}
	if n := f.count(`SELECT count(*) FROM sync_operations WHERE user_id = ? AND op_id = 'late-op'`, f.a); n != 0 {
		t.Fatal("stale-epoch op must not be recorded in the ledger")
	}
	cur := int64(2)
	if r, err := f.e.Apply(ctx, f.a, op, &cur); err != nil || r.Status != StatusApplied {
		t.Fatalf("matching epoch should work: %+v %v", r, err)
	}
}

// ---- retention ----

func TestPurgeRemovesOldSoftDeletesAndLedgerOnly(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.do(f.a, OpFoodLogCreate, logPayload(logA))
	f.do(f.a, OpFoodLogCreate, logPayload(logB))
	f.do(f.a, OpFoodLogCreate, logPayload(logC))
	f.do(f.a, OpFoodLogDelete, m("id", logA)) // deleted now
	f.now = f.now.Add(25 * 24 * time.Hour)
	f.do(f.a, OpFoodLogDelete, m("id", logB)) // deleted 25 days after A
	f.now = f.now.Add(10 * 24 * time.Hour)    // A is 35 days old, B is 10 days old

	st, err := f.e.Purge(ctx, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if st.FoodLogs != 1 {
		t.Fatalf("purged %d food logs, want 1 (only A is past 30 days)", st.FoodLogs)
	}
	if l, _ := loadFoodLog(ctx, f.db.Read, f.a, logA); l != nil {
		t.Fatal("A should be gone")
	}
	if l, _ := loadFoodLog(ctx, f.db.Read, f.a, logB); l == nil {
		t.Fatal("B (recently deleted) must stay restorable")
	}
	if l, _ := loadFoodLog(ctx, f.db.Read, f.a, logC); l == nil {
		t.Fatal("active entries are never purged")
	}
	// Clients holding A learn it is gone through pull (state null).
	pull, _ := f.e.Pull(ctx, f.a, 0, 100)
	var sawNull bool
	for _, c := range pull.Changes {
		if c.Key == logA && string(c.State) == "null" {
			sawNull = true
		}
	}
	if !sawNull {
		t.Fatal("purged food log must be reported as state null")
	}

	f.now = f.now.Add(100 * 24 * time.Hour)
	st, _ = f.e.Purge(ctx, f.now)
	if st.Operations == 0 || f.count(`SELECT count(*) FROM sync_operations`) != 0 {
		t.Fatalf("ledger rows older than 90 days must be purged: %+v", st)
	}
}

// ---- lists ----

func TestFoodLogListPaginationAndFilters(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ids := []string{logA, logB, logC, "44444444-4444-4444-8444-444444444444", "55555555-5555-4555-8555-555555555555"}
	for i, id := range ids {
		f.now = f.now.Add(time.Second)
		f.do(f.a, OpFoodLogCreate, logPayload(id, "date", fmt.Sprintf("2026-11-%02d", 10+i%3)))
	}
	f.do(f.a, OpFoodLogDelete, m("id", logB))

	var got []string
	cursor := ""
	for pages := 0; pages < 10; pages++ {
		logs, next, err := f.e.ListFoodLogs(ctx, f.a, FoodLogQuery{From: "2026-11-01", To: "2026-11-30", Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range logs {
			got = append(got, l.ID)
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if len(got) != 4 {
		t.Fatalf("deleted entries must be hidden by default, got %v", got)
	}
	all, _, _ := f.e.ListFoodLogs(ctx, f.a, FoodLogQuery{From: "2026-11-01", To: "2026-11-30", Limit: 100, IncludeDeleted: true})
	if len(all) != 5 {
		t.Fatalf("include_deleted: %d", len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].Date > all[i].Date {
			t.Fatal("results must be ordered by date")
		}
	}
	narrow, _, _ := f.e.ListFoodLogs(ctx, f.a, FoodLogQuery{From: "2026-11-11", To: "2026-11-11", Limit: 100})
	for _, l := range narrow {
		if l.Date != "2026-11-11" {
			t.Fatalf("range filter leaked %s", l.Date)
		}
	}
	if _, _, err := f.e.ListFoodLogs(ctx, f.a, FoodLogQuery{From: "2026-11-01", To: "2026-11-30", Limit: 2, Cursor: "garbage!"}); !errors.Is(err, ErrBadCursor) {
		t.Fatalf("want ErrBadCursor, got %v", err)
	}
}

func TestCustomFoodsAndWeightsPagination(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	names := []string{"Smoothie", "Bubur", "Nasi bakar", "Kue"}
	for i, n := range names {
		id := fmt.Sprintf("%08d-0000-4000-8000-000000000000", i+1)
		f.do(f.a, OpCustomFoodCreate, m("id", id, "name", n, "serving_label", "1", "kcal", 100, "protein_g", 1, "carbs_g", 1, "fat_g", 1))
		f.do(f.a, OpWeightPut, m("date", fmt.Sprintf("2026-11-%02d", 10+i), "weight_kg", 70+float64(i)))
	}
	var got []string
	cursor := ""
	for range 5 {
		foods, next, err := f.e.ListCustomFoods(ctx, f.a, 3, cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range foods {
			got = append(got, c.Name)
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if fmt.Sprint(got) != "[Bubur Kue Nasi bakar Smoothie]" {
		t.Fatalf("custom foods = %v", got)
	}
	ws, next, _ := f.e.ListWeights(ctx, f.a, "2026-11-11", "2026-11-12", 100, "")
	if len(ws) != 2 || next != "" || ws[0].Date != "2026-11-11" {
		t.Fatalf("weights range: %+v", ws)
	}
	page, next, _ := f.e.ListWeights(ctx, f.a, "", "", 3, "")
	page2, next2, _ := f.e.ListWeights(ctx, f.a, "", "", 3, next)
	if len(page) != 3 || next == "" || len(page2) != 1 || next2 != "" || page2[0].Date != "2026-11-13" {
		t.Fatalf("weights paging: %v / %v", page, page2)
	}
}
