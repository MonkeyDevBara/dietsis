import type { FoodItem } from '../types'

/** Lowercase, strip diacritics and punctuation so "Gado-gado" matches "gado gado". */
export function normalize(s: string): string {
  return s
    .normalize('NFD')
    .replace(/[\u0300-\u036f]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, ' ')
    .trim()
}

/**
 * Ranks foods for a query. Every query token must prefix some word of the name or an alias.
 * Score: name starts with query > a name word starts with the first token > alias-only match.
 */
export function searchFoods(foods: readonly FoodItem[], query: string, limit = 50): FoodItem[] {
  const q = normalize(query)
  if (!q) return foods.slice(0, limit)
  const tokens = q.split(' ')

  const scored: { food: FoodItem; score: number; index: number }[] = []
  foods.forEach((food, index) => {
    const name = normalize(food.name)
    const nameWords = name.split(' ')
    const aliasWords = (food.aliases ?? []).flatMap((a) => normalize(a).split(' '))
    const all = [...nameWords, ...aliasWords]
    if (!tokens.every((t) => all.some((w) => w.startsWith(t)))) return

    let score = 1
    if (tokens.every((t) => nameWords.some((w) => w.startsWith(t)))) score = 2
    if (name.startsWith(q)) score = 3
    scored.push({ food, score, index })
  })

  scored.sort((a, b) => b.score - a.score || a.index - b.index)
  return scored.slice(0, limit).map((s) => s.food)
}
