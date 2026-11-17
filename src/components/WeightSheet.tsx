import { useState } from 'react'
import type { SubmitEvent } from 'react'
import { useActions, useLocalState } from '../context/DataContext'
import { useToast } from '../context/ToastContext'
import { todayKey } from '../lib/date'
import { formatRelativeDay } from '../lib/format'
import { hasErrors, parseNumber, validateWeight } from '../lib/validation'
import { Button } from './ui/Button'
import { Field, NumberField } from './ui/Field'
import { Sheet } from './ui/Sheet'

const FORM_ID = 'weight-form'
const str = (n: number | null | undefined) => (n === null || n === undefined ? '' : String(n).replace('.', ','))

/** Log or edit the weight (and waist) for a date. One entry per date: saving an existing date edits it. */
export function WeightSheet({ open, onClose, initialDate }: { open: boolean; onClose: () => void; initialDate?: string }) {
  return open ? <WeightForm key={initialDate ?? 'new'} onClose={onClose} initialDate={initialDate ?? todayKey()} /> : null
}

function WeightForm({ onClose, initialDate }: { onClose: () => void; initialDate: string }) {
  const state = useLocalState()
  const actions = useActions()
  const toast = useToast()
  const [date, setDate] = useState(initialDate)
  const existing = state.weights[date]
  // Prefill from the entry for the chosen date; once the user types, their text wins.
  const [weight, setWeight] = useState<string | null>(null)
  const [waist, setWaist] = useState<string | null>(null)
  const [errors, setErrors] = useState<{ weight_kg?: string; waist_cm?: string }>({})

  const weightText = weight ?? str(existing?.weight_kg ?? (state.profile && Object.keys(state.weights).length === 0 ? state.profile.weight_kg : undefined))
  const waistText = waist ?? str(existing?.waist_cm)

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    const w = parseNumber(weightText)
    const c = waistText.trim() === '' ? null : parseNumber(waistText)
    const found = validateWeight({ weight_kg: w, waist_cm: c })
    setErrors(found)
    if (hasErrors(found)) return
    await actions.saveWeight(date, w, c)
    toast({ message: existing ? 'Berat diperbarui.' : 'Berat dicatat.' })
    onClose()
  }

  return (
    <Sheet
      open
      onClose={onClose}
      title="Catat berat badan"
      footer={
        <Button type="submit" form={FORM_ID} block>
          Simpan
        </Button>
      }
    >
      <form id={FORM_ID} onSubmit={(e) => void submit(e)} noValidate className="flex flex-col gap-4">
        <Field label="Tanggal" hint={existing ? 'Sudah ada catatan di tanggal ini; menyimpan akan menggantinya.' : formatRelativeDay(date)}>
          {(p) => (
            <input
              type="date"
              {...p}
              value={date}
              max={todayKey()}
              min="2000-01-01"
              onChange={(e) => {
                if (e.target.value) {
                  setDate(e.target.value)
                  setWeight(null)
                  setWaist(null)
                }
              }}
            />
          )}
        </Field>
        <NumberField label="Berat badan" suffix="kg" value={weightText} onChange={(e) => setWeight(e.target.value)} error={errors.weight_kg} data-autofocus />
        <NumberField label="Lingkar pinggang" suffix="cm" value={waistText} onChange={(e) => setWaist(e.target.value)} error={errors.waist_cm} hint="Opsional." />
      </form>
    </Sheet>
  )
}
