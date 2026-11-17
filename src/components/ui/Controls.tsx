import { Minus, Plus } from 'lucide-react'
import { useId } from 'react'
import { fmt } from '../../lib/format'

interface SegmentedControlProps<T extends string> {
  label: string
  value: T
  options: readonly { value: T; label: string }[]
  onChange: (value: T) => void
  hideLabel?: boolean
}

/** Single choice among 2–5 options; a radio group under the hood (arrow keys work natively). */
export function SegmentedControl<T extends string>({ label, value, options, onChange, hideLabel }: SegmentedControlProps<T>) {
  const name = useId()
  return (
    <fieldset className="flex flex-col gap-1.5">
      <legend className={hideLabel ? 'sr-only' : 'mb-1.5 text-sm font-medium text-fg-muted'}>{label}</legend>
      <div className="flex gap-1 rounded-control bg-surface-muted p-1">
        {options.map((o) => (
          <label
            key={o.value}
            className="flex min-h-10 flex-1 cursor-pointer items-center justify-center rounded-[0.625rem] px-2 text-center text-sm font-medium text-fg-muted transition-colors has-[:checked]:bg-surface has-[:checked]:text-fg has-[:checked]:shadow-card has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-primary"
          >
            <input type="radio" name={name} value={o.value} checked={value === o.value} onChange={() => onChange(o.value)} className="sr-only" />
            {o.label}
          </label>
        ))}
      </div>
    </fieldset>
  )
}

interface StepperProps {
  label: string
  value: number
  onChange: (value: number) => void
  step: number
  min: number
  max: number
  format?: (n: number) => string
  unit?: string
}

export function Stepper({ label, value, onChange, step, min, max, format = fmt.qty, unit }: StepperProps) {
  const clamp = (n: number) => Math.min(max, Math.max(min, Math.round(n * 100) / 100))
  return (
    <div className="flex items-center gap-2" role="group" aria-label={label}>
      <button
        type="button"
        aria-label={`Kurangi ${label.toLowerCase()}`}
        disabled={value <= min}
        onClick={() => onChange(clamp(value - step))}
        className="inline-flex size-11 items-center justify-center rounded-pill bg-surface-muted text-fg transition-colors hover:bg-border disabled:opacity-40"
      >
        <Minus className="size-5" aria-hidden />
      </button>
      <output className="min-w-16 text-center text-xl font-semibold tabular" aria-live="polite">
        {format(value)}
        {unit && <span className="ml-1 text-sm font-normal text-fg-muted">{unit}</span>}
      </output>
      <button
        type="button"
        aria-label={`Tambah ${label.toLowerCase()}`}
        disabled={value >= max}
        onClick={() => onChange(clamp(value + step))}
        className="inline-flex size-11 items-center justify-center rounded-pill bg-primary-soft text-primary transition-colors hover:bg-primary hover:text-on-primary disabled:opacity-40"
      >
        <Plus className="size-5" aria-hidden />
      </button>
    </div>
  )
}
