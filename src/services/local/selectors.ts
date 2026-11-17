// Pure derived views over LocalState. Kept free of React so they are unit-testable and reusable.

import { catalogFood, FOODS } from '../../data/foods'
import { addDays } from '../../lib/date'
import { sumTotals } from '../../lib/nutrition'
import { MEALS } from '../../types'
import type { CustomFood, Day, FoodItem, FoodLog, Meal, Nutrients, Profile, Weight } from '../../types'
import { emptyDay } from './state'
import type { LocalState } from './state'

export function dayOrDefault(state: LocalState, date: string): Day {
  return state.days[date] ?? emptyDay(date)
}

export function logsForDate(state: LocalState, date: string): FoodLog[] {
  return Object.values(state.foodLogs)
    .filter((l) => l.date === date && l.deleted_at === null)
    .sort((a, b) => a.created_at - b.created_at || a.id.localeCompare(b.id))
}

export function groupByMeal(logs: readonly FoodLog[]): Record<Meal, FoodLog[]> {
  const out = Object.fromEntries(MEALS.map((m) => [m, [] as FoodLog[]])) as Record<Meal, FoodLog[]>
  for (const l of logs) out[l.meal].push(l)
  return out
}

export function totalsOf(logs: readonly FoodLog[]): Nutrients {
  return sumTotals(logs.map((l) => l.totals))
}

export function customFoodItem(f: CustomFood): FoodItem {
  return {
    id: `c:${f.id}`,
    name: f.name,
    serving_label: f.serving_label,
    serving_grams: f.serving_grams,
    kcal: f.kcal,
    protein_g: f.protein_g,
    carbs_g: f.carbs_g,
    fat_g: f.fat_g,
    category: 'kustom',
  }
}

export function customFoodItems(state: LocalState): FoodItem[] {
  return Object.values(state.customFoods)
    .sort((a, b) => a.name.localeCompare(b.name, 'id'))
    .map(customFoodItem)
}

/** Custom foods first (the user made them on purpose), then the built-in catalog. */
export function allFoods(state: LocalState): FoodItem[] {
  return [...customFoodItems(state), ...FOODS]
}

export function resolveFood(state: LocalState, foodId: string): FoodItem | undefined {
  if (foodId.startsWith('c:')) {
    const f = state.customFoods[foodId.slice(2)]
    return f ? customFoodItem(f) : undefined
  }
  return catalogFood(foodId)
}

export function favoriteFoods(state: LocalState): FoodItem[] {
  return Object.values(state.favorites)
    .sort((a, b) => b.created_at - a.created_at)
    .map((f) => resolveFood(state, f.food_id))
    .filter((f): f is FoodItem => f !== undefined)
}

export interface RecentFood {
  food: FoodItem
  quantity: number // last quantity used, for one-tap re-logging
  meal: Meal
}

/** Distinct foods from the most recent logs. Uses the logged snapshot, so it works even if a catalog item changes. */
export function recentFoods(state: LocalState, limit = 15): RecentFood[] {
  const seen = new Set<string>()
  const out: RecentFood[] = []
  const logs = Object.values(state.foodLogs)
    .filter((l) => l.deleted_at === null && l.food_id !== null)
    .sort((a, b) => b.created_at - a.created_at)
  for (const l of logs) {
    const id = l.food_id as string
    if (seen.has(id)) continue
    seen.add(id)
    out.push({
      food: {
        id,
        name: l.name,
        serving_label: l.serving_label,
        serving_grams: l.serving_grams,
        kcal: l.kcal,
        protein_g: l.protein_g,
        carbs_g: l.carbs_g,
        fat_g: l.fat_g,
        category: id.startsWith('c:') ? 'kustom' : (catalogFood(id)?.category ?? 'lain'),
      },
      quantity: l.quantity,
      meal: l.meal,
    })
    if (out.length >= limit) break
  }
  return out
}

export function sortedWeights(state: LocalState): Weight[] {
  return Object.values(state.weights).sort((a, b) => a.date.localeCompare(b.date))
}

export interface WeightSummary {
  start: number | null // first logged weight (or profile weight)
  current: number | null // latest logged weight (or profile weight)
  currentDate: string | null
  change: number | null // current - start
  goal: number | null
  remaining: number | null // goal - current
  /** 0..1 progress from start toward goal; null when start == goal or unknown. */
  progress: number | null
  latestWaist: number | null
}

export function weightSummary(weights: readonly Weight[], profile: Profile | null): WeightSummary {
  const first = weights[0]
  const last = weights[weights.length - 1]
  const start = first?.weight_kg ?? profile?.weight_kg ?? null
  const current = last?.weight_kg ?? profile?.weight_kg ?? null
  const goal = profile?.goal_weight_kg ?? null
  const r1 = (x: number) => Math.round(x * 10) / 10
  let progress: number | null = null
  if (start !== null && current !== null && goal !== null && start !== goal) {
    progress = Math.min(1, Math.max(0, (start - current) / (start - goal)))
  }
  const waist = [...weights].reverse().find((w) => w.waist_cm !== null)?.waist_cm ?? null
  return {
    start,
    current,
    currentDate: last?.date ?? null,
    change: start !== null && current !== null ? r1(current - start) : null,
    goal,
    remaining: goal !== null && current !== null ? r1(goal - current) : null,
    progress,
    latestWaist: waist,
  }
}

export function waterHistory(state: LocalState, endDate: string, days: number): { date: string; glasses: number }[] {
  const out: { date: string; glasses: number }[] = []
  for (let i = days - 1; i >= 0; i--) {
    const date = addDays(endDate, -i)
    out.push({ date, glasses: state.days[date]?.water_glasses ?? 0 })
  }
  return out
}

/** Meal suggested by local time of day: <10 breakfast, <15 lunch, <18 snack, else dinner. */
export function mealForTime(date: Date): Meal {
  const h = date.getHours()
  if (h < 10) return 'breakfast'
  if (h < 15) return 'lunch'
  if (h < 18) return 'snack'
  return 'dinner'
}

export interface WeekSummary {
  daysLogged: number
  avgKcal: number | null // average over days that have food logged (a missed day is not "fasting")
  daysOnTarget: number // days with 80-105% of the calorie target
}

/** Last 7 days ending at endDate (inclusive). */
export function weekSummary(state: LocalState, endDate: string, calorieTarget: number): WeekSummary {
  let logged = 0
  let sum = 0
  let onTarget = 0
  for (let i = 0; i < 7; i++) {
    const kcal = totalsOf(logsForDate(state, addDays(endDate, -i))).kcal
    if (kcal === 0) continue
    logged++
    sum += kcal
    if (kcal >= calorieTarget * 0.8 && kcal <= calorieTarget * 1.05) onTarget++
  }
  return { daysLogged: logged, avgKcal: logged > 0 ? Math.round(sum / logged) : null, daysOnTarget: onTarget }
}
