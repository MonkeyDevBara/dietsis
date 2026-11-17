import { describe, expect, it } from 'vitest'
import type { ProfileInput } from '../types'
import { hasErrors, parseNumber, quantityError, validateCustomFood, validateProfile, validateWeight } from './validation'

const profile: ProfileInput = {
  display_name: 'Rina',
  gender: 'female',
  age: 30,
  height_cm: 160,
  weight_kg: 70,
  goal_weight_kg: 60,
  activity_level: 'moderate',
  calorie_target: 1650,
  protein_target_g: 96,
  water_target_glasses: 9,
  calorie_target_manual: false,
  protein_target_manual: false,
  water_target_manual: false,
}

describe('parseNumber', () => {
  it('accepts decimal comma and dot, rejects junk', () => {
    expect(parseNumber('72,5')).toBe(72.5)
    expect(parseNumber(' 72.5 ')).toBe(72.5)
    expect(parseNumber('')).toBeNaN()
    expect(parseNumber('12abc')).toBeNaN()
    expect(parseNumber('1e3')).toBeNaN()
  })
})

describe('validateProfile', () => {
  it('accepts a valid profile', () => {
    expect(hasErrors(validateProfile(profile))).toBe(false)
  })

  it('enforces D-012 limits at the boundaries', () => {
    expect(validateProfile({ ...profile, age: 17 }).age).toBeDefined()
    expect(validateProfile({ ...profile, age: 18 }).age).toBeUndefined()
    expect(validateProfile({ ...profile, age: 30.5 }).age).toBeDefined()
    expect(validateProfile({ ...profile, weight_kg: 29.9 }).weight_kg).toBeDefined()
    expect(validateProfile({ ...profile, calorie_target: 799 }).calorie_target).toBeDefined()
    expect(validateProfile({ ...profile, display_name: '   ' }).display_name).toBeDefined()
    expect(validateProfile({ ...profile, height_cm: Number.NaN }).height_cm).toBeDefined()
  })
})

describe('food validation', () => {
  it('requires calories and rejects negative macros', () => {
    const errs = validateCustomFood({ name: 'Smoothie', serving_label: '1 gelas', serving_grams: null, kcal: Number.NaN, protein_g: -1, carbs_g: 0, fat_g: 0 })
    expect(errs.kcal).toBeDefined()
    expect(errs.protein_g).toBeDefined()
    expect(errs.name).toBeUndefined()
  })

  it('limits quantity range and precision', () => {
    expect(quantityError(0.05)).toBeDefined()
    expect(quantityError(51)).toBeDefined()
    expect(quantityError(1.234)).toBeDefined()
    expect(quantityError(1.25)).toBeUndefined()
  })

  it('rejects control characters in names', () => {
    expect(validateCustomFood({ name: 'a\u0000b', serving_label: '1', serving_grams: null, kcal: 1, protein_g: 0, carbs_g: 0, fat_g: 0 }).name).toBeDefined()
  })
})

describe('validateWeight', () => {
  it('treats waist as optional but bounded', () => {
    expect(hasErrors(validateWeight({ weight_kg: 72.4, waist_cm: null }))).toBe(false)
    expect(validateWeight({ weight_kg: 72.4, waist_cm: 20 }).waist_cm).toBeDefined()
  })
})
