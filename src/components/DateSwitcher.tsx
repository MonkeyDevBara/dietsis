import { ChevronLeft, ChevronRight } from 'lucide-react'
import { addDays, formatDateLong, todayKey } from '../lib/date'
import { formatRelativeDay } from '../lib/format'
import { IconButton } from './ui/Button'

interface DateSwitcherProps {
  date: string
  onChange: (date: string) => void
  /** Latest selectable date (default: today). Logging the future is not supported. */
  max?: string
}

export function DateSwitcher({ date, onChange, max = todayKey() }: DateSwitcherProps) {
  const isMax = date >= max
  return (
    <div className="flex items-center justify-between gap-2 rounded-pill bg-surface p-1 ring-1 ring-border">
      <IconButton label="Hari sebelumnya" onClick={() => onChange(addDays(date, -1))}>
        <ChevronLeft className="size-5" aria-hidden />
      </IconButton>
      <div className="flex flex-col items-center leading-tight">
        <span className="text-sm font-semibold text-fg" aria-live="polite">
          {formatRelativeDay(date)}
        </span>
        {date !== todayKey() && (
          <button type="button" className="text-xs font-medium text-primary hover:underline" onClick={() => onChange(todayKey())}>
            Kembali ke hari ini
          </button>
        )}
        {date === todayKey() && <span className="text-xs text-fg-subtle">{formatDateLong(date)}</span>}
      </div>
      <IconButton label="Hari berikutnya" disabled={isMax} onClick={() => onChange(addDays(date, 1))}>
        <ChevronRight className="size-5" aria-hidden />
      </IconButton>
    </div>
  )
}
