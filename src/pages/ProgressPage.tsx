import { Pencil, Plus, Scale, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { PageHeader } from '../components/AppShell'
import { WaterBars, WeightChart } from '../components/WeightChart'
import { WeightSheet } from '../components/WeightSheet'
import { Button, IconButton } from '../components/ui/Button'
import { Card, CardTitle, EmptyState } from '../components/ui/Card'
import { SegmentedControl } from '../components/ui/Controls'
import { ProgressBar } from '../components/ui/Progress'
import { useActions, useLocalState } from '../context/DataContext'
import { useToast } from '../context/ToastContext'
import { addDays, todayKey } from '../lib/date'
import { fmt, formatShortDate } from '../lib/format'
import { sortedWeights, waterHistory, weightSummary } from '../services/local/selectors'

type Range = '30' | '90' | 'all'

export default function ProgressPage() {
  const state = useLocalState()
  const actions = useActions()
  const toast = useToast()
  const profile = state.profile
  const [range, setRange] = useState<Range>('30')
  const [sheet, setSheet] = useState<{ date?: string } | null>(null)

  const weights = useMemo(() => sortedWeights(state), [state])
  const summary = useMemo(() => weightSummary(weights, profile), [weights, profile])
  const today = todayKey()
  const visible = useMemo(() => {
    if (range === 'all') return weights
    const from = addDays(today, -Number(range))
    return weights.filter((w) => w.date >= from)
  }, [weights, range, today])
  const water = useMemo(() => waterHistory(state, today, 7), [state, today])

  if (!profile) return null

  function remove(date: string) {
    const prev = state.weights[date]
    if (!prev) return
    void actions.deleteWeight(date)
    toast({
      message: 'Catatan berat dihapus.',
      action: { label: 'Urungkan', onClick: () => void actions.saveWeight(date, prev.weight_kg, prev.waist_cm) },
    })
  }

  return (
    <>
      <PageHeader
        title="Progres"
        subtitle="Berat badan dan air."
        actions={
          <Button size="sm" icon={<Plus className="size-4" aria-hidden />} onClick={() => setSheet({})}>
            Catat berat
          </Button>
        }
      />

      <div className="flex flex-col gap-4">
        <Card aria-label="Ringkasan berat">
          {summary.current === null ? (
            <EmptyState icon={<Scale className="size-6" />} title="Belum ada data berat" action={<Button onClick={() => setSheet({})}>Catat berat pertama</Button>}>
              Catat berat secara rutin untuk melihat grafik dan perubahannya.
            </EmptyState>
          ) : (
            <>
              <div className="flex items-end justify-between gap-3">
                <div>
                  <p className="text-xs uppercase tracking-wide text-fg-subtle">Berat terakhir</p>
                  <p className="tabular text-4xl font-semibold">{fmt.kg(summary.current)}</p>
                </div>
                <dl className="text-right text-sm">
                  {summary.change !== null && weights.length > 1 && (
                    <div>
                      <dt className="sr-only">Perubahan sejak awal</dt>
                      <dd className="tabular font-semibold">{summary.change === 0 ? 'Tidak berubah' : `${fmt.kgDelta(summary.change)} sejak awal`}</dd>
                    </div>
                  )}
                  {summary.latestWaist !== null && (
                    <div className="text-fg-muted">
                      <dt className="sr-only">Lingkar pinggang</dt>
                      <dd className="tabular">Pinggang {fmt.dec1(summary.latestWaist)} cm</dd>
                    </div>
                  )}
                </dl>
              </div>
              {summary.goal !== null && summary.remaining !== null && (
                <div className="mt-4">
                  <ProgressBar
                    label={`Menuju target ${fmt.kg(summary.goal)}`}
                    tone="kcal"
                    value={(summary.progress ?? 0) * 100}
                    max={100}
                    detail={summary.remaining === 0 ? 'Tercapai!' : `${fmt.kg(Math.abs(summary.remaining))} lagi`}
                  />
                </div>
              )}
            </>
          )}
        </Card>

        {weights.length > 0 && (
          <Card aria-label="Grafik berat">
            <CardTitle>Grafik</CardTitle>
            <div className="mb-3">
              <SegmentedControl
                hideLabel
                label="Rentang grafik"
                value={range}
                onChange={setRange}
                options={[
                  { value: '30', label: '30 hari' },
                  { value: '90', label: '90 hari' },
                  { value: 'all', label: 'Semua' },
                ]}
              />
            </div>
            {visible.length > 0 ? <WeightChart weights={visible} goal={profile.goal_weight_kg} /> : <p className="py-6 text-center text-sm text-fg-muted">Tidak ada catatan pada rentang ini.</p>}
          </Card>
        )}

        <Card aria-label="Air 7 hari">
          <CardTitle>Air 7 hari terakhir</CardTitle>
          <WaterBars data={water} target={profile.water_target_glasses} />
          <p className="mt-2 text-xs text-fg-subtle">Garis putus-putus = target {profile.water_target_glasses} gelas.</p>
        </Card>

        {weights.length > 0 && (
          <Card aria-label="Riwayat berat" className="!p-0">
            <div className="px-4 pb-2 pt-4 sm:px-5">
              <CardTitle>Riwayat</CardTitle>
            </div>
            <ul className="divide-y divide-border border-t border-border">
              {[...weights].reverse().map((w) => (
                <li key={w.date} className="flex items-center gap-2 py-1 pl-4 pr-2 sm:pl-5">
                  <div className="min-w-0 flex-1">
                    <p className="tabular font-semibold">{fmt.kg(w.weight_kg)}</p>
                    <p className="text-sm text-fg-muted">
                      {formatShortDate(w.date)}
                      {w.waist_cm !== null && ` · pinggang ${fmt.dec1(w.waist_cm)} cm`}
                    </p>
                  </div>
                  <IconButton label={`Ubah catatan ${formatShortDate(w.date)}`} onClick={() => setSheet({ date: w.date })}>
                    <Pencil className="size-5" aria-hidden />
                  </IconButton>
                  <IconButton label={`Hapus catatan ${formatShortDate(w.date)}`} tone="danger" onClick={() => remove(w.date)}>
                    <Trash2 className="size-5" aria-hidden />
                  </IconButton>
                </li>
              ))}
            </ul>
          </Card>
        )}
      </div>

      <WeightSheet open={sheet !== null} onClose={() => setSheet(null)} {...(sheet?.date ? { initialDate: sheet.date } : {})} />
    </>
  )
}
