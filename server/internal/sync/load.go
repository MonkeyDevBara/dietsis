package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/MonkeyDevBara/dietsis/server/internal/nutrition"
)

var errNotWhole = errors.New("must be a whole number")

// queryer is satisfied by both *sql.DB (read pool) and *sql.Tx (inside an op).
type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Every loader is scoped by user_id: the owner is part of every lookup, so one user can never
// reach another user's rows by guessing ids (docs/SECURITY.md → IDOR).

func nullable[T any](n sql.Null[T]) *T {
	if !n.Valid {
		return nil
	}
	v := n.V
	return &v
}

func notFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }

func loadProfile(ctx context.Context, q queryer, uid string) (*Profile, error) {
	var p Profile
	var cm, pm, wm int
	err := q.QueryRowContext(ctx, `SELECT display_name, gender, age, height_cm, weight_kg, goal_weight_kg, activity_level,
		calorie_target, protein_target_g, water_target_glasses, calorie_target_manual, protein_target_manual, water_target_manual,
		version, created_at, updated_at FROM profiles WHERE user_id = ?`, uid).
		Scan(&p.DisplayName, &p.Gender, &p.Age, &p.HeightCM, &p.WeightKG, &p.GoalWeightKG, &p.ActivityLevel,
			&p.CalorieTarget, &p.ProteinTargetG, &p.WaterTargetGlasses, &cm, &pm, &wm, &p.Version, &p.CreatedAt, &p.UpdatedAt)
	if notFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load profile: %w", err)
	}
	p.CalorieTargetManual, p.ProteinTargetManual, p.WaterTargetManual = cm == 1, pm == 1, wm == 1
	rec := nutrition.Recommend(nutrition.Body{
		Gender: nutrition.Gender(p.Gender), Age: p.Age, HeightCM: p.HeightCM, WeightKG: p.WeightKG,
		GoalWeightKG: p.GoalWeightKG, ActivityLevel: nutrition.ActivityLevel(p.ActivityLevel),
	})
	p.Recommended = Targets{CalorieTarget: rec.CalorieTarget, ProteinTargetG: rec.ProteinTargetG, WaterTargetGlasses: rec.WaterTargetGlasses}
	return &p, nil
}

func loadFavorite(ctx context.Context, q queryer, uid, foodID string) (*Favorite, error) {
	f := Favorite{FoodID: foodID}
	err := q.QueryRowContext(ctx, `SELECT created_at FROM favorites WHERE user_id = ? AND food_id = ?`, uid, foodID).Scan(&f.CreatedAt)
	if notFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load favorite: %w", err)
	}
	return &f, nil
}

const dayCols = `date, water_glasses, steps, note, version, updated_at`

func scanDay(s interface{ Scan(...any) error }) (Day, error) {
	var d Day
	var steps sql.Null[int]
	if err := s.Scan(&d.Date, &d.WaterGlasses, &steps, &d.Note, &d.Version, &d.UpdatedAt); err != nil {
		return Day{}, err
	}
	d.Steps = nullable(steps)
	return d, nil
}

func loadDay(ctx context.Context, q queryer, uid, date string) (*Day, error) {
	d, err := scanDay(q.QueryRowContext(ctx, `SELECT `+dayCols+` FROM days WHERE user_id = ? AND date = ?`, uid, date))
	if notFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load day: %w", err)
	}
	return &d, nil
}

const foodLogCols = `id, date, meal, food_id, name, serving_label, serving_grams, quantity, kcal, protein_g, carbs_g, fat_g, version, created_at, updated_at, deleted_at`

func scanFoodLog(s interface{ Scan(...any) error }) (FoodLog, error) {
	var l FoodLog
	var foodID sql.Null[string]
	var grams sql.Null[float64]
	var deleted sql.Null[int64]
	if err := s.Scan(&l.ID, &l.Date, &l.Meal, &foodID, &l.Name, &l.ServingLabel, &grams, &l.Quantity,
		&l.Kcal, &l.ProteinG, &l.CarbsG, &l.FatG, &l.Version, &l.CreatedAt, &l.UpdatedAt, &deleted); err != nil {
		return FoodLog{}, err
	}
	l.FoodID, l.ServingGrams, l.DeletedAt = nullable(foodID), nullable(grams), nullable(deleted)
	t := nutrition.EntryTotals(nutrition.Nutrients{Kcal: l.Kcal, ProteinG: l.ProteinG, CarbsG: l.CarbsG, FatG: l.FatG}, l.Quantity)
	l.Totals = Nutrients{Kcal: t.Kcal, ProteinG: t.ProteinG, CarbsG: t.CarbsG, FatG: t.FatG}
	return l, nil
}

func loadFoodLog(ctx context.Context, q queryer, uid, id string) (*FoodLog, error) {
	l, err := scanFoodLog(q.QueryRowContext(ctx, `SELECT `+foodLogCols+` FROM food_logs WHERE user_id = ? AND id = ?`, uid, id))
	if notFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load food log: %w", err)
	}
	return &l, nil
}

const customFoodCols = `id, name, serving_label, serving_grams, kcal, protein_g, carbs_g, fat_g, version, created_at, updated_at`

func scanCustomFood(s interface{ Scan(...any) error }) (CustomFood, error) {
	var f CustomFood
	var grams sql.Null[float64]
	if err := s.Scan(&f.ID, &f.Name, &f.ServingLabel, &grams, &f.Kcal, &f.ProteinG, &f.CarbsG, &f.FatG, &f.Version, &f.CreatedAt, &f.UpdatedAt); err != nil {
		return CustomFood{}, err
	}
	f.ServingGrams = nullable(grams)
	return f, nil
}

func loadCustomFood(ctx context.Context, q queryer, uid, id string) (*CustomFood, error) {
	f, err := scanCustomFood(q.QueryRowContext(ctx, `SELECT `+customFoodCols+` FROM custom_foods WHERE user_id = ? AND id = ?`, uid, id))
	if notFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load custom food: %w", err)
	}
	return &f, nil
}

const weightCols = `date, weight_kg, waist_cm, version, updated_at`

func scanWeight(s interface{ Scan(...any) error }) (Weight, error) {
	var w Weight
	var waist sql.Null[float64]
	if err := s.Scan(&w.Date, &w.WeightKG, &waist, &w.Version, &w.UpdatedAt); err != nil {
		return Weight{}, err
	}
	w.WaistCM = nullable(waist)
	return w, nil
}

func loadWeight(ctx context.Context, q queryer, uid, date string) (*Weight, error) {
	w, err := scanWeight(q.QueryRowContext(ctx, `SELECT `+weightCols+` FROM weights WHERE user_id = ? AND date = ?`, uid, date))
	if notFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load weight: %w", err)
	}
	return &w, nil
}

var jsonNull = json.RawMessage("null")

// marshalState renders an entity pointer as JSON, or "null" when it does not exist. The nil check
// must happen on the typed pointer, so each caller passes its own typed value through here.
func marshalState[T any](v *T) (json.RawMessage, error) {
	if v == nil {
		return jsonNull, nil
	}
	b, err := json.Marshal(v)
	return b, err
}

// loadState returns the current JSON state of any entity ("null" if missing). Used for op results
// and for GET /sync/pull.
func loadState(ctx context.Context, q queryer, uid, entity, key string) (json.RawMessage, error) {
	switch entity {
	case EntityProfile:
		v, err := loadProfile(ctx, q, uid)
		if err != nil {
			return nil, err
		}
		return marshalState(v)
	case EntityFavorite:
		v, err := loadFavorite(ctx, q, uid, key)
		if err != nil {
			return nil, err
		}
		return marshalState(v)
	case EntityDay:
		v, err := loadDay(ctx, q, uid, key)
		if err != nil {
			return nil, err
		}
		return marshalState(v)
	case EntityFoodLog:
		v, err := loadFoodLog(ctx, q, uid, key)
		if err != nil {
			return nil, err
		}
		return marshalState(v)
	case EntityCustomFood:
		v, err := loadCustomFood(ctx, q, uid, key)
		if err != nil {
			return nil, err
		}
		return marshalState(v)
	case EntityWeight:
		v, err := loadWeight(ctx, q, uid, key)
		if err != nil {
			return nil, err
		}
		return marshalState(v)
	}
	return nil, fmt.Errorf("unknown entity %q", entity)
}
