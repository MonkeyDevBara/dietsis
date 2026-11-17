// Input limits from docs/DECISIONS.md D-012. The Go server (internal/validate) enforces the same
// limits; the client validates first so forms can explain problems before anything is saved.

import { ACTIVITY_LEVELS, MEALS } from '../types'
import type { CustomFoodInput, DayPatch, FoodLogInput, ProfileInput } from '../types'
import { isDateKey } from './date'

export type FieldErrors<T> = Partial<Record<keyof T, string>>

export const LIMITS = {
  display_name: { min: 1, max: 60 },
  age: { min: 18, max: 100 },
  height_cm: { min: 100, max: 250 },
  weight_kg: { min: 30, max: 300 },
  waist_cm: { min: 30, max: 250 },
  calorie_target: { min: 800, max: 6000 },
  protein_target_g: { min: 10, max: 400 },
  water_target_glasses: { min: 1, max: 30 },
  food_name: { min: 1, max: 80 },
  serving_label: { min: 1, max: 40 },
  serving_grams: { min: 0.1, max: 5000 },
  kcal: { min: 0, max: 5000 },
  macro_g: { min: 0, max: 500 },
  quantity: { min: 0.1, max: 50 },
  steps: { min: 0, max: 200000 },
  note: { max: 500 },
  water_glasses: { min: 0, max: 100 },
} as const

const fmt = new Intl.NumberFormat('id-ID', { maximumFractionDigits: 1 })

function range(value: number, min: number, max: number, integer = false): string | undefined {
  if (!Number.isFinite(value)) return 'Wajib diisi angka'
  if (integer && !Number.isInteger(value)) return 'Harus bilangan bulat'
  if (value < min || value > max) return `Harus antara ${fmt.format(min)} dan ${fmt.format(max)}`
  return undefined
}

// eslint-disable-next-line no-control-regex
const CONTROL_RE = /[\u0000-\u0008\u000B\u000C\u000E-\u001F\u007F]/

function text(value: string, min: number, max: number, allowNewlines = false): string | undefined {
  const t = value.trim()
  if (t.length < min) return 'Wajib diisi'
  if (Array.from(t).length > max) return `Maksimal ${max} karakter`
  if (CONTROL_RE.test(t) || (!allowNewlines && /[\n\r\t]/.test(t))) return 'Mengandung karakter tidak valid'
  return undefined
}

function collect<T>(entries: readonly (readonly [keyof T, string | undefined])[]): FieldErrors<T> {
  const out: FieldErrors<T> = {}
  for (const [k, v] of entries) if (v) out[k] = v
  return out
}

export function hasErrors(errors: object): boolean {
  return Object.keys(errors).length > 0
}

export function validateProfile(p: ProfileInput): FieldErrors<ProfileInput> {
  const L = LIMITS
  return collect<ProfileInput>([
    ['display_name', text(p.display_name, L.display_name.min, L.display_name.max)],
    ['gender', (['male', 'female'] as readonly string[]).includes(p.gender) ? undefined : 'Pilih salah satu'],
    ['age', range(p.age, L.age.min, L.age.max, true)],
    ['height_cm', range(p.height_cm, L.height_cm.min, L.height_cm.max)],
    ['weight_kg', range(p.weight_kg, L.weight_kg.min, L.weight_kg.max)],
    ['goal_weight_kg', range(p.goal_weight_kg, L.weight_kg.min, L.weight_kg.max)],
    ['activity_level', ACTIVITY_LEVELS.includes(p.activity_level) ? undefined : 'Pilih salah satu'],
    ['calorie_target', range(p.calorie_target, L.calorie_target.min, L.calorie_target.max, true)],
    ['protein_target_g', range(p.protein_target_g, L.protein_target_g.min, L.protein_target_g.max, true)],
    ['water_target_glasses', range(p.water_target_glasses, L.water_target_glasses.min, L.water_target_glasses.max, true)],
  ])
}

function nutrients(f: CustomFoodInput | FoodLogInput) {
  const L = LIMITS
  return [
    ['name', text(f.name, L.food_name.min, L.food_name.max)],
    ['serving_label', text(f.serving_label, L.serving_label.min, L.serving_label.max)],
    ['serving_grams', f.serving_grams === null ? undefined : range(f.serving_grams, L.serving_grams.min, L.serving_grams.max)],
    ['kcal', range(f.kcal, L.kcal.min, L.kcal.max)],
    ['protein_g', range(f.protein_g, L.macro_g.min, L.macro_g.max)],
    ['carbs_g', range(f.carbs_g, L.macro_g.min, L.macro_g.max)],
    ['fat_g', range(f.fat_g, L.macro_g.min, L.macro_g.max)],
  ] as const
}

export function validateCustomFood(f: CustomFoodInput): FieldErrors<CustomFoodInput> {
  return collect<CustomFoodInput>(nutrients(f))
}

export function quantityError(q: number): string | undefined {
  const err = range(q, LIMITS.quantity.min, LIMITS.quantity.max)
  if (err) return err
  // Tolerance: 1.15 * 100 is 114.99999999999999, which is still a valid 2-decimal value.
  if (Math.abs(q * 100 - Math.round(q * 100)) > 1e-9) return 'Maksimal 2 angka desimal'
  return undefined
}

export function validateFoodLog(f: FoodLogInput): FieldErrors<FoodLogInput> {
  return collect<FoodLogInput>([
    ...nutrients(f),
    ['date', isDateKey(f.date) ? undefined : 'Tanggal tidak valid'],
    ['meal', MEALS.includes(f.meal) ? undefined : 'Pilih waktu makan'],
    ['quantity', quantityError(f.quantity)],
  ])
}

export function validateWeight(w: { weight_kg: number; waist_cm: number | null }): FieldErrors<{ weight_kg: number; waist_cm: number }> {
  return collect<{ weight_kg: number; waist_cm: number }>([
    ['weight_kg', range(w.weight_kg, LIMITS.weight_kg.min, LIMITS.weight_kg.max)],
    ['waist_cm', w.waist_cm === null ? undefined : range(w.waist_cm, LIMITS.waist_cm.min, LIMITS.waist_cm.max)],
  ])
}

export function validateDayPatch(p: DayPatch): FieldErrors<DayPatch> {
  return collect<DayPatch>([
    ['steps', p.steps === undefined || p.steps === null ? undefined : range(p.steps, LIMITS.steps.min, LIMITS.steps.max, true)],
    ['note', p.note === undefined ? undefined : p.note.trim().length > LIMITS.note.max ? `Maksimal ${LIMITS.note.max} karakter` : text(p.note, 0, LIMITS.note.max, true)],
  ])
}

/** Parses user-typed numbers; accepts Indonesian decimal comma ("72,5"). Empty → NaN. */
export function parseNumber(raw: string): number {
  const t = raw.trim().replace(/\s/g, '').replace(',', '.')
  if (t === '' || !/^-?\d*\.?\d+$/.test(t)) return Number.NaN
  return Number(t)
}
