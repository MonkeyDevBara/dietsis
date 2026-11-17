import { useState } from 'react'
import type { SubmitEvent } from 'react'
import { hasErrors, parseNumber, validateCustomFood } from '../../lib/validation'
import type { FieldErrors } from '../../lib/validation'
import type { CustomFoodInput } from '../../types'
import { Button } from '../ui/Button'
import { NumberField, TextField } from '../ui/Field'
import { Sheet } from '../ui/Sheet'

interface CustomFoodSheetProps {
  open: boolean
  onClose: () => void
  onCreate: (input: CustomFoodInput) => Promise<void>
  initialName?: string
}

const FORM_ID = 'custom-food-form'

export function CustomFoodSheet({ open, onClose, onCreate, initialName = '' }: CustomFoodSheetProps) {
  // The sheet unmounts its body when closed (Sheet returns null), so state resets via the key below.
  return open ? <CustomFoodForm key={initialName} onClose={onClose} onCreate={onCreate} initialName={initialName} /> : null
}

function CustomFoodForm({ onClose, onCreate, initialName }: Omit<CustomFoodSheetProps, 'open'> & { initialName: string }) {
  const [name, setName] = useState(initialName)
  const [serving, setServing] = useState('1 porsi')
  const [grams, setGrams] = useState('')
  const [kcal, setKcal] = useState('')
  const [protein, setProtein] = useState('')
  const [carbs, setCarbs] = useState('')
  const [fat, setFat] = useState('')
  const [errors, setErrors] = useState<FieldErrors<CustomFoodInput>>({})
  const [saving, setSaving] = useState(false)

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    // Empty macro fields mean 0 (people often only know calories); calories are required.
    const num = (s: string, emptyAs: number) => (s.trim() === '' ? emptyAs : parseNumber(s))
    const input: CustomFoodInput = {
      name: name.trim(),
      serving_label: serving.trim(),
      serving_grams: grams.trim() === '' ? null : parseNumber(grams),
      kcal: num(kcal, Number.NaN),
      protein_g: num(protein, 0),
      carbs_g: num(carbs, 0),
      fat_g: num(fat, 0),
    }
    const found = validateCustomFood(input)
    setErrors(found)
    if (hasErrors(found)) return
    setSaving(true)
    try {
      await onCreate(input)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Sheet
      open
      onClose={onClose}
      title="Makanan kustom"
      footer={
        <Button type="submit" form={FORM_ID} block loading={saving}>
          Simpan makanan
        </Button>
      }
    >
      <form id={FORM_ID} onSubmit={(e) => void submit(e)} noValidate className="flex flex-col gap-4">
        <TextField label="Nama makanan" value={name} onChange={(e) => setName(e.target.value)} error={errors.name} maxLength={80} data-autofocus />
        <div className="grid grid-cols-2 gap-3">
          <TextField label="Ukuran porsi" value={serving} onChange={(e) => setServing(e.target.value)} error={errors.serving_label} maxLength={40} hint="mis. 1 gelas" />
          <NumberField label="Berat porsi" suffix="g" value={grams} onChange={(e) => setGrams(e.target.value)} error={errors.serving_grams} hint="opsional" />
        </div>
        <p className="text-sm font-medium text-fg-muted">Nutrisi per 1 porsi</p>
        <div className="grid grid-cols-2 gap-3">
          <NumberField label="Kalori" suffix="kkal" value={kcal} onChange={(e) => setKcal(e.target.value)} error={errors.kcal} />
          <NumberField label="Protein" suffix="g" value={protein} onChange={(e) => setProtein(e.target.value)} error={errors.protein_g} />
          <NumberField label="Karbohidrat" suffix="g" value={carbs} onChange={(e) => setCarbs(e.target.value)} error={errors.carbs_g} />
          <NumberField label="Lemak" suffix="g" value={fat} onChange={(e) => setFat(e.target.value)} error={errors.fat_g} />
        </div>
      </form>
    </Sheet>
  )
}
