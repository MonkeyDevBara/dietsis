import { describe, expect, it } from 'vitest'
import type { FoodLogInput, ProfileInput } from '../../types'
import { dayOrDefault, groupByMeal, logsForDate, mealForTime, recentFoods, totalsOf, weekSummary, weightSummary } from './selectors'
import { applyOp, EMPTY_STATE } from './state'
import type { LocalState, Op } from './state'

const T = 1_700_000_000_000
const run = (ops: Op[], from: LocalState = EMPTY_STATE) => ops.reduce((s, op, i) => applyOp(s, op, T + i), from)

const nasi: FoodLogInput & { id: string } = {
  id: 'log-1',
  date: '2026-11-17',
  meal: 'lunch',
  food_id: 'b:nasi-putih',
  name: 'Nasi putih',
  serving_label: '1 porsi (150 g)',
  serving_grams: 150,
  quantity: 1.5,
  kcal: 195,
  protein_g: 4,
  carbs_g: 42,
  fat_g: 0.5,
}

describe('food logs', () => {
  it('computes totals from per-serving values and quantity', () => {
    const s = run([{ type: 'food_log.create', payload: nasi }])
    expect(s.foodLogs['log-1']?.totals).toEqual({ kcal: 293, protein_g: 6, carbs_g: 63, fat_g: 0.8 })
  })

  it('is idempotent on create with the same id', () => {
    const once = run([{ type: 'food_log.create', payload: nasi }])
    const twice = applyOp(once, { type: 'food_log.create', payload: { ...nasi, quantity: 9 } }, T + 99)
    expect(twice).toBe(once)
  })

  it('edits quantity without drift across repeated edits', () => {
    let s = run([{ type: 'food_log.create', payload: nasi }])
    for (const q of [2, 0.5, 3, 1.5]) s = applyOp(s, { type: 'food_log.update', payload: { id: 'log-1', patch: { quantity: q } } }, T + 10)
    expect(s.foodLogs['log-1']?.totals.kcal).toBe(293)
    expect(s.foodLogs['log-1']?.version).toBe(5)
  })

  it('soft-deletes, hides from the day, and restores', () => {
    const created = run([{ type: 'food_log.create', payload: nasi }])
    const deleted = applyOp(created, { type: 'food_log.delete', payload: { id: 'log-1' } }, T + 5)
    expect(logsForDate(deleted, '2026-11-17')).toHaveLength(0)
    expect(deleted.foodLogs['log-1']?.deleted_at).toBe(T + 5)
    expect(applyOp(deleted, { type: 'food_log.delete', payload: { id: 'log-1' } }, T + 6)).toBe(deleted)

    const restored = applyOp(deleted, { type: 'food_log.restore', payload: { id: 'log-1' } }, T + 7)
    expect(logsForDate(restored, '2026-11-17')).toHaveLength(1)
    expect(restored.foodLogs['log-1']?.deleted_at).toBeNull()
  })

  it('ignores updates to deleted or unknown entries', () => {
    const deleted = run([
      { type: 'food_log.create', payload: nasi },
      { type: 'food_log.delete', payload: { id: 'log-1' } },
    ])
    expect(applyOp(deleted, { type: 'food_log.update', payload: { id: 'log-1', patch: { quantity: 3 } } }, T + 9)).toBe(deleted)
    expect(applyOp(deleted, { type: 'food_log.update', payload: { id: 'nope', patch: { quantity: 3 } } }, T + 9)).toBe(deleted)
  })

  it('groups by meal and sums the day', () => {
    const s = run([
      { type: 'food_log.create', payload: nasi },
      { type: 'food_log.create', payload: { ...nasi, id: 'log-2', meal: 'dinner', quantity: 1 } },
    ])
    const logs = logsForDate(s, '2026-11-17')
    expect(groupByMeal(logs).lunch).toHaveLength(1)
    expect(groupByMeal(logs).dinner).toHaveLength(1)
    expect(totalsOf(logs).kcal).toBe(293 + 195)
  })
})

describe('water', () => {
  it('adds and clamps at 0 and 100', () => {
    let s = run([{ type: 'day.water', payload: { date: '2026-11-17', delta: 3 } }])
    expect(dayOrDefault(s, '2026-11-17').water_glasses).toBe(3)
    s = applyOp(s, { type: 'day.water', payload: { date: '2026-11-17', delta: -10 } }, T)
    expect(dayOrDefault(s, '2026-11-17').water_glasses).toBe(0)
    const unchanged = applyOp(s, { type: 'day.water', payload: { date: '2026-11-17', delta: -1 } }, T)
    expect(unchanged).toBe(s)
    s = applyOp(s, { type: 'day.water', payload: { date: '2026-11-17', delta: 20 } }, T)
    for (let i = 0; i < 5; i++) s = applyOp(s, { type: 'day.water', payload: { date: '2026-11-17', delta: 20 } }, T)
    expect(dayOrDefault(s, '2026-11-17').water_glasses).toBe(100)
  })
})

describe('weights and favorites', () => {
  it('keeps one entry per date and replaces on re-put', () => {
    const s = run([
      { type: 'weight.put', payload: { date: '2026-11-17', weight_kg: 72, waist_cm: null } },
      { type: 'weight.put', payload: { date: '2026-11-17', weight_kg: 71.5, waist_cm: 80 } },
    ])
    expect(Object.keys(s.weights)).toEqual(['2026-11-17'])
    expect(s.weights['2026-11-17']).toMatchObject({ weight_kg: 71.5, waist_cm: 80, version: 2 })
  })

  it('toggles favorites idempotently', () => {
    const on = run([{ type: 'favorite.set', payload: { food_id: 'b:pisang', favorite: true } }])
    expect(applyOp(on, { type: 'favorite.set', payload: { food_id: 'b:pisang', favorite: true } }, T)).toBe(on)
    expect(applyOp(on, { type: 'favorite.set', payload: { food_id: 'b:pisang', favorite: false } }, T).favorites).toEqual({})
  })

  it('summarises change, goal progress and latest waist', () => {
    const s = run([
      { type: 'weight.put', payload: { date: '2026-11-01', weight_kg: 74, waist_cm: 84 } },
      { type: 'weight.put', payload: { date: '2026-11-10', weight_kg: 72, waist_cm: null } },
    ])
    const profile = { goal_weight_kg: 70, weight_kg: 74 } as Parameters<typeof weightSummary>[1]
    const sum = weightSummary(Object.values(s.weights).sort((a, b) => a.date.localeCompare(b.date)), profile)
    expect(sum).toMatchObject({ start: 74, current: 72, change: -2, remaining: -2, progress: 0.5, latestWaist: 84 })
  })
})

describe('profile', () => {
  const input: ProfileInput = {
    display_name: ' Rina ',
    gender: 'female',
    age: 30,
    height_cm: 160,
    weight_kg: 70,
    goal_weight_kg: 60,
    activity_level: 'moderate',
    calorie_target: 1000,
    protein_target_g: 50,
    water_target_glasses: 4,
    calorie_target_manual: true,
    protein_target_manual: false,
    water_target_manual: false,
  }

  it('resolves targets: manual kept, others recomputed; version increments', () => {
    const s1 = run([{ type: 'profile.put', payload: input }])
    expect(s1.profile).toMatchObject({ display_name: 'Rina', calorie_target: 1000, protein_target_g: 96, water_target_glasses: 9, version: 1 })
    expect(s1.profile?.recommended.calorie_target).toBe(1650)
    const s2 = applyOp(s1, { type: 'profile.put', payload: { ...input, weight_kg: 68 } }, T + 50)
    expect(s2.profile?.version).toBe(2)
    expect(s2.profile?.created_at).toBe(s1.profile?.created_at)
  })
})

describe('recent foods and week summary', () => {
  it('lists distinct foods newest-first with the last quantity', () => {
    const s = run([
      { type: 'food_log.create', payload: nasi },
      { type: 'food_log.create', payload: { ...nasi, id: 'log-2', quantity: 2 } },
      { type: 'food_log.create', payload: { ...nasi, id: 'log-3', food_id: 'b:pisang', name: 'Pisang', quantity: 1 } },
      { type: 'food_log.create', payload: { ...nasi, id: 'log-4', food_id: null, name: 'Manual' } },
    ])
    const recent = recentFoods(s)
    expect(recent.map((r) => r.food.id)).toEqual(['b:pisang', 'b:nasi-putih'])
    expect(recent[1]?.quantity).toBe(2)
  })

  it('counts only logged days and the 80-105% band', () => {
    const day = (id: string, date: string, kcal: number): Op => ({ type: 'food_log.create', payload: { ...nasi, id, date, quantity: 1, kcal } })
    const s = run([day('a', '2026-11-17', 1600), day('b', '2026-11-16', 1000), day('c', '2026-11-15', 1900), day('d', '2026-11-01', 1600)])
    expect(weekSummary(s, '2026-11-17', 1650)).toEqual({ daysLogged: 3, avgKcal: 1500, daysOnTarget: 1 })
  })
})

describe('mealForTime', () => {
  it('maps hours to meals', () => {
    const at = (h: number) => mealForTime(new Date(2026, 10, 17, h))
    expect([at(7), at(12), at(16), at(20)]).toEqual(['breakfast', 'lunch', 'snack', 'dinner'])
  })
})
