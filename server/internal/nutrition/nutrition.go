// Package nutrition holds the target formulas and nutrient arithmetic. The TypeScript twin lives in
// src/lib/nutrition.ts; both are checked against testdata/vectors.json (docs/DECISIONS.md D-010/D-011).
// Estimates only; not medical advice.
package nutrition

import "math"

type Gender string

const (
	Male   Gender = "male"
	Female Gender = "female"
)

type ActivityLevel string

var activityFactors = map[ActivityLevel]float64{
	"sedentary":   1.2,
	"light":       1.375,
	"moderate":    1.55,
	"active":      1.725,
	"very_active": 1.9,
}

const GlassML = 250

type Body struct {
	Gender        Gender        `json:"gender"`
	Age           int           `json:"age"`
	HeightCM      float64       `json:"height_cm"`
	WeightKG      float64       `json:"weight_kg"`
	GoalWeightKG  float64       `json:"goal_weight_kg"`
	ActivityLevel ActivityLevel `json:"activity_level"`
}

type Targets struct {
	CalorieTarget      int `json:"calorie_target"`
	ProteinTargetG     int `json:"protein_target_g"`
	WaterTargetGlasses int `json:"water_target_glasses"`
}

type Nutrients struct {
	Kcal     float64 `json:"kcal"`
	ProteinG float64 `json:"protein_g"`
	CarbsG   float64 `json:"carbs_g"`
	FatG     float64 `json:"fat_g"`
}

// SafeFloor is the lowest calorie value the *suggestion* will return.
func SafeFloor(g Gender) float64 {
	if g == Male {
		return 1500
	}
	return 1200
}

func round1(x float64) float64 { return math.Round(x*10) / 10 }

// EntryTotals scales per-serving values by quantity (kcal to whole numbers, macros to 1 decimal).
func EntryTotals(per Nutrients, quantity float64) Nutrients {
	return Nutrients{
		Kcal:     math.Round(per.Kcal * quantity),
		ProteinG: round1(per.ProteinG * quantity),
		CarbsG:   round1(per.CarbsG * quantity),
		FatG:     round1(per.FatG * quantity),
	}
}

// SumTotals adds already-rounded entry totals and re-rounds to drop floating-point noise.
func SumTotals(items []Nutrients) Nutrients {
	var s Nutrients
	for _, n := range items {
		s.Kcal += n.Kcal
		s.ProteinG += n.ProteinG
		s.CarbsG += n.CarbsG
		s.FatG += n.FatG
	}
	return Nutrients{Kcal: math.Round(s.Kcal), ProteinG: round1(s.ProteinG), CarbsG: round1(s.CarbsG), FatG: round1(s.FatG)}
}

func BMR(b Body) float64 {
	s := -161.0
	if b.Gender == Male {
		s = 5
	}
	return 10*b.WeightKG + 6.25*b.HeightCM - 5*float64(b.Age) + s
}

func Recommend(b Body) Targets {
	adjust := 0.0
	switch diff := b.GoalWeightKG - b.WeightKG; {
	case diff <= -1:
		adjust = -500
	case diff >= 1:
		adjust = 300
	}
	cal := math.Max(SafeFloor(b.Gender), BMR(b)*activityFactors[b.ActivityLevel]+adjust)
	water := int(math.Ceil(b.WeightKG * 30 / GlassML))
	return Targets{
		CalorieTarget:      int(math.Round(cal/10)) * 10,
		ProteinTargetG:     int(math.Round(1.6 * math.Min(b.WeightKG, b.GoalWeightKG))),
		WaterTargetGlasses: min(16, max(6, water)),
	}
}
