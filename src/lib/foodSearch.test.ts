import { describe, expect, it } from 'vitest'
import { FOODS } from '../data/foods'
import { normalize, searchFoods } from './foodSearch'

describe('catalog integrity', () => {
  it('has unique ids and sane nutrient values', () => {
    const ids = FOODS.map((f) => f.id)
    expect(new Set(ids).size).toBe(ids.length)
    for (const f of FOODS) {
      expect(f.id).toMatch(/^b:[a-z0-9-]+$/)
      expect(f.kcal).toBeGreaterThanOrEqual(0)
      // Atwater sanity: macros explain calories within a generous margin (fiber, alcohol, rounding).
      const fromMacros = f.protein_g * 4 + f.carbs_g * 4 + f.fat_g * 9
      if (f.kcal >= 50) expect(Math.abs(fromMacros - f.kcal) / f.kcal, f.name).toBeLessThan(0.35)
    }
  })
})

describe('searchFoods', () => {
  it('ignores case, punctuation and diacritics', () => {
    expect(normalize('Gado-Gado!')).toBe('gado gado')
    expect(searchFoods(FOODS, 'GADO gado')[0]?.id).toBe('b:gado-gado')
  })

  it('ranks name-prefix matches above word matches', () => {
    const ids = searchFoods(FOODS, 'nasi').map((f) => f.id)
    expect(ids[0]).toBe('b:nasi-putih')
    expect(ids.indexOf('b:nasi-goreng')).toBeLessThan(ids.indexOf('b:nasi-padang') + 1)
  })

  it('matches aliases and requires every token', () => {
    expect(searchFoods(FOODS, 'chicken breast').map((f) => f.id)).toContain('b:dada-ayam-rebus')
    expect(searchFoods(FOODS, 'nasi pizza')).toEqual([])
  })

  it('returns the list unchanged for an empty query', () => {
    expect(searchFoods(FOODS, '  ', 5)).toHaveLength(5)
  })
})
