import { useState } from 'react'
import { MEAL_LABELS } from '../../data/labels'
import { fmt } from '../../lib/format'
import { entryTotals } from '../../lib/nutrition'
import { MEALS } from '../../types'
import type { Meal, Nutrients } from '../../types'
import { SegmentedControl, Stepper } from '../ui/Controls'
import { LIMITS } from '../../lib/validation'

export function MacroChips({ n, className = '' }: { n: Nutrients; className?: string }) {
  return (
    <div className={`flex flex-wrap gap-1.5 text-xs font-medium ${className}`}>
      <span className="rounded-pill bg-protein-soft px-2 py-0.5 text-protein">P {fmt.dec1(n.protein_g)} g</span>
      <span className="rounded-pill bg-carbs-soft px-2 py-0.5 text-carbs">K {fmt.dec1(n.carbs_g)} g</span>
      <span className="rounded-pill bg-fat-soft px-2 py-0.5 text-fat">L {fmt.dec1(n.fat_g)} g</span>
    </div>
  )
}

interface FoodDetailProps {
  name: string
  servingLabel: string
  perServing: Nutrients
  quantity: number
  meal: Meal
  onQuantity: (q: number) => void
  onMeal: (m: Meal) => void
}

/** Quantity + meal chooser with a live nutrition preview (same rounding as the stored totals). */
export function FoodDetail({ name, servingLabel, perServing, quantity, meal, onQuantity, onMeal }: FoodDetailProps) {
  const totals = entryTotals(perServing, quantity)
  return (
    <div className="flex flex-col gap-5">
      <div>
        <p className="text-lg font-semibold">{name}</p>
        <p className="text-sm text-fg-muted">1 porsi = {servingLabel}</p>
      </div>

      <div className="flex items-center justify-between gap-3">
        <span className="text-sm font-medium text-fg-muted">Jumlah porsi</span>
        <Stepper label="Porsi" value={quantity} onChange={onQuantity} step={0.5} min={LIMITS.quantity.min} max={LIMITS.quantity.max} />
      </div>

      <SegmentedControl label="Waktu makan" value={meal} onChange={onMeal} options={MEALS.map((m) => ({ value: m, label: MEAL_LABELS[m] }))} />

      <div className="rounded-card bg-surface-muted p-4">
        <p className="tabular text-3xl font-semibold text-kcal">
          {fmt.int(totals.kcal)} <span className="text-base font-normal text-fg-muted">kkal</span>
        </p>
        <MacroChips n={totals} className="mt-2" />
      </div>
    </div>
  )
}

/** Local state for FoodDetail forms. */
export function useFoodDetailState(initialQuantity: number, initialMeal: Meal) {
  const [quantity, setQuantity] = useState(initialQuantity)
  const [meal, setMeal] = useState<Meal>(initialMeal)
  return { quantity, setQuantity, meal, setMeal }
}
