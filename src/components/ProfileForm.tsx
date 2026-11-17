import { useMemo, useState } from 'react'
import type { SubmitEvent } from 'react'
import { ACTIVITY_LABELS, GENDER_LABELS } from '../data/labels'
import { fmt } from '../lib/format'
import { isBelowSafeFloor, recommendTargets } from '../lib/nutrition'
import { hasErrors, parseNumber, validateProfile } from '../lib/validation'
import type { FieldErrors } from '../lib/validation'
import { ACTIVITY_LEVELS } from '../types'
import type { ActivityLevel, Gender, Profile, ProfileInput } from '../types'
import { SegmentedControl } from './ui/Controls'
import { Button } from './ui/Button'
import { Card, CardTitle } from './ui/Card'
import { NumberField, TextField } from './ui/Field'

interface ProfileFormProps {
  initial?: Profile | null
  defaultName?: string
  submitLabel: string
  onSubmit: (input: ProfileInput) => Promise<void>
}

const str = (n: number | undefined) => (n === undefined ? '' : String(n).replace('.', ','))

type TargetKey = 'calorie' | 'protein' | 'water'

export function ProfileForm({ initial, defaultName = '', submitLabel, onSubmit }: ProfileFormProps) {
  const [name, setName] = useState(initial?.display_name ?? defaultName)
  const [gender, setGender] = useState<Gender>(initial?.gender ?? 'female')
  const [age, setAge] = useState(str(initial?.age))
  const [height, setHeight] = useState(str(initial?.height_cm))
  const [weight, setWeight] = useState(str(initial?.weight_kg))
  const [goal, setGoal] = useState(str(initial?.goal_weight_kg))
  const [activity, setActivity] = useState<ActivityLevel>(initial?.activity_level ?? 'light')

  // A target is "manual" once the user edits it; "Pakai saran" switches it back to following the formula.
  const [manual, setManual] = useState<Record<TargetKey, boolean>>({
    calorie: initial?.calorie_target_manual ?? false,
    protein: initial?.protein_target_manual ?? false,
    water: initial?.water_target_manual ?? false,
  })
  const [manualValue, setManualValue] = useState<Record<TargetKey, string>>({
    calorie: str(initial?.calorie_target),
    protein: str(initial?.protein_target_g),
    water: str(initial?.water_target_glasses),
  })

  const [errors, setErrors] = useState<FieldErrors<ProfileInput>>({})
  const [saving, setSaving] = useState(false)

  const body = useMemo(
    () => ({
      gender,
      age: parseNumber(age),
      height_cm: parseNumber(height),
      weight_kg: parseNumber(weight),
      goal_weight_kg: parseNumber(goal),
      activity_level: activity,
    }),
    [gender, age, height, weight, goal, activity],
  )
  const bodyValid = [body.age, body.height_cm, body.weight_kg, body.goal_weight_kg].every(Number.isFinite)
  const recommended = useMemo(() => (bodyValid ? recommendTargets(body) : null), [body, bodyValid])

  function targetValue(key: TargetKey, rec: number | undefined): string {
    return manual[key] ? manualValue[key] : rec !== undefined ? String(rec) : ''
  }
  const calorieStr = targetValue('calorie', recommended?.calorie_target)
  const proteinStr = targetValue('protein', recommended?.protein_target_g)
  const waterStr = targetValue('water', recommended?.water_target_glasses)

  function editTarget(key: TargetKey, value: string) {
    setManual((m) => ({ ...m, [key]: true }))
    setManualValue((v) => ({ ...v, [key]: value }))
  }
  function applySuggestion(key: TargetKey) {
    setManual((m) => ({ ...m, [key]: false }))
  }

  const calorieNum = parseNumber(calorieStr)
  const belowFloor = Number.isFinite(calorieNum) && isBelowSafeFloor(gender, calorieNum)

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    const input: ProfileInput = {
      display_name: name.trim(),
      ...body,
      calorie_target: calorieNum,
      protein_target_g: parseNumber(proteinStr),
      water_target_glasses: parseNumber(waterStr),
      calorie_target_manual: manual.calorie,
      protein_target_manual: manual.protein,
      water_target_manual: manual.water,
    }
    const found = validateProfile(input)
    setErrors(found)
    if (hasErrors(found)) return
    setSaving(true)
    try {
      await onSubmit(input)
    } finally {
      setSaving(false)
    }
  }

  function targetHint(key: TargetKey, rec: number | undefined, unit: string) {
    if (rec === undefined) return 'Isi data tubuh dulu untuk melihat saran.'
    return manual[key] ? (
      <>
        Diatur manual.{' '}
        <button type="button" className="font-medium text-primary underline" onClick={() => applySuggestion(key)}>
          Pakai saran ({fmt.int(rec)} {unit})
        </button>
      </>
    ) : (
      `Saran otomatis dari datamu. Ubah angkanya untuk mengatur sendiri.`
    )
  }

  return (
    <form onSubmit={(e) => void submit(e)} noValidate className="flex flex-col gap-4">
      <Card>
        <CardTitle>Tentang kamu</CardTitle>
        <div className="flex flex-col gap-4">
          <TextField label="Nama" value={name} onChange={(e) => setName(e.target.value)} error={errors.display_name} autoComplete="given-name" maxLength={60} />
          <SegmentedControl
            label="Gender"
            value={gender}
            onChange={setGender}
            options={(['female', 'male'] as const).map((g) => ({ value: g, label: GENDER_LABELS[g] }))}
          />
          <div className="grid grid-cols-2 gap-3">
            <NumberField label="Usia" suffix="tahun" value={age} onChange={(e) => setAge(e.target.value)} error={errors.age} />
            <NumberField label="Tinggi" suffix="cm" value={height} onChange={(e) => setHeight(e.target.value)} error={errors.height_cm} />
            <NumberField label="Berat sekarang" suffix="kg" value={weight} onChange={(e) => setWeight(e.target.value)} error={errors.weight_kg} />
            <NumberField label="Target berat" suffix="kg" value={goal} onChange={(e) => setGoal(e.target.value)} error={errors.goal_weight_kg} />
          </div>
          <fieldset className="flex flex-col gap-2">
            <legend className="mb-1 text-sm font-medium text-fg-muted">Aktivitas harian</legend>
            {ACTIVITY_LEVELS.map((level) => (
              <label
                key={level}
                className="flex min-h-12 cursor-pointer items-center gap-3 rounded-control px-3 py-2 ring-1 ring-border transition-colors has-[:checked]:bg-primary-soft has-[:checked]:ring-2 has-[:checked]:ring-primary"
              >
                <input type="radio" name="activity" className="size-4 accent-primary" checked={activity === level} onChange={() => setActivity(level)} />
                <span className="flex flex-col leading-tight">
                  <span className="font-medium">{ACTIVITY_LABELS[level].label}</span>
                  <span className="text-sm text-fg-muted">{ACTIVITY_LABELS[level].hint}</span>
                </span>
              </label>
            ))}
          </fieldset>
        </div>
      </Card>

      <Card>
        <CardTitle>Target harian</CardTitle>
        <div className="flex flex-col gap-4">
          <NumberField
            label="Kalori"
            suffix="kkal"
            value={calorieStr}
            onChange={(e) => editTarget('calorie', e.target.value)}
            error={errors.calorie_target}
            hint={targetHint('calorie', recommended?.calorie_target, 'kkal')}
          />
          {belowFloor && (
            <p role="alert" className="rounded-control bg-warning-soft px-3 py-2 text-sm text-warning">
              Target ini di bawah batas aman umum ({fmt.int(gender === 'male' ? 1500 : 1200)} kkal). Konsultasikan dengan tenaga kesehatan sebelum makan sesedikit ini.
            </p>
          )}
          <NumberField
            label="Protein"
            suffix="g"
            value={proteinStr}
            onChange={(e) => editTarget('protein', e.target.value)}
            error={errors.protein_target_g}
            hint={targetHint('protein', recommended?.protein_target_g, 'g')}
          />
          <NumberField
            label="Air"
            suffix="gelas"
            value={waterStr}
            onChange={(e) => editTarget('water', e.target.value)}
            error={errors.water_target_glasses}
            hint={targetHint('water', recommended?.water_target_glasses, 'gelas')}
          />
          <p className="text-xs text-fg-subtle">
            1 gelas = 250 ml. Angka saran adalah perkiraan umum (rumus Mifflin-St Jeor), bukan saran medis atau diagnosis. Hubungi dokter atau ahli gizi untuk kebutuhan khusus.
          </p>
        </div>
      </Card>

      <Button type="submit" loading={saving} block>
        {submitLabel}
      </Button>
    </form>
  )
}
