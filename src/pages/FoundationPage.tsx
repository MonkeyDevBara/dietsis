import { formatDateLong, todayKey } from '../lib/date'

export function FoundationPage() {
  return (
    <main className="mx-auto flex min-h-dvh max-w-md flex-col justify-center gap-6 px-5 pb-safe pt-safe">
      <div className="flex items-center gap-3">
        <img src="/favicon.svg" alt="" className="size-12 rounded-control" />
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-fg">Dietsis</h1>
          <p className="text-sm text-fg-muted">Teman diet harianmu.</p>
        </div>
      </div>
      <section className="rounded-card bg-surface p-5 shadow-card ring-1 ring-border">
        <p className="text-xs font-medium uppercase tracking-wide text-fg-subtle">{formatDateLong(todayKey())}</p>
        <p className="mt-2 text-fg">Fondasi proyek siap. Fitur pencatatan dibangun mulai milestone M1.</p>
        <div className="mt-4 flex gap-2">
          <span className="rounded-pill bg-kcal-soft px-3 py-1 text-xs font-medium text-kcal">Kalori</span>
          <span className="rounded-pill bg-protein-soft px-3 py-1 text-xs font-medium text-protein">Protein</span>
          <span className="rounded-pill bg-carbs-soft px-3 py-1 text-xs font-medium text-carbs">Karbo</span>
          <span className="rounded-pill bg-fat-soft px-3 py-1 text-xs font-medium text-fat">Lemak</span>
          <span className="rounded-pill bg-water-soft px-3 py-1 text-xs font-medium text-water">Air</span>
        </div>
      </section>
    </main>
  )
}
