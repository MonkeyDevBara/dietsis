import { Plus, UtensilsCrossed } from 'lucide-react'
import { useMemo, useState } from 'react'
import { PageHeader } from '../components/AppShell'
import { DateSwitcher } from '../components/DateSwitcher'
import { FoodEntrySheet } from '../components/food/FoodEntrySheet'
import { FoodPickerSheet } from '../components/food/FoodPickerSheet'
import { Card } from '../components/ui/Card'
import { ProgressBar } from '../components/ui/Progress'
import { useLocalState } from '../context/DataContext'
import { MEAL_LABELS } from '../data/labels'
import { useSelectedDate } from '../hooks/useSelectedDate'
import { fmt } from '../lib/format'
import { groupByMeal, logsForDate, totalsOf } from '../services/local/selectors'
import { MEALS } from '../types'
import type { FoodLog, Meal } from '../types'

export default function DiaryPage() {
  const state = useLocalState()
  const profile = state.profile
  const [date, setDate] = useSelectedDate()
  const [addMeal, setAddMeal] = useState<Meal | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)

  const logs = useMemo(() => logsForDate(state, date), [state, date])
  const byMeal = useMemo(() => groupByMeal(logs), [logs])
  const totals = useMemo(() => totalsOf(logs), [logs])

  // Look the entry up live so the sheet closes itself if it was deleted (e.g. via the undo toast race).
  const editing: FoodLog | null = editingId ? (state.foodLogs[editingId] ?? null) : null

  if (!profile) return null

  return (
    <>
      <PageHeader title="Diary" subtitle="Catatan makananmu." />
      <div className="flex flex-col gap-4">
        <DateSwitcher date={date} onChange={setDate} />

        <Card aria-label="Total hari ini">
          <ProgressBar label="Kalori" value={totals.kcal} max={profile.calorie_target} detail={`${fmt.int(totals.kcal)} / ${fmt.int(profile.calorie_target)} kkal`} />
        </Card>

        {MEALS.map((meal) => {
          const entries = byMeal[meal]
          const mealKcal = totalsOf(entries).kcal
          return (
            <Card key={meal} aria-label={MEAL_LABELS[meal]} className="!p-0">
              <div className="flex items-center justify-between px-4 pb-2 pt-4 sm:px-5">
                <h2 className="text-base font-semibold">{MEAL_LABELS[meal]}</h2>
                <span className="tabular text-sm text-fg-muted">{entries.length > 0 ? fmt.kcal(mealKcal) : '—'}</span>
              </div>

              {entries.length > 0 && (
                <ul className="divide-y divide-border border-t border-border">
                  {entries.map((log) => (
                    <li key={log.id}>
                      <button type="button" onClick={() => setEditingId(log.id)} className="flex min-h-14 w-full items-center gap-3 px-4 py-2 text-left hover:bg-surface-muted sm:px-5">
                        <span className="flex min-w-0 flex-1 flex-col">
                          <span className="truncate font-medium">{log.name}</span>
                          <span className="truncate text-sm text-fg-muted">
                            {fmt.qty(log.quantity)} × {log.serving_label} · P {fmt.dec1(log.totals.protein_g)} · K {fmt.dec1(log.totals.carbs_g)} · L {fmt.dec1(log.totals.fat_g)}
                          </span>
                        </span>
                        <span className="tabular shrink-0 font-semibold">{fmt.int(log.totals.kcal)}</span>
                      </button>
                    </li>
                  ))}
                </ul>
              )}

              <button
                type="button"
                onClick={() => setAddMeal(meal)}
                className={`flex min-h-12 w-full items-center gap-2 rounded-b-card px-4 text-sm font-semibold text-primary hover:bg-primary-soft sm:px-5 ${entries.length > 0 ? 'border-t border-border' : ''}`}
              >
                <Plus className="size-5" aria-hidden />
                Tambah {MEAL_LABELS[meal].toLowerCase()}
              </button>
            </Card>
          )
        })}

        {logs.length === 0 && (
          <p className="flex items-center justify-center gap-2 text-sm text-fg-subtle">
            <UtensilsCrossed className="size-4" aria-hidden /> Belum ada catatan untuk hari ini.
          </p>
        )}
      </div>

      <FoodPickerSheet open={addMeal !== null} onClose={() => setAddMeal(null)} date={date} meal={addMeal ?? 'breakfast'} />
      <FoodEntrySheet log={editing && editing.deleted_at === null ? editing : null} onClose={() => setEditingId(null)} />
    </>
  )
}
