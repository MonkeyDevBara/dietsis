// Domain types. Field names mirror the API contract (docs/API.md) exactly, so data from the
// server and from IndexedDB need no mapping layer.

export type Meal = 'breakfast' | 'lunch' | 'dinner' | 'snack'
export type Gender = 'male' | 'female'
export type ActivityLevel = 'sedentary' | 'light' | 'moderate' | 'active' | 'very_active'

export const MEALS: readonly Meal[] = ['breakfast', 'lunch', 'dinner', 'snack']
export const ACTIVITY_LEVELS: readonly ActivityLevel[] = ['sedentary', 'light', 'moderate', 'active', 'very_active']

export interface User {
  id: string
  email: string
  name: string
  picture: string
  created_at: number
}

export interface Nutrients {
  kcal: number
  protein_g: number
  carbs_g: number
  fat_g: number
}

/** Body data used by the target formulas (docs/DECISIONS.md D-011). */
export interface BodyMetrics {
  gender: Gender
  age: number
  height_cm: number
  weight_kg: number
  goal_weight_kg: number
  activity_level: ActivityLevel
}

export interface Targets {
  calorie_target: number
  protein_target_g: number
  water_target_glasses: number
}

/** Writable profile fields (PUT /profile, op profile.put). */
export interface ProfileInput extends BodyMetrics, Targets {
  display_name: string
  calorie_target_manual: boolean
  protein_target_manual: boolean
  water_target_manual: boolean
}

export interface Profile extends ProfileInput {
  recommended: Targets
  version: number
  created_at: number
  updated_at: number
}

export interface Favorite {
  food_id: string
  created_at: number
}

export interface Day {
  date: string
  water_glasses: number
  steps: number | null
  note: string
  version: number
  updated_at: number
}

export interface DayPatch {
  steps?: number | null
  note?: string
}

/** Writable food log fields. Nutrient values are PER SERVING (D-009). */
export interface FoodLogInput extends Nutrients {
  date: string
  meal: Meal
  food_id: string | null
  name: string
  serving_label: string
  serving_grams: number | null
  quantity: number
}

export type FoodLogPatch = Partial<Omit<FoodLogInput, 'food_id'>>

export interface FoodLog extends FoodLogInput {
  id: string
  totals: Nutrients
  version: number
  created_at: number
  updated_at: number
  deleted_at: number | null
}

export interface CustomFoodInput extends Nutrients {
  name: string
  serving_label: string
  serving_grams: number | null
}

export interface CustomFood extends CustomFoodInput {
  id: string
  version: number
  created_at: number
  updated_at: number
}

export interface Weight {
  date: string
  weight_kg: number
  waist_cm: number | null
  version: number
  updated_at: number
}

/** A food the user can pick: built-in catalog item, custom food, or a recent entry. Per serving. */
export interface FoodItem extends Nutrients {
  id: string // 'b:<slug>' | 'c:<uuid>'
  name: string
  serving_label: string
  serving_grams: number | null
  category: FoodCategory
  aliases?: readonly string[]
}

export type FoodCategory = 'pokok' | 'lauk' | 'sayur' | 'buah' | 'menu' | 'minuman' | 'lain' | 'kustom'
