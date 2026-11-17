import type { ReactNode } from 'react'

type Tone = 'kcal' | 'protein' | 'carbs' | 'fat' | 'water'

const STROKE: Record<Tone, string> = {
  kcal: 'stroke-kcal',
  protein: 'stroke-protein',
  carbs: 'stroke-carbs',
  fat: 'stroke-fat',
  water: 'stroke-water',
}
const FILL: Record<Tone, string> = {
  kcal: 'bg-kcal',
  protein: 'bg-protein',
  carbs: 'bg-carbs',
  fat: 'bg-fat',
  water: 'bg-water',
}

/** Over 105% of target turns red: eating far above target is the one state worth flagging. */
function overTarget(value: number, max: number) {
  return max > 0 && value > max * 1.05
}

interface ProgressRingProps {
  value: number
  max: number
  label: string
  tone?: Tone
  size?: number
  children?: ReactNode
}

export function ProgressRing({ value, max, label, tone = 'kcal', size = 148, children }: ProgressRingProps) {
  const stroke = 12
  const r = (size - stroke) / 2
  const c = 2 * Math.PI * r
  const ratio = max > 0 ? Math.min(1, value / max) : 0
  return (
    <div
      className="relative inline-flex shrink-0 items-center justify-center"
      style={{ width: size, height: size }}
      role="progressbar"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={max}
      aria-valuenow={Math.round(value)}
    >
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} className="-rotate-90" aria-hidden>
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" strokeWidth={stroke} className="stroke-surface-muted" />
        <circle
          cx={size / 2}
          cy={size / 2}
          r={r}
          fill="none"
          strokeWidth={stroke}
          strokeLinecap="round"
          strokeDasharray={c}
          strokeDashoffset={c * (1 - ratio)}
          className={`transition-[stroke-dashoffset] duration-500 ease-standard ${overTarget(value, max) ? 'stroke-danger' : STROKE[tone]}`}
        />
      </svg>
      <div className="absolute inset-0 flex flex-col items-center justify-center text-center">{children}</div>
    </div>
  )
}

interface ProgressBarProps {
  value: number
  max: number
  label: string
  tone?: Tone
  detail?: ReactNode
}

export function ProgressBar({ value, max, label, tone = 'kcal', detail }: ProgressBarProps) {
  const pct = max > 0 ? Math.min(100, (value / max) * 100) : 0
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-baseline justify-between gap-2 text-sm">
        <span className="font-medium text-fg">{label}</span>
        {detail && <span className="tabular text-fg-muted">{detail}</span>}
      </div>
      <div
        className="h-2 overflow-hidden rounded-pill bg-surface-muted"
        role="progressbar"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={max}
        aria-valuenow={Math.round(value)}
      >
        <div className={`h-full rounded-pill transition-[width] duration-500 ease-standard ${overTarget(value, max) ? 'bg-danger' : FILL[tone]}`} style={{ width: `${pct}%` }} />
      </div>
    </div>
  )
}
