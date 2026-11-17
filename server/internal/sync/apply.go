package sync

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/MonkeyDevBara/dietsis/server/internal/nutrition"
	"github.com/MonkeyDevBara/dietsis/server/internal/validate"
)

const (
	maxFavorites = 500
	waterMax     = 100
)

var (
	uuidRe   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	foodIDRe = regexp.MustCompile(`^(b:[a-z0-9][a-z0-9-]{0,59}|c:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$`)
)

var meals = []string{"breakfast", "lunch", "dinner", "snack"}

// ---- profile ----

// validateProfile checks the input and replaces DisplayName with its trimmed form.
func validateProfile(in *ProfilePut) validate.Errors {
	e := validate.Errors{}
	in.DisplayName = e.Text("display_name", in.DisplayName, 1, 60, false)
	e.Oneof("gender", in.Gender, "male", "female")
	e.Range("age", in.Age, 18, 100, true)
	e.Range("height_cm", in.HeightCM, 100, 250, false)
	e.Range("weight_kg", in.WeightKG, 30, 300, false)
	e.Range("goal_weight_kg", in.GoalWeightKG, 30, 300, false)
	e.Oneof("activity_level", in.ActivityLevel, "sedentary", "light", "moderate", "active", "very_active")
	// A target the user did not set manually is recomputed from the body data; whatever the client
	// sent for it is ignored, so only manual targets are range-checked.
	if in.CalorieTargetManual {
		e.Range("calorie_target", in.CalorieTarget, 800, 6000, true)
	}
	if in.ProteinTargetManual {
		e.Range("protein_target_g", in.ProteinTargetG, 10, 400, true)
	}
	if in.WaterTargetManual {
		e.Range("water_target_glasses", in.WaterTargetGlasses, 1, 30, true)
	}
	return e
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (e *Engine) profilePut(c *opCtx, raw json.RawMessage) (Result, error) {
	var in ProfilePut
	if err := strictDecode(raw, &in); err != nil {
		return badPayload(), nil
	}
	if errs := validateProfile(&in); !errs.Empty() {
		return invalidFields(errs), nil
	}
	prev, err := loadProfile(c.ctx, c.tx, c.uid)
	if err != nil {
		return Result{}, err
	}
	if stale(in.BaseVersion, prev != nil, profileVersion(prev)) {
		return c.result(StatusConflict, EntityProfile, "profile")
	}

	rec := nutrition.Recommend(nutrition.Body{
		Gender: nutrition.Gender(in.Gender), Age: validate.Whole(in.Age), HeightCM: in.HeightCM, WeightKG: in.WeightKG,
		GoalWeightKG: in.GoalWeightKG, ActivityLevel: nutrition.ActivityLevel(in.ActivityLevel),
	})
	cal, prot, water := rec.CalorieTarget, rec.ProteinTargetG, rec.WaterTargetGlasses
	if in.CalorieTargetManual {
		cal = validate.Whole(in.CalorieTarget)
	}
	if in.ProteinTargetManual {
		prot = validate.Whole(in.ProteinTargetG)
	}
	if in.WaterTargetManual {
		water = validate.Whole(in.WaterTargetGlasses)
	}

	if prev == nil {
		_, err = c.tx.ExecContext(c.ctx, `INSERT INTO profiles (user_id, display_name, gender, age, height_cm, weight_kg, goal_weight_kg, activity_level,
			calorie_target, protein_target_g, water_target_glasses, calorie_target_manual, protein_target_manual, water_target_manual, version, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
			c.uid, in.DisplayName, in.Gender, validate.Whole(in.Age), in.HeightCM, in.WeightKG, in.GoalWeightKG, in.ActivityLevel,
			cal, prot, water, boolInt(in.CalorieTargetManual), boolInt(in.ProteinTargetManual), boolInt(in.WaterTargetManual), c.nowMs, c.nowMs)
	} else {
		_, err = c.tx.ExecContext(c.ctx, `UPDATE profiles SET display_name = ?, gender = ?, age = ?, height_cm = ?, weight_kg = ?, goal_weight_kg = ?, activity_level = ?,
			calorie_target = ?, protein_target_g = ?, water_target_glasses = ?, calorie_target_manual = ?, protein_target_manual = ?, water_target_manual = ?,
			version = version + 1, updated_at = ? WHERE user_id = ?`,
			in.DisplayName, in.Gender, validate.Whole(in.Age), in.HeightCM, in.WeightKG, in.GoalWeightKG, in.ActivityLevel,
			cal, prot, water, boolInt(in.CalorieTargetManual), boolInt(in.ProteinTargetManual), boolInt(in.WaterTargetManual), c.nowMs, c.uid)
	}
	if err != nil {
		return Result{}, fmt.Errorf("write profile: %w", err)
	}
	if err := c.touch(EntityProfile, "profile"); err != nil {
		return Result{}, err
	}
	res, err := c.result(StatusApplied, EntityProfile, "profile")
	res.Created = prev == nil
	return res, err
}

func profileVersion(p *Profile) int64 {
	if p == nil {
		return 0
	}
	return p.Version
}

// ---- favorites ----

func (e *Engine) favoriteSet(c *opCtx, raw json.RawMessage) (Result, error) {
	var in FavoriteSet
	if err := strictDecode(raw, &in); err != nil {
		return badPayload(), nil
	}
	if !foodIDRe.MatchString(in.FoodID) {
		return invalidFields(map[string]string{"food_id": "Format tidak valid"}), nil
	}
	exists, err := loadFavorite(c.ctx, c.tx, c.uid, in.FoodID)
	if err != nil {
		return Result{}, err
	}
	changed := false
	switch {
	case in.Favorite && exists == nil:
		var n int
		if err := c.tx.QueryRowContext(c.ctx, `SELECT count(*) FROM favorites WHERE user_id = ?`, c.uid).Scan(&n); err != nil {
			return Result{}, err
		}
		if n >= maxFavorites {
			return invalidFields(map[string]string{"food_id": fmt.Sprintf("Maksimal %d favorit", maxFavorites)}), nil
		}
		_, err = c.tx.ExecContext(c.ctx, `INSERT INTO favorites (user_id, food_id, created_at) VALUES (?, ?, ?)`, c.uid, in.FoodID, c.nowMs)
		changed = true
	case !in.Favorite && exists != nil:
		_, err = c.tx.ExecContext(c.ctx, `DELETE FROM favorites WHERE user_id = ? AND food_id = ?`, c.uid, in.FoodID)
		changed = true
	}
	if err != nil {
		return Result{}, fmt.Errorf("write favorite: %w", err)
	}
	if changed {
		if err := c.touch(EntityFavorite, in.FoodID); err != nil {
			return Result{}, err
		}
	}
	return c.result(StatusApplied, EntityFavorite, in.FoodID)
}

// ---- days ----

func (e *Engine) dayWater(c *opCtx, raw json.RawMessage) (Result, error) {
	var in DayWater
	if err := strictDecode(raw, &in); err != nil {
		return badPayload(), nil
	}
	errs := validate.Errors{}
	errs.Date("date", in.Date, c.now)
	errs.Range("delta", in.Delta, -20, 20, true)
	if in.Delta == 0 {
		errs.Add("delta", "Tidak boleh 0")
	}
	if !errs.Empty() {
		return invalidFields(errs), nil
	}
	// One atomic statement: no read-modify-write in Go. The delta is used raw in DO UPDATE
	// (not excluded.*, which the INSERT part has already clamped to >= 0).
	// version stays 0 for rows created here: water is a counter outside versioning (OFFLINE_SYNC.md).
	_, err := c.tx.ExecContext(c.ctx, `INSERT INTO days (user_id, date, water_glasses, version, updated_at) VALUES (?1, ?2, max(0, min(?5, ?3)), 0, ?4)
		ON CONFLICT (user_id, date) DO UPDATE SET water_glasses = max(0, min(?5, water_glasses + ?3)), updated_at = ?4`,
		c.uid, in.Date, validate.Whole(in.Delta), c.nowMs, waterMax)
	if err != nil {
		return Result{}, fmt.Errorf("water upsert: %w", err)
	}
	if err := c.touch(EntityDay, in.Date); err != nil {
		return Result{}, err
	}
	return c.result(StatusApplied, EntityDay, in.Date)
}

func (e *Engine) dayUpdate(c *opCtx, raw json.RawMessage) (Result, error) {
	var in DayUpdate
	if err := strictDecode(raw, &in); err != nil {
		return badPayload(), nil
	}
	errs := validate.Errors{}
	errs.Date("date", in.Date, c.now)
	if !in.Patch.Steps.Set && in.Patch.Note == nil {
		return rejected(CodeBadRequest, "Tidak ada field yang diubah.", nil), nil
	}
	if in.Patch.Steps.Set && in.Patch.Steps.Val != nil {
		errs.Range("steps", float64(*in.Patch.Steps.Val), 0, 200000, true)
	}
	note := ""
	if in.Patch.Note != nil {
		note = errs.Text("note", *in.Patch.Note, 0, 500, true)
	}
	if !errs.Empty() {
		return invalidFields(errs), nil
	}

	prev, err := loadDay(c.ctx, c.tx, c.uid, in.Date)
	if err != nil {
		return Result{}, err
	}
	if stale(in.BaseVersion, prev != nil, dayVersion(prev)) {
		return c.result(StatusConflict, EntityDay, in.Date)
	}

	next := Day{Date: in.Date}
	if prev != nil {
		next = *prev
	}
	if in.Patch.Steps.Set {
		next.Steps = in.Patch.Steps.Val
	}
	if in.Patch.Note != nil {
		next.Note = note
	}
	var steps any
	if next.Steps != nil {
		steps = *next.Steps
	}
	_, err = c.tx.ExecContext(c.ctx, `INSERT INTO days (user_id, date, water_glasses, steps, note, version, updated_at) VALUES (?1, ?2, 0, ?3, ?4, 1, ?5)
		ON CONFLICT (user_id, date) DO UPDATE SET steps = ?3, note = ?4, version = version + 1, updated_at = ?5`,
		c.uid, in.Date, steps, next.Note, c.nowMs)
	if err != nil {
		return Result{}, fmt.Errorf("write day: %w", err)
	}
	if err := c.touch(EntityDay, in.Date); err != nil {
		return Result{}, err
	}
	res, err := c.result(StatusApplied, EntityDay, in.Date)
	res.Created = prev == nil
	return res, err
}

func dayVersion(d *Day) int64 {
	if d == nil {
		return 0
	}
	return d.Version
}

// ---- food logs ----

// validateFoodFields checks the fields shared by food logs and custom foods and returns the trimmed
// name and serving label.
func validateFoodFields(e validate.Errors, name, label string, grams *float64, kcal, protein, carbs, fat float64) (string, string) {
	n := e.Text("name", name, 1, 80, false)
	l := e.Text("serving_label", label, 1, 40, false)
	if grams != nil {
		e.Range("serving_grams", *grams, 0.1, 5000, false)
	}
	e.Range("kcal", kcal, 0, 5000, false)
	e.Range("protein_g", protein, 0, 500, false)
	e.Range("carbs_g", carbs, 0, 500, false)
	e.Range("fat_g", fat, 0, 500, false)
	return n, l
}

func validateFoodLog(l *FoodLog, c *opCtx) validate.Errors {
	e := validate.Errors{}
	l.Name, l.ServingLabel = validateFoodFields(e, l.Name, l.ServingLabel, l.ServingGrams, l.Kcal, l.ProteinG, l.CarbsG, l.FatG)
	e.Date("date", l.Date, c.now)
	e.Oneof("meal", l.Meal, meals...)
	e.Quantity("quantity", l.Quantity)
	if l.FoodID != nil && !foodIDRe.MatchString(*l.FoodID) {
		e.Add("food_id", "Format tidak valid")
	}
	return e
}

func (e *Engine) foodLogCreate(c *opCtx, raw json.RawMessage) (Result, error) {
	var in FoodLogCreate
	if err := strictDecode(raw, &in); err != nil {
		return badPayload(), nil
	}
	l := FoodLog{ID: in.ID, Date: in.Date, Meal: in.Meal, FoodID: in.FoodID, Name: in.Name, ServingLabel: in.ServingLabel,
		ServingGrams: in.ServingGrams, Quantity: in.Quantity, Kcal: in.Kcal, ProteinG: in.ProteinG, CarbsG: in.CarbsG, FatG: in.FatG}
	errs := validateFoodLog(&l, c)
	if !uuidRe.MatchString(in.ID) {
		errs.Add("id", "Harus UUID huruf kecil")
	}
	if !errs.Empty() {
		return invalidFields(errs), nil
	}

	prev, err := loadFoodLog(c.ctx, c.tx, c.uid, l.ID)
	if err != nil {
		return Result{}, err
	}
	if prev != nil {
		// Same id again (a retry, or the same op from another path): return what exists.
		return c.result(StatusApplied, EntityFoodLog, l.ID)
	}
	_, err = c.tx.ExecContext(c.ctx, `INSERT INTO food_logs (user_id, id, date, meal, food_id, name, serving_label, serving_grams, quantity, kcal, protein_g, carbs_g, fat_g, version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		c.uid, l.ID, l.Date, l.Meal, ptr(l.FoodID), l.Name, l.ServingLabel, ptr(l.ServingGrams), l.Quantity, l.Kcal, l.ProteinG, l.CarbsG, l.FatG, c.nowMs, c.nowMs)
	if err != nil {
		return Result{}, fmt.Errorf("insert food log: %w", err)
	}
	if err := c.touch(EntityFoodLog, l.ID); err != nil {
		return Result{}, err
	}
	res, err := c.result(StatusApplied, EntityFoodLog, l.ID)
	res.Created = true
	return res, err
}

// ptr turns an optional pointer into a driver value (nil → SQL NULL).
func ptr[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

func (e *Engine) foodLogUpdate(c *opCtx, raw json.RawMessage) (Result, error) {
	var in FoodLogUpdate
	if err := strictDecode(raw, &in); err != nil {
		return badPayload(), nil
	}
	if !uuidRe.MatchString(in.ID) {
		return invalidFields(map[string]string{"id": "Harus UUID huruf kecil"}), nil
	}
	if in.Patch.empty() {
		return rejected(CodeBadRequest, "Tidak ada field yang diubah.", nil), nil
	}
	prev, err := loadFoodLog(c.ctx, c.tx, c.uid, in.ID)
	if err != nil {
		return Result{}, err
	}
	if prev == nil || prev.DeletedAt != nil || stale(in.BaseVersion, true, prev.Version) {
		return c.result(StatusConflict, EntityFoodLog, in.ID)
	}

	next := *prev
	p := in.Patch
	if p.Date != nil {
		next.Date = *p.Date
	}
	if p.Meal != nil {
		next.Meal = *p.Meal
	}
	if p.Name != nil {
		next.Name = *p.Name
	}
	if p.ServingLabel != nil {
		next.ServingLabel = *p.ServingLabel
	}
	if p.ServingGrams.Set {
		next.ServingGrams = p.ServingGrams.Val
	}
	if p.Quantity != nil {
		next.Quantity = *p.Quantity
	}
	if p.Kcal != nil {
		next.Kcal = *p.Kcal
	}
	if p.ProteinG != nil {
		next.ProteinG = *p.ProteinG
	}
	if p.CarbsG != nil {
		next.CarbsG = *p.CarbsG
	}
	if p.FatG != nil {
		next.FatG = *p.FatG
	}
	if errs := validateFoodLog(&next, c); !errs.Empty() {
		return invalidFields(errs), nil
	}

	_, err = c.tx.ExecContext(c.ctx, `UPDATE food_logs SET date = ?, meal = ?, name = ?, serving_label = ?, serving_grams = ?, quantity = ?, kcal = ?, protein_g = ?, carbs_g = ?, fat_g = ?,
		version = version + 1, updated_at = ? WHERE user_id = ? AND id = ?`,
		next.Date, next.Meal, next.Name, next.ServingLabel, ptr(next.ServingGrams), next.Quantity, next.Kcal, next.ProteinG, next.CarbsG, next.FatG, c.nowMs, c.uid, in.ID)
	if err != nil {
		return Result{}, fmt.Errorf("update food log: %w", err)
	}
	if err := c.touch(EntityFoodLog, in.ID); err != nil {
		return Result{}, err
	}
	return c.result(StatusApplied, EntityFoodLog, in.ID)
}

func (e *Engine) foodLogDelete(c *opCtx, raw json.RawMessage) (Result, error) {
	return e.softDelete(c, raw, true)
}

func (e *Engine) foodLogRestore(c *opCtx, raw json.RawMessage) (Result, error) {
	return e.softDelete(c, raw, false)
}

// softDelete implements delete/restore. Both are idempotent no-ops when already in the target state,
// and never conflict. A missing entry yields an applied result with state null (REST maps it to 404).
func (e *Engine) softDelete(c *opCtx, raw json.RawMessage, deleting bool) (Result, error) {
	var in IDRef
	if err := strictDecode(raw, &in); err != nil {
		return badPayload(), nil
	}
	if !uuidRe.MatchString(in.ID) {
		return invalidFields(map[string]string{"id": "Harus UUID huruf kecil"}), nil
	}
	prev, err := loadFoodLog(c.ctx, c.tx, c.uid, in.ID)
	if err != nil {
		return Result{}, err
	}
	if prev != nil && (prev.DeletedAt != nil) != deleting {
		var deletedAt any
		if deleting {
			deletedAt = c.nowMs
		}
		if _, err := c.tx.ExecContext(c.ctx, `UPDATE food_logs SET deleted_at = ?, version = version + 1, updated_at = ? WHERE user_id = ? AND id = ?`,
			deletedAt, c.nowMs, c.uid, in.ID); err != nil {
			return Result{}, fmt.Errorf("soft delete: %w", err)
		}
		if err := c.touch(EntityFoodLog, in.ID); err != nil {
			return Result{}, err
		}
	}
	return c.result(StatusApplied, EntityFoodLog, in.ID)
}

// ---- custom foods ----

func (e *Engine) customFoodCreate(c *opCtx, raw json.RawMessage) (Result, error) {
	var in CustomFoodCreate
	if err := strictDecode(raw, &in); err != nil {
		return badPayload(), nil
	}
	errs := validate.Errors{}
	in.Name, in.ServingLabel = validateFoodFields(errs, in.Name, in.ServingLabel, in.ServingGrams, in.Kcal, in.ProteinG, in.CarbsG, in.FatG)
	if !uuidRe.MatchString(in.ID) {
		errs.Add("id", "Harus UUID huruf kecil")
	}
	if !errs.Empty() {
		return invalidFields(errs), nil
	}
	prev, err := loadCustomFood(c.ctx, c.tx, c.uid, in.ID)
	if err != nil {
		return Result{}, err
	}
	if prev != nil {
		return c.result(StatusApplied, EntityCustomFood, in.ID)
	}
	_, err = c.tx.ExecContext(c.ctx, `INSERT INTO custom_foods (user_id, id, name, serving_label, serving_grams, kcal, protein_g, carbs_g, fat_g, version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		c.uid, in.ID, in.Name, in.ServingLabel, ptr(in.ServingGrams), in.Kcal, in.ProteinG, in.CarbsG, in.FatG, c.nowMs, c.nowMs)
	if err != nil {
		return Result{}, fmt.Errorf("insert custom food: %w", err)
	}
	if err := c.touch(EntityCustomFood, in.ID); err != nil {
		return Result{}, err
	}
	res, err := c.result(StatusApplied, EntityCustomFood, in.ID)
	res.Created = true
	return res, err
}

// ---- weights ----

func (e *Engine) weightPut(c *opCtx, raw json.RawMessage) (Result, error) {
	var in WeightPut
	if err := strictDecode(raw, &in); err != nil {
		return badPayload(), nil
	}
	errs := validate.Errors{}
	errs.Date("date", in.Date, c.now)
	errs.Range("weight_kg", in.WeightKG, 30, 300, false)
	if in.WaistCM != nil {
		errs.Range("waist_cm", *in.WaistCM, 30, 250, false)
	}
	if !errs.Empty() {
		return invalidFields(errs), nil
	}
	prev, err := loadWeight(c.ctx, c.tx, c.uid, in.Date)
	if err != nil {
		return Result{}, err
	}
	var prevVersion int64
	if prev != nil {
		prevVersion = prev.Version
	}
	if stale(in.BaseVersion, prev != nil, prevVersion) {
		return c.result(StatusConflict, EntityWeight, in.Date)
	}
	// PRIMARY KEY (user_id, date) guarantees one entry per user per date even under races.
	_, err = c.tx.ExecContext(c.ctx, `INSERT INTO weights (user_id, date, weight_kg, waist_cm, version, updated_at) VALUES (?1, ?2, ?3, ?4, 1, ?5)
		ON CONFLICT (user_id, date) DO UPDATE SET weight_kg = ?3, waist_cm = ?4, version = version + 1, updated_at = ?5`,
		c.uid, in.Date, in.WeightKG, ptr(in.WaistCM), c.nowMs)
	if err != nil {
		return Result{}, fmt.Errorf("write weight: %w", err)
	}
	if err := c.touch(EntityWeight, in.Date); err != nil {
		return Result{}, err
	}
	res, err := c.result(StatusApplied, EntityWeight, in.Date)
	res.Created = prev == nil
	return res, err
}

func (e *Engine) weightDelete(c *opCtx, raw json.RawMessage) (Result, error) {
	var in WeightDelete
	if err := strictDecode(raw, &in); err != nil {
		return badPayload(), nil
	}
	errs := validate.Errors{}
	errs.Date("date", in.Date, c.now)
	if !errs.Empty() {
		return invalidFields(errs), nil
	}
	prev, err := loadWeight(c.ctx, c.tx, c.uid, in.Date)
	if err != nil {
		return Result{}, err
	}
	if prev == nil {
		return c.result(StatusApplied, EntityWeight, in.Date) // already gone: idempotent
	}
	if stale(in.BaseVersion, true, prev.Version) {
		return c.result(StatusConflict, EntityWeight, in.Date)
	}
	if _, err := c.tx.ExecContext(c.ctx, `DELETE FROM weights WHERE user_id = ? AND date = ?`, c.uid, in.Date); err != nil {
		return Result{}, fmt.Errorf("delete weight: %w", err)
	}
	if err := c.touch(EntityWeight, in.Date); err != nil {
		return Result{}, err
	}
	return c.result(StatusApplied, EntityWeight, in.Date)
}
