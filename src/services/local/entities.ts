// Maps ops and server changes onto LocalState slots, so the sync layer can read and replace one
// entity at a time. Entity names equal the server's change_log.entity values.

import type { CustomFood, Day, Favorite, FoodLog, Profile, Weight } from '../../types'
import type { LocalState, Op } from './state'

export type EntityName = 'profile' | 'favorite' | 'day' | 'food_log' | 'custom_food' | 'weight'

export interface EntityRef {
  entity: EntityName
  key: string
}

export const refId = (r: EntityRef): string => `${r.entity}|${r.key}`

export function refOf(op: Op): EntityRef {
  switch (op.type) {
    case 'profile.put':
      return { entity: 'profile', key: 'profile' }
    case 'favorite.set':
      return { entity: 'favorite', key: op.payload.food_id }
    case 'day.water':
    case 'day.update':
      return { entity: 'day', key: op.payload.date }
    case 'food_log.create':
    case 'food_log.update':
    case 'food_log.delete':
    case 'food_log.restore':
      return { entity: 'food_log', key: op.payload.id }
    case 'custom_food.create':
      return { entity: 'custom_food', key: op.payload.id }
    case 'weight.put':
    case 'weight.delete':
      return { entity: 'weight', key: op.payload.date }
  }
}

export function getValue(s: LocalState, r: EntityRef): unknown {
  switch (r.entity) {
    case 'profile':
      return s.profile
    case 'favorite':
      return s.favorites[r.key] ?? null
    case 'day':
      return s.days[r.key] ?? null
    case 'food_log':
      return s.foodLogs[r.key] ?? null
    case 'custom_food':
      return s.customFoods[r.key] ?? null
    case 'weight':
      return s.weights[r.key] ?? null
  }
}

function setIn<T>(map: Readonly<Record<string, T>>, key: string, value: T | null): Record<string, T> {
  if (value === null) return Object.fromEntries(Object.entries(map).filter(([k]) => k !== key))
  return { ...map, [key]: value }
}

/** Returns a new state with one entity replaced (null removes it). Values come from the server or the reducer, so they have the API shape. */
export function setValue(s: LocalState, r: EntityRef, value: unknown): LocalState {
  switch (r.entity) {
    case 'profile':
      return { ...s, profile: value as Profile | null }
    case 'favorite':
      return { ...s, favorites: setIn(s.favorites, r.key, value as Favorite | null) }
    case 'day':
      return { ...s, days: setIn(s.days, r.key, value as Day | null) }
    case 'food_log':
      return { ...s, foodLogs: setIn(s.foodLogs, r.key, value as FoodLog | null) }
    case 'custom_food':
      return { ...s, customFoods: setIn(s.customFoods, r.key, value as CustomFood | null) }
    case 'weight':
      return { ...s, weights: setIn(s.weights, r.key, value as Weight | null) }
  }
}

export function versionOf(server: unknown): number | undefined {
  return server && typeof server === 'object' && 'version' in server ? (server as { version: number }).version : undefined
}
