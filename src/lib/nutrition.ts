// Nutrition math shared in spirit with server/internal/nutrition (Go). Both implementations are
// checked against server/internal/nutrition/testdata/vectors.json, so results are identical.
// Formulas and rounding rules: docs/DECISIONS.md D-010 and D-011. Estimates, not medical advice.

import type { ActivityLevel, BodyMetrics, Gender, Nutrients, ProfileInput, Targets } from '../types'

export const ACTIVITY_FACTORS: Record<ActivityLevel, number> = {
  sedentary: 1.2,
  light: 1.375,
  moderate: 1.55,
  active: 1.725,
  very_active: 1.9,
}

/** General safe lower bound for a *suggested* daily target (manual targets may go lower, with a warning). */
export const SAFE_CALORIE_FLOOR: Record<Gender, number> = { female: 1200, male: 1500 }

export const GLASS_ML = 250

/** Half away from zero to 1 decimal. Equivalent to Go math.Round(x*10)/10 for non-negative x. */
export function round1(x: number): number {
  return Math.round(x * 10) / 10
}

export function entryTotals(perServing: Nutrients, quantity: number): Nutrients {
  return {
    kcal: Math.round(perServing.kcal * quantity),
    protein_g: round1(perServing.protein_g * quantity),
    carbs_g: round1(perServing.carbs_g * quantity),
    fat_g: round1(perServing.fat_g * quantity),
  }
}

export const ZERO_NUTRIENTS: Nutrients = { kcal: 0, protein_g: 0, carbs_g: 0, fat_g: 0 }

/** Sums already-rounded entry totals, then re-rounds to remove floating-point noise. */
export function sumTotals(items: readonly Nutrients[]): Nutrients {
  let kcal = 0
  let protein = 0
  let carbs = 0
  let fat = 0
  for (const n of items) {
    kcal += n.kcal
    protein += n.protein_g
    carbs += n.carbs_g
    fat += n.fat_g
  }
  return { kcal: Math.round(kcal), protein_g: round1(protein), carbs_g: round1(carbs), fat_g: round1(fat) }
}

/** Mifflin-St Jeor basal metabolic rate (kcal/day), unrounded. */
export function bmr(b: Pick<BodyMetrics, 'gender' | 'age' | 'height_cm' | 'weight_kg'>): number {
  const s = b.gender === 'male' ? 5 : -161
  return 10 * b.weight_kg + 6.25 * b.height_cm - 5 * b.age + s
}

export function tdee(b: BodyMetrics): number {
  return bmr(b) * ACTIVITY_FACTORS[b.activity_level]
}

export type GoalDirection = 'lose' | 'maintain' | 'gain'

export function goalDirection(b: Pick<BodyMetrics, 'weight_kg' | 'goal_weight_kg'>): GoalDirection {
  const diff = b.goal_weight_kg - b.weight_kg
  if (diff <= -1) return 'lose'
  if (diff >= 1) return 'gain'
  return 'maintain'
}

export function recommendTargets(b: BodyMetrics): Targets {
  const direction = goalDirection(b)
  const adjust = direction === 'lose' ? -500 : direction === 'gain' ? 300 : 0
  const calories = Math.max(SAFE_CALORIE_FLOOR[b.gender], tdee(b) + adjust)
  const water = Math.ceil((b.weight_kg * 30) / GLASS_ML)
  return {
    calorie_target: Math.round(calories / 10) * 10,
    protein_target_g: Math.round(1.6 * Math.min(b.weight_kg, b.goal_weight_kg)),
    water_target_glasses: Math.min(16, Math.max(6, water)),
  }
}

/** Targets the profile actually uses: manual values are kept, the rest follow the recommendation. */
export function resolveTargets(input: ProfileInput): Targets {
  const rec = recommendTargets(input)
  return {
    calorie_target: input.calorie_target_manual ? input.calorie_target : rec.calorie_target,
    protein_target_g: input.protein_target_manual ? input.protein_target_g : rec.protein_target_g,
    water_target_glasses: input.water_target_manual ? input.water_target_glasses : rec.water_target_glasses,
  }
}

export function isBelowSafeFloor(gender: Gender, calories: number): boolean {
  return calories < SAFE_CALORIE_FLOOR[gender]
}
