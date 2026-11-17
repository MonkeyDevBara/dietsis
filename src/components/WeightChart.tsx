import { fmt } from '../lib/format'
import { formatShortDate } from '../lib/format'
import { parseDateKey } from '../lib/date'
import type { Weight } from '../types'

interface WeightChartProps {
  weights: readonly Weight[] // ascending by date
  goal: number | null
}

const W = 320
const H = 170
const PAD = { top: 14, right: 12, bottom: 26, left: 38 }

/** Hand-written SVG line chart (docs/DECISIONS.md D-018): x is proportional to calendar time. */
export function WeightChart({ weights, goal }: WeightChartProps) {
  if (weights.length === 0) return null

  const times = weights.map((w) => parseDateKey(w.date).getTime())
  const values = weights.map((w) => w.weight_kg)
  const all = goal === null ? values : [...values, goal]
  const rawMin = Math.min(...all)
  const rawMax = Math.max(...all)
  const span = Math.max(rawMax - rawMin, 2)
  const mid = (rawMin + rawMax) / 2
  const yMin = mid - span / 2 - span * 0.1
  const yMax = mid + span / 2 + span * 0.1

  const t0 = times[0] as number
  const t1 = times[times.length - 1] as number
  const innerW = W - PAD.left - PAD.right
  const innerH = H - PAD.top - PAD.bottom
  const x = (t: number) => PAD.left + (t1 === t0 ? innerW / 2 : ((t - t0) / (t1 - t0)) * innerW)
  const y = (v: number) => PAD.top + (1 - (v - yMin) / (yMax - yMin)) * innerH

  const points = weights.map((w, i) => ({ cx: x(times[i] as number), cy: y(w.weight_kg), w }))
  const path = points.map((p, i) => `${i === 0 ? 'M' : 'L'}${p.cx.toFixed(1)} ${p.cy.toFixed(1)}`).join(' ')
  const ticks = [yMin + (yMax - yMin) * 0.1, (yMin + yMax) / 2, yMax - (yMax - yMin) * 0.1]

  const first = weights[0] as Weight
  const last = weights[weights.length - 1] as Weight
  const label = `Grafik berat badan dari ${formatShortDate(first.date)} (${fmt.kg(first.weight_kg)}) sampai ${formatShortDate(last.date)} (${fmt.kg(last.weight_kg)})${goal === null ? '' : `, target ${fmt.kg(goal)}`}.`

  return (
    <svg viewBox={`0 0 ${W} ${H}`} role="img" aria-label={label} className="h-auto w-full">
      {ticks.map((t) => (
        <g key={t}>
          <line x1={PAD.left} x2={W - PAD.right} y1={y(t)} y2={y(t)} className="stroke-border" strokeWidth={1} />
          <text x={PAD.left - 6} y={y(t)} textAnchor="end" dominantBaseline="middle" className="fill-fg-subtle" fontSize={10}>
            {fmt.dec1(t)}
          </text>
        </g>
      ))}
      {goal !== null && (
        <g>
          <line x1={PAD.left} x2={W - PAD.right} y1={y(goal)} y2={y(goal)} className="stroke-primary" strokeWidth={1.5} strokeDasharray="5 4" />
          <text x={W - PAD.right} y={y(goal) - 4} textAnchor="end" className="fill-primary" fontSize={10} fontWeight={600}>
            Target {fmt.dec1(goal)}
          </text>
        </g>
      )}
      {points.length > 1 && <path d={path} fill="none" className="stroke-kcal" strokeWidth={2.5} strokeLinejoin="round" strokeLinecap="round" />}
      {points.map((p) => (
        <circle key={p.w.date} cx={p.cx} cy={p.cy} r={points.length > 40 ? 2 : 3.5} className="fill-surface stroke-kcal" strokeWidth={2}>
          <title>{`${formatShortDate(p.w.date)}: ${fmt.kg(p.w.weight_kg)}`}</title>
        </circle>
      ))}
      <text x={PAD.left} y={H - 6} className="fill-fg-subtle" fontSize={10}>
        {formatShortDate(first.date)}
      </text>
      {points.length > 1 && (
        <text x={W - PAD.right} y={H - 6} textAnchor="end" className="fill-fg-subtle" fontSize={10}>
          {formatShortDate(last.date)}
        </text>
      )}
    </svg>
  )
}

interface WaterBarsProps {
  data: readonly { date: string; glasses: number }[]
  target: number
}

export function WaterBars({ data, target }: WaterBarsProps) {
  const max = Math.max(target, ...data.map((d) => d.glasses), 1)
  return (
    <div className="flex h-32 items-end gap-2" role="img" aria-label={`Air 7 hari terakhir: ${data.map((d) => `${formatShortDate(d.date)} ${d.glasses} gelas`).join(', ')}. Target ${target} gelas.`}>
      {data.map((d) => (
        <div key={d.date} className="flex h-full flex-1 flex-col items-center justify-end gap-1">
          <span className="tabular text-xs text-fg-muted">{d.glasses}</span>
          <div className="relative flex w-full flex-1 items-end rounded-t-control bg-surface-muted">
            <div className={`w-full rounded-t-control ${d.glasses >= target ? 'bg-water' : 'bg-water/50'}`} style={{ height: `${(d.glasses / max) * 100}%` }} />
            <div className="pointer-events-none absolute inset-x-0 border-t border-dashed border-primary" style={{ bottom: `${(target / max) * 100}%` }} />
          </div>
          <span className="text-xs text-fg-subtle">{formatWeekdayAbbr(d.date)}</span>
        </div>
      ))}
    </div>
  )
}

function formatWeekdayAbbr(date: string): string {
  return new Intl.DateTimeFormat('id-ID', { weekday: 'short' }).format(parseDateKey(date))
}
