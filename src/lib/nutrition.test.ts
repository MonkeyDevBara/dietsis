import { describe, expect, it } from 'vitest'
import vectors from '../../server/internal/nutrition/testdata/vectors.json'
import type { BodyMetrics, Nutrients, ProfileInput, Targets } from '../types'
import { entryTotals, isBelowSafeFloor, recommendTargets, resolveTargets, sumTotals } from './nutrition'

// Same vectors as server/internal/nutrition/nutrition_test.go: Go and TS must agree exactly.
describe('shared vectors', () => {
  for (const c of vectors.targets as { name: string; input: BodyMetrics; expected: Targets }[]) {
    it(`recommends targets: ${c.name}`, () => {
      expect(recommendTargets(c.input)).toEqual(c.expected)
    })
  }
  for (const c of vectors.entry_totals as { name: string; per_serving: Nutrients; quantity: number; expected: Nutrients }[]) {
    it(`entry totals: ${c.name}`, () => {
      expect(entryTotals(c.per_serving, c.quantity)).toEqual(c.expected)
    })
  }
  for (const c of vectors.sum_totals as { name: string; items: Nutrients[]; expected: Nutrients }[]) {
    it(`sum totals: ${c.name}`, () => {
      expect(sumTotals(c.items)).toEqual(c.expected)
    })
  }
})

describe('resolveTargets', () => {
  const base: ProfileInput = {
    display_name: 'Rina',
    gender: 'female',
    age: 30,
    height_cm: 160,
    weight_kg: 70,
    goal_weight_kg: 60,
    activity_level: 'moderate',
    calorie_target: 1400,
    protein_target_g: 80,
    water_target_glasses: 12,
    calorie_target_manual: false,
    protein_target_manual: false,
    water_target_manual: false,
  }

  it('follows the recommendation for non-manual targets and ignores submitted values', () => {
    expect(resolveTargets(base)).toEqual({ calorie_target: 1650, protein_target_g: 96, water_target_glasses: 9 })
  })

  it('keeps manual targets, even below the safe floor', () => {
    const t = resolveTargets({ ...base, calorie_target_manual: true, calorie_target: 1000, water_target_manual: true })
    expect(t).toEqual({ calorie_target: 1000, protein_target_g: 96, water_target_glasses: 12 })
    expect(isBelowSafeFloor('female', 1000)).toBe(true)
    expect(isBelowSafeFloor('male', 1400)).toBe(true)
    expect(isBelowSafeFloor('female', 1200)).toBe(false)
  })
})
