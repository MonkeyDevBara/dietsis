// Package sync is the single write path for user data (docs/OFFLINE_SYNC.md). Both the REST
// endpoints and POST /sync/push turn requests into Ops and call Engine.Apply, so idempotency,
// versioning, conflicts and the change log live in exactly one place.
package sync

import "encoding/json"

// API entity shapes (docs/API.md → Skema entitas). Timestamps are epoch milliseconds.

type Nutrients struct {
	Kcal     float64 `json:"kcal"`
	ProteinG float64 `json:"protein_g"`
	CarbsG   float64 `json:"carbs_g"`
	FatG     float64 `json:"fat_g"`
}

type Targets struct {
	CalorieTarget      int `json:"calorie_target"`
	ProteinTargetG     int `json:"protein_target_g"`
	WaterTargetGlasses int `json:"water_target_glasses"`
}

type Profile struct {
	DisplayName         string  `json:"display_name"`
	Gender              string  `json:"gender"`
	Age                 int     `json:"age"`
	HeightCM            float64 `json:"height_cm"`
	WeightKG            float64 `json:"weight_kg"`
	GoalWeightKG        float64 `json:"goal_weight_kg"`
	ActivityLevel       string  `json:"activity_level"`
	CalorieTarget       int     `json:"calorie_target"`
	ProteinTargetG      int     `json:"protein_target_g"`
	WaterTargetGlasses  int     `json:"water_target_glasses"`
	CalorieTargetManual bool    `json:"calorie_target_manual"`
	ProteinTargetManual bool    `json:"protein_target_manual"`
	WaterTargetManual   bool    `json:"water_target_manual"`
	Recommended         Targets `json:"recommended"`
	Version             int64   `json:"version"`
	CreatedAt           int64   `json:"created_at"`
	UpdatedAt           int64   `json:"updated_at"`
}

type Favorite struct {
	FoodID    string `json:"food_id"`
	CreatedAt int64  `json:"created_at"`
}

type Day struct {
	Date         string `json:"date"`
	WaterGlasses int    `json:"water_glasses"`
	Steps        *int   `json:"steps"`
	Note         string `json:"note"`
	Version      int64  `json:"version"`
	UpdatedAt    int64  `json:"updated_at"`
}

type FoodLog struct {
	ID           string    `json:"id"`
	Date         string    `json:"date"`
	Meal         string    `json:"meal"`
	FoodID       *string   `json:"food_id"`
	Name         string    `json:"name"`
	ServingLabel string    `json:"serving_label"`
	ServingGrams *float64  `json:"serving_grams"`
	Quantity     float64   `json:"quantity"`
	Kcal         float64   `json:"kcal"`
	ProteinG     float64   `json:"protein_g"`
	CarbsG       float64   `json:"carbs_g"`
	FatG         float64   `json:"fat_g"`
	Totals       Nutrients `json:"totals"`
	Version      int64     `json:"version"`
	CreatedAt    int64     `json:"created_at"`
	UpdatedAt    int64     `json:"updated_at"`
	DeletedAt    *int64    `json:"deleted_at"`
}

type CustomFood struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	ServingLabel string   `json:"serving_label"`
	ServingGrams *float64 `json:"serving_grams"`
	Kcal         float64  `json:"kcal"`
	ProteinG     float64  `json:"protein_g"`
	CarbsG       float64  `json:"carbs_g"`
	FatG         float64  `json:"fat_g"`
	Version      int64    `json:"version"`
	CreatedAt    int64    `json:"created_at"`
	UpdatedAt    int64    `json:"updated_at"`
}

type Weight struct {
	Date      string   `json:"date"`
	WeightKG  float64  `json:"weight_kg"`
	WaistCM   *float64 `json:"waist_cm"`
	Version   int64    `json:"version"`
	UpdatedAt int64    `json:"updated_at"`
}

// Entity names double as change_log.entity values.
const (
	EntityProfile    = "profile"
	EntityFavorite   = "favorite"
	EntityDay        = "day"
	EntityFoodLog    = "food_log"
	EntityCustomFood = "custom_food"
	EntityWeight     = "weight"
)

// Op types (docs/API.md → POST /sync/push).
const (
	OpProfilePut       = "profile.put"
	OpFavoriteSet      = "favorite.set"
	OpDayWater         = "day.water"
	OpDayUpdate        = "day.update"
	OpFoodLogCreate    = "food_log.create"
	OpFoodLogUpdate    = "food_log.update"
	OpFoodLogDelete    = "food_log.delete"
	OpFoodLogRestore   = "food_log.restore"
	OpCustomFoodCreate = "custom_food.create"
	OpWeightPut        = "weight.put"
	OpWeightDelete     = "weight.delete"
)

// Op is one client operation. ID is the idempotency key ("" = not recorded in the ledger).
type Op struct {
	ID      string          `json:"op_id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

const (
	StatusApplied  = "applied"
	StatusConflict = "conflict"
	StatusRejected = "rejected"
)

// Error codes carried by rejected results (mapped to HTTP statuses by the REST layer).
const (
	CodeValidation = "validation_failed"
	CodeBadRequest = "bad_request"
)

type ErrorInfo struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Details map[string]string `json:"details,omitempty"`
}

// Result is the outcome of one op. State is the entity's current server state (JSON "null" when it
// does not exist) for applied and conflict results; it is absent for rejected ones.
type Result struct {
	OpID   string          `json:"op_id"`
	Status string          `json:"status"`
	Entity string          `json:"entity,omitempty"`
	Key    string          `json:"key,omitempty"`
	State  json.RawMessage `json:"state,omitempty"`
	Error  *ErrorInfo      `json:"error,omitempty"`

	// Set by the engine, never persisted or sent.
	Created  bool `json:"-"` // a new entity was created by this call
	Replayed bool `json:"-"` // answered from the idempotency ledger
}

// ---- op payloads (strictly decoded; unknown fields are rejected) ----

type ProfilePut struct {
	DisplayName         string  `json:"display_name"`
	Gender              string  `json:"gender"`
	Age                 float64 `json:"age"`
	HeightCM            float64 `json:"height_cm"`
	WeightKG            float64 `json:"weight_kg"`
	GoalWeightKG        float64 `json:"goal_weight_kg"`
	ActivityLevel       string  `json:"activity_level"`
	CalorieTarget       float64 `json:"calorie_target"`
	ProteinTargetG      float64 `json:"protein_target_g"`
	WaterTargetGlasses  float64 `json:"water_target_glasses"`
	CalorieTargetManual bool    `json:"calorie_target_manual"`
	ProteinTargetManual bool    `json:"protein_target_manual"`
	WaterTargetManual   bool    `json:"water_target_manual"`
	BaseVersion         *int64  `json:"base_version"`
}

type FavoriteSet struct {
	FoodID   string `json:"food_id"`
	Favorite bool   `json:"favorite"`
}

type DayWater struct {
	Date  string  `json:"date"`
	Delta float64 `json:"delta"`
}

// OptInt distinguishes an absent field from an explicit null.
type OptInt struct {
	Set bool
	Val *int
}

func (o *OptInt) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Val = nil
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	if f != float64(int(f)) {
		return errNotWhole
	}
	v := int(f)
	o.Val = &v
	return nil
}

// OptFloat is the float twin of OptInt.
type OptFloat struct {
	Set bool
	Val *float64
}

func (o *OptFloat) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Val = nil
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	o.Val = &f
	return nil
}

type DayPatch struct {
	Steps OptInt  `json:"steps"`
	Note  *string `json:"note"`
}

type DayUpdate struct {
	Date        string   `json:"date"`
	BaseVersion *int64   `json:"base_version"`
	Patch       DayPatch `json:"patch"`
}

type FoodLogCreate struct {
	ID           string   `json:"id"`
	Date         string   `json:"date"`
	Meal         string   `json:"meal"`
	FoodID       *string  `json:"food_id"`
	Name         string   `json:"name"`
	ServingLabel string   `json:"serving_label"`
	ServingGrams *float64 `json:"serving_grams"`
	Quantity     float64  `json:"quantity"`
	Kcal         float64  `json:"kcal"`
	ProteinG     float64  `json:"protein_g"`
	CarbsG       float64  `json:"carbs_g"`
	FatG         float64  `json:"fat_g"`
}

type FoodLogPatch struct {
	Date         *string  `json:"date"`
	Meal         *string  `json:"meal"`
	Name         *string  `json:"name"`
	ServingLabel *string  `json:"serving_label"`
	ServingGrams OptFloat `json:"serving_grams"`
	Quantity     *float64 `json:"quantity"`
	Kcal         *float64 `json:"kcal"`
	ProteinG     *float64 `json:"protein_g"`
	CarbsG       *float64 `json:"carbs_g"`
	FatG         *float64 `json:"fat_g"`
}

func (p FoodLogPatch) empty() bool {
	return p.Date == nil && p.Meal == nil && p.Name == nil && p.ServingLabel == nil && !p.ServingGrams.Set &&
		p.Quantity == nil && p.Kcal == nil && p.ProteinG == nil && p.CarbsG == nil && p.FatG == nil
}

type FoodLogUpdate struct {
	ID          string       `json:"id"`
	BaseVersion *int64       `json:"base_version"`
	Patch       FoodLogPatch `json:"patch"`
}

type IDRef struct {
	ID string `json:"id"`
}

type CustomFoodCreate struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	ServingLabel string   `json:"serving_label"`
	ServingGrams *float64 `json:"serving_grams"`
	Kcal         float64  `json:"kcal"`
	ProteinG     float64  `json:"protein_g"`
	CarbsG       float64  `json:"carbs_g"`
	FatG         float64  `json:"fat_g"`
}

type WeightPut struct {
	Date        string   `json:"date"`
	WeightKG    float64  `json:"weight_kg"`
	WaistCM     *float64 `json:"waist_cm"`
	BaseVersion *int64   `json:"base_version"`
}

type WeightDelete struct {
	Date        string `json:"date"`
	BaseVersion *int64 `json:"base_version"`
}
