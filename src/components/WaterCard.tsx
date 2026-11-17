import { Droplets, Minus, Plus } from 'lucide-react'
import { useActions } from '../context/DataContext'
import { useToast } from '../context/ToastContext'
import { GLASS_ML } from '../lib/nutrition'
import { fmt } from '../lib/format'
import { WATER_MAX } from '../services/local/state'
import { ProgressBar } from './ui/Progress'
import { Card, CardTitle } from './ui/Card'

interface WaterCardProps {
  date: string
  glasses: number
  target: number
}

export function WaterCard({ date, glasses, target }: WaterCardProps) {
  const actions = useActions()
  const toast = useToast()
  const dots = Math.max(target, glasses)

  function change(delta: number) {
    actions.addWater(date, delta).catch(() => toast({ message: 'Gagal mengubah jumlah air.', tone: 'error' }))
  }

  return (
    <Card aria-label="Air minum">
      <CardTitle
        action={
          <span className="tabular text-sm text-fg-muted">
            {fmt.int(glasses * GLASS_ML)} / {fmt.int(target * GLASS_ML)} ml
          </span>
        }
      >
        <span className="flex items-center gap-2">
          <Droplets className="size-5 text-water" aria-hidden /> Air
        </span>
      </CardTitle>

      <div className="flex items-center gap-3">
        <button
          type="button"
          aria-label="Kurangi satu gelas air"
          disabled={glasses <= 0}
          onClick={() => change(-1)}
          className="inline-flex size-11 shrink-0 items-center justify-center rounded-pill bg-surface-muted text-fg hover:bg-border disabled:opacity-40"
        >
          <Minus className="size-5" aria-hidden />
        </button>
        <div className="flex flex-1 flex-col items-center gap-1.5">
          <p className="tabular text-2xl font-semibold" aria-live="polite">
            {glasses} <span className="text-base font-normal text-fg-muted">/ {target} gelas</span>
          </p>
          <div className="flex flex-wrap justify-center gap-1" aria-hidden>
            {Array.from({ length: Math.min(dots, 20) }, (_, i) => (
              <span key={i} className={`size-2.5 rounded-pill transition-colors ${i < glasses ? 'bg-water' : 'bg-surface-muted'}`} />
            ))}
          </div>
        </div>
        <button
          type="button"
          aria-label="Tambah satu gelas air"
          disabled={glasses >= WATER_MAX}
          onClick={() => change(1)}
          className="inline-flex size-11 shrink-0 items-center justify-center rounded-pill bg-water-soft text-water hover:bg-water hover:text-on-primary disabled:opacity-40"
        >
          <Plus className="size-5" aria-hidden />
        </button>
      </div>
      <div className="mt-3">
        <ProgressBar value={glasses} max={target} label="Target air" tone="water" detail={`${Math.round((glasses / target) * 100)}%`} />
      </div>
    </Card>
  )
}
