// User intents → validated operations on the local repository. Pages call these, never repo.apply
// directly, so validation and op shapes stay in one place.

import { uuid } from '../lib/id'
import { hasErrors, validateCustomFood, validateDayPatch, validateFoodLog, validateProfile, validateWeight, quantityError } from '../lib/validation'
import { isDateKey } from '../lib/date'
import type { CustomFoodInput, DayPatch, FoodItem, FoodLogInput, FoodLogPatch, Meal, ProfileInput } from '../types'
import type { Repository } from './local/repository'

export type FieldMessages = Partial<Record<string, string>>

export class ValidationError extends Error {
  readonly fields: FieldMessages
  constructor(fields: FieldMessages) {
    super('Data tidak valid')
    this.name = 'ValidationError'
    this.fields = fields
  }
}

function ensure(errors: FieldMessages): void {
  if (hasErrors(errors)) throw new ValidationError(errors)
}

export interface Actions {
  saveProfile(input: ProfileInput, opts?: { logWeightFor?: string }): Promise<void>
  /** Returns the new food log id. */
  logFood(food: FoodItem, opts: { date: string; meal: Meal; quantity: number }): Promise<string>
  updateFoodLog(id: string, patch: FoodLogPatch): Promise<void>
  deleteFoodLog(id: string): Promise<void>
  restoreFoodLog(id: string): Promise<void>
  setFavorite(foodId: string, favorite: boolean): Promise<void>
  /** Returns the food id ('c:<uuid>') of the new custom food. */
  createCustomFood(input: CustomFoodInput): Promise<string>
  addWater(date: string, delta: number): Promise<void>
  updateDay(date: string, patch: DayPatch): Promise<void>
  saveWeight(date: string, weight_kg: number, waist_cm: number | null): Promise<void>
  deleteWeight(date: string): Promise<void>
  deleteAllData(): Promise<void>
}

export function createActions(repo: Repository): Actions {
  return {
    async saveProfile(input: ProfileInput, opts: { logWeightFor?: string } = {}) {
      ensure(validateProfile(input))
      await repo.apply({ type: 'profile.put', payload: input })
      // The profile weight is the user's current weight: record it in the weight history too.
      if (opts.logWeightFor) {
        const existing = repo.getSnapshot().weights[opts.logWeightFor]
        if (existing?.weight_kg !== input.weight_kg) {
          await repo.apply({
            type: 'weight.put',
            payload: { date: opts.logWeightFor, weight_kg: input.weight_kg, waist_cm: existing?.waist_cm ?? null },
          })
        }
      }
    },

    async logFood(food: FoodItem, opts: { date: string; meal: Meal; quantity: number }): Promise<string> {
      const input: FoodLogInput = {
        date: opts.date,
        meal: opts.meal,
        food_id: food.id,
        name: food.name,
        serving_label: food.serving_label,
        serving_grams: food.serving_grams,
        quantity: opts.quantity,
        kcal: food.kcal,
        protein_g: food.protein_g,
        carbs_g: food.carbs_g,
        fat_g: food.fat_g,
      }
      ensure(validateFoodLog(input))
      const id = uuid()
      await repo.apply({ type: 'food_log.create', payload: { ...input, id } })
      return id
    },

    async updateFoodLog(id: string, patch: FoodLogPatch) {
      if (patch.quantity !== undefined) {
        const err = quantityError(patch.quantity)
        if (err) throw new ValidationError({ quantity: err })
      }
      await repo.apply({ type: 'food_log.update', payload: { id, patch } })
    },

    async deleteFoodLog(id: string) {
      await repo.apply({ type: 'food_log.delete', payload: { id } })
    },

    async restoreFoodLog(id: string) {
      await repo.apply({ type: 'food_log.restore', payload: { id } })
    },

    async setFavorite(foodId: string, favorite: boolean) {
      await repo.apply({ type: 'favorite.set', payload: { food_id: foodId, favorite } })
    },

    async createCustomFood(input: CustomFoodInput): Promise<string> {
      ensure(validateCustomFood(input))
      const id = uuid()
      await repo.apply({ type: 'custom_food.create', payload: { ...input, id } })
      return `c:${id}`
    },

    async addWater(date: string, delta: number) {
      if (!isDateKey(date) || !Number.isInteger(delta) || delta === 0 || Math.abs(delta) > 20) {
        throw new ValidationError({ delta: 'Perubahan air tidak valid' })
      }
      await repo.apply({ type: 'day.water', payload: { date, delta } })
    },

    async updateDay(date: string, patch: DayPatch) {
      ensure(validateDayPatch(patch))
      await repo.apply({ type: 'day.update', payload: { date, patch } })
    },

    async saveWeight(date: string, weight_kg: number, waist_cm: number | null) {
      if (!isDateKey(date)) throw new ValidationError({ date: 'Tanggal tidak valid' })
      ensure(validateWeight({ weight_kg, waist_cm }))
      await repo.apply({ type: 'weight.put', payload: { date, weight_kg, waist_cm } })
    },

    async deleteWeight(date: string) {
      await repo.apply({ type: 'weight.delete', payload: { date } })
    },

    async deleteAllData() {
      await repo.clear()
    },
  }
}
