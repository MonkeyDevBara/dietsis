import { Footprints, Pencil } from 'lucide-react'
import { useState } from 'react'
import type { SubmitEvent } from 'react'
import { useActions } from '../context/DataContext'
import { useToast } from '../context/ToastContext'
import { fmt } from '../lib/format'
import { hasErrors, parseNumber, validateDayPatch } from '../lib/validation'
import type { FieldErrors } from '../lib/validation'
import type { Day, DayPatch } from '../types'
import { Button, IconButton } from './ui/Button'
import { Card, CardTitle } from './ui/Card'
import { NumberField, TextareaField } from './ui/Field'
import { Sheet } from './ui/Sheet'

export function ActivityCard({ day }: { day: Day }) {
  const [editing, setEditing] = useState(false)
  const empty = day.steps === null && day.note === ''

  return (
    <Card aria-label="Aktivitas harian">
      <CardTitle
        action={
          <IconButton label="Ubah aktivitas" onClick={() => setEditing(true)}>
            <Pencil className="size-5" aria-hidden />
          </IconButton>
        }
      >
        <span className="flex items-center gap-2">
          <Footprints className="size-5 text-primary" aria-hidden /> Aktivitas
        </span>
      </CardTitle>
      {empty ? (
        <p className="text-sm text-fg-muted">Belum ada catatan. Tambahkan langkah atau catatan singkat hari ini.</p>
      ) : (
        <div className="flex flex-col gap-1">
          {day.steps !== null && (
            <p className="tabular text-lg font-semibold">
              {fmt.int(day.steps)} <span className="text-sm font-normal text-fg-muted">langkah</span>
            </p>
          )}
          {day.note && <p className="whitespace-pre-wrap text-sm text-fg-muted">{day.note}</p>}
        </div>
      )}
      {editing && <ActivityForm day={day} onClose={() => setEditing(false)} />}
    </Card>
  )
}

const FORM_ID = 'activity-form'

function ActivityForm({ day, onClose }: { day: Day; onClose: () => void }) {
  const actions = useActions()
  const toast = useToast()
  const [steps, setSteps] = useState(day.steps === null ? '' : String(day.steps))
  const [note, setNote] = useState(day.note)
  const [errors, setErrors] = useState<FieldErrors<DayPatch>>({})

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    const patch: DayPatch = { steps: steps.trim() === '' ? null : parseNumber(steps), note }
    const found = validateDayPatch(patch)
    setErrors(found)
    if (hasErrors(found)) return
    await actions.updateDay(day.date, patch)
    toast({ message: 'Aktivitas disimpan.' })
    onClose()
  }

  return (
    <Sheet
      open
      onClose={onClose}
      title="Aktivitas harian"
      footer={
        <Button type="submit" form={FORM_ID} block>
          Simpan
        </Button>
      }
    >
      <form id={FORM_ID} onSubmit={(e) => void submit(e)} noValidate className="flex flex-col gap-4">
        <NumberField label="Langkah" suffix="langkah" value={steps} onChange={(e) => setSteps(e.target.value)} error={errors.steps} hint="Kosongkan bila tidak ingin mencatat." data-autofocus />
        <TextareaField label="Catatan" value={note} onChange={(e) => setNote(e.target.value)} error={errors.note} maxLength={500} placeholder="mis. Jalan pagi 30 menit" />
      </form>
    </Sheet>
  )
}
