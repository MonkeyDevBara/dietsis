// Local data model and the pure reducer that applies an operation to it.
//
// Every user mutation is expressed as an Op whose `type`/`payload` match the sync protocol
// (docs/API.md → POST /sync/push). The same reducer gives the optimistic local result now (M1, in
// memory) and later (M6) runs over the IndexedDB-backed state before the op is queued for the server.

import { entryTotals, recommendTargets, resolveTargets } from '../../lib/nutrition'
import type {
  CustomFood,
  CustomFoodInput,
  Day,
  DayPatch,
  Favorite,
  FoodLog,
  FoodLogInput,
  FoodLogPatch,
  Profile,
  ProfileInput,
  Weight,
} from '../../types'

export interface LocalState {
  profile: Profile | null
  favorites: Readonly<Record<string, Favorite>> // by food_id
  days: Readonly<Record<string, Day>> // by date
  foodLogs: Readonly<Record<string, FoodLog>> // by id
  customFoods: Readonly<Record<string, CustomFood>> // by id
  weights: Readonly<Record<string, Weight>> // by date
}

export const EMPTY_STATE: LocalState = {
  profile: null,
  favorites: {},
  days: {},
  foodLogs: {},
  customFoods: {},
  weights: {},
}

export type Op =
  | { type: 'profile.put'; payload: ProfileInput }
  | { type: 'favorite.set'; payload: { food_id: string; favorite: boolean } }
  | { type: 'day.water'; payload: { date: string; delta: number } }
  | { type: 'day.update'; payload: { date: string; patch: DayPatch } }
  | { type: 'food_log.create'; payload: FoodLogInput & { id: string } }
  | { type: 'food_log.update'; payload: { id: string; patch: FoodLogPatch } }
  | { type: 'food_log.delete'; payload: { id: string } }
  | { type: 'food_log.restore'; payload: { id: string } }
  | { type: 'custom_food.create'; payload: CustomFoodInput & { id: string } }
  | { type: 'weight.put'; payload: { date: string; weight_kg: number; waist_cm: number | null } }
  | { type: 'weight.delete'; payload: { date: string } }

export const WATER_MAX = 100

export function emptyDay(date: string): Day {
  return { date, water_glasses: 0, steps: null, note: '', version: 0, updated_at: 0 }
}

function put<T>(map: Readonly<Record<string, T>>, key: string, value: T): Record<string, T> {
  return { ...map, [key]: value }
}

function without<T>(map: Readonly<Record<string, T>>, key: string): Record<string, T> {
  return Object.fromEntries(Object.entries(map).filter(([k]) => k !== key))
}

/** Applies one operation. Returns the same object when the op is a no-op (callers can skip notifying). */
export function applyOp(state: LocalState, op: Op, now: number): LocalState {
  switch (op.type) {
    case 'profile.put': {
      const prev = state.profile
      const input = op.payload
      const profile: Profile = {
        ...input,
        display_name: input.display_name.trim(),
        ...resolveTargets(input),
        recommended: recommendTargets(input),
        version: (prev?.version ?? 0) + 1,
        created_at: prev?.created_at ?? now,
        updated_at: now,
      }
      return { ...state, profile }
    }

    case 'favorite.set': {
      const { food_id, favorite } = op.payload
      const exists = food_id in state.favorites
      if (favorite === exists) return state
      return {
        ...state,
        favorites: favorite ? put(state.favorites, food_id, { food_id, created_at: now }) : without(state.favorites, food_id),
      }
    }

    case 'day.water': {
      const { date, delta } = op.payload
      const day = state.days[date] ?? emptyDay(date)
      const water = Math.min(WATER_MAX, Math.max(0, day.water_glasses + delta))
      if (water === day.water_glasses) return state
      // Water is a counter outside versioning (docs/OFFLINE_SYNC.md), so version is untouched.
      return { ...state, days: put(state.days, date, { ...day, water_glasses: water, updated_at: now }) }
    }

    case 'day.update': {
      const { date, patch } = op.payload
      const day = state.days[date] ?? emptyDay(date)
      const next: Day = {
        ...day,
        ...(patch.steps !== undefined ? { steps: patch.steps } : {}),
        ...(patch.note !== undefined ? { note: patch.note.trim() } : {}),
        version: day.version + 1,
        updated_at: now,
      }
      return { ...state, days: put(state.days, date, next) }
    }

    case 'food_log.create': {
      const input = op.payload
      if (input.id in state.foodLogs) return state // idempotent
      const log: FoodLog = {
        ...input,
        name: input.name.trim(),
        serving_label: input.serving_label.trim(),
        totals: entryTotals(input, input.quantity),
        version: 1,
        created_at: now,
        updated_at: now,
        deleted_at: null,
      }
      return { ...state, foodLogs: put(state.foodLogs, log.id, log) }
    }

    case 'food_log.update': {
      const prev = state.foodLogs[op.payload.id]
      if (!prev || prev.deleted_at !== null) return state
      const merged = { ...prev, ...op.payload.patch }
      const log: FoodLog = { ...merged, totals: entryTotals(merged, merged.quantity), version: prev.version + 1, updated_at: now }
      return { ...state, foodLogs: put(state.foodLogs, log.id, log) }
    }

    case 'food_log.delete':
    case 'food_log.restore': {
      const prev = state.foodLogs[op.payload.id]
      const deleting = op.type === 'food_log.delete'
      if (!prev || (prev.deleted_at !== null) === deleting) return state
      const log: FoodLog = { ...prev, deleted_at: deleting ? now : null, version: prev.version + 1, updated_at: now }
      return { ...state, foodLogs: put(state.foodLogs, log.id, log) }
    }

    case 'custom_food.create': {
      const input = op.payload
      if (input.id in state.customFoods) return state
      const food: CustomFood = {
        ...input,
        name: input.name.trim(),
        serving_label: input.serving_label.trim(),
        version: 1,
        created_at: now,
        updated_at: now,
      }
      return { ...state, customFoods: put(state.customFoods, food.id, food) }
    }

    case 'weight.put': {
      const { date, weight_kg, waist_cm } = op.payload
      const prev = state.weights[date]
      const weight: Weight = { date, weight_kg, waist_cm, version: (prev?.version ?? 0) + 1, updated_at: now }
      return { ...state, weights: put(state.weights, date, weight) }
    }

    case 'weight.delete': {
      if (!(op.payload.date in state.weights)) return state
      return { ...state, weights: without(state.weights, op.payload.date) }
    }
  }
}
