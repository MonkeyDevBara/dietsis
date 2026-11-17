import { Plus, Scale, TrendingDown, TrendingUp } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Link } from 'react-router'
import { ActivityCard } from '../components/ActivityCard'
import { PageHeader } from '../components/AppShell'
import { DateSwitcher } from '../components/DateSwitcher'
import { FoodPickerSheet } from '../components/food/FoodPickerSheet'
import { SyncBadge } from '../components/SyncBadge'
import { WaterCard } from '../components/WaterCard'
import { Button } from '../components/ui/Button'
import { Card, CardTitle } from '../components/ui/Card'
import { ProgressBar, ProgressRing } from '../components/ui/Progress'
import { useLocalState } from '../context/DataContext'
import { useSelectedDate } from '../hooks/useSelectedDate'
import { useSyncStatus } from '../hooks/useSyncStatus'
import { fmt } from '../lib/format'
import { dayOrDefault, logsForDate, mealForTime, sortedWeights, totalsOf, weekSummary, weightSummary } from '../services/local/selectors'

export function DashboardPage() {
  const state = useLocalState()
  const profile = state.profile
  const [date, setDate] = useSelectedDate()
  const sync = useSyncStatus()
  const [adding, setAdding] = useState(false)

  const logs = useMemo(() => logsForDate(state, date), [state, date])
  const totals = useMemo(() => totalsOf(logs), [logs])
  const day = dayOrDefault(state, date)
  const weights = useMemo(() => sortedWeights(state), [state])
  const wsum = useMemo(() => weightSummary(weights, profile), [weights, profile])
  const week = useMemo(() => (profile ? weekSummary(state, date, profile.calorie_target) : null), [state, date, profile])

  if (!profile) return null // RequireProfile guarantees a profile; this narrows the type.

  const remaining = profile.calorie_target - totals.kcal
  const over = remaining < 0
  const kcalFromMacros = totals.protein_g * 4 + totals.carbs_g * 4 + totals.fat_g * 9
  const share = (kcal: number) => (kcalFromMacros > 0 ? `${Math.round((kcal / kcalFromMacros) * 100)}% energi` : '—')

  return (
    <>
      <PageHeader title={`Halo, ${profile.display_name}`} subtitle="Ringkasan harianmu." actions={<SyncBadge status={sync} />} />

      <div className="flex flex-col gap-4">
        <DateSwitcher date={date} onChange={setDate} />

        <Card aria-label="Kalori">
          <div className="flex items-center gap-5">
            <ProgressRing value={totals.kcal} max={profile.calorie_target} label="Kalori hari ini">
              <span className={`tabular text-3xl font-semibold ${over ? 'text-danger' : 'text-fg'}`}>{fmt.int(Math.abs(remaining))}</span>
              <span className="text-xs text-fg-muted">{over ? 'kkal lebih' : 'kkal tersisa'}</span>
            </ProgressRing>
            <dl className="flex flex-1 flex-col gap-3">
              <div>
                <dt className="text-xs uppercase tracking-wide text-fg-subtle">Dimakan</dt>
                <dd className="tabular text-xl font-semibold">{fmt.kcal(totals.kcal)}</dd>
              </div>
              <div>
                <dt className="text-xs uppercase tracking-wide text-fg-subtle">Target</dt>
                <dd className="tabular text-xl font-semibold text-fg-muted">{fmt.kcal(profile.calorie_target)}</dd>
              </div>
            </dl>
          </div>
          <Button block className="mt-4" icon={<Plus className="size-5" aria-hidden />} onClick={() => setAdding(true)}>
            Tambah makanan
          </Button>
        </Card>

        <Card aria-label="Makronutrien">
          <CardTitle>Nutrisi</CardTitle>
          <div className="flex flex-col gap-4">
            <ProgressBar label="Protein" tone="protein" value={totals.protein_g} max={profile.protein_target_g} detail={`${fmt.dec1(totals.protein_g)} / ${fmt.int(profile.protein_target_g)} g`} />
            <div className="grid grid-cols-2 gap-3">
              <div className="rounded-control bg-carbs-soft p-3">
                <p className="text-xs font-medium text-carbs">Karbohidrat</p>
                <p className="tabular text-lg font-semibold">{fmt.gram(totals.carbs_g)}</p>
                <p className="text-xs text-fg-muted">{share(totals.carbs_g * 4)}</p>
              </div>
              <div className="rounded-control bg-fat-soft p-3">
                <p className="text-xs font-medium text-fat">Lemak</p>
                <p className="tabular text-lg font-semibold">{fmt.gram(totals.fat_g)}</p>
                <p className="text-xs text-fg-muted">{share(totals.fat_g * 9)}</p>
              </div>
            </div>
          </div>
        </Card>

        <WaterCard date={date} glasses={day.water_glasses} target={profile.water_target_glasses} />

        <Card aria-label="Berat badan">
          <CardTitle
            action={
              <Link to="/progress" className="text-sm font-medium text-primary hover:underline">
                Lihat progres
              </Link>
            }
          >
            <span className="flex items-center gap-2">
              <Scale className="size-5 text-primary" aria-hidden /> Berat badan
            </span>
          </CardTitle>
          {wsum.current === null ? (
            <p className="text-sm text-fg-muted">Belum ada data berat.</p>
          ) : (
            <div className="flex items-end justify-between gap-3">
              <div>
                <p className="tabular text-3xl font-semibold">{fmt.kg(wsum.current)}</p>
                <p className="text-sm text-fg-muted">Target {fmt.kg(profile.goal_weight_kg)}</p>
              </div>
              {wsum.change !== null && wsum.change !== 0 && (
                <p className="flex items-center gap-1 text-sm font-medium text-fg-muted">
                  {wsum.change < 0 ? <TrendingDown className="size-4 text-primary" aria-hidden /> : <TrendingUp className="size-4 text-carbs" aria-hidden />}
                  {fmt.kgDelta(wsum.change)} sejak awal
                </p>
              )}
            </div>
          )}
        </Card>

        <Card aria-label="Ringkasan 7 hari">
          <CardTitle>7 hari terakhir</CardTitle>
          {week && week.daysLogged > 0 ? (
            <dl className="grid grid-cols-3 gap-2 text-center">
              <div>
                <dd className="tabular text-xl font-semibold">{week.daysLogged}/7</dd>
                <dt className="text-xs text-fg-muted">hari tercatat</dt>
              </div>
              <div>
                <dd className="tabular text-xl font-semibold">{week.avgKcal === null ? '—' : fmt.int(week.avgKcal)}</dd>
                <dt className="text-xs text-fg-muted">rata-rata kkal</dt>
              </div>
              <div>
                <dd className="tabular text-xl font-semibold">{week.daysOnTarget}</dd>
                <dt className="text-xs text-fg-muted">hari sesuai target</dt>
              </div>
            </dl>
          ) : (
            <p className="text-sm text-fg-muted">Catat makananmu beberapa hari untuk melihat ringkasan mingguan.</p>
          )}
          <p className="mt-3 text-xs text-fg-subtle">Sesuai target = 80–105% dari target kalori. Hari tanpa catatan tidak dihitung.</p>
        </Card>

        <ActivityCard key={date} day={day} />
      </div>

      <FoodPickerSheet open={adding} onClose={() => setAdding(false)} date={date} meal={mealForTime(new Date())} />
    </>
  )
}
