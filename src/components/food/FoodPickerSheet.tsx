import { ArrowLeft, Plus, Search, Star } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useActions, useLocalState } from '../../context/DataContext'
import { useToast } from '../../context/ToastContext'
import { MEAL_LABELS } from '../../data/labels'
import { searchFoods } from '../../lib/foodSearch'
import { fmt } from '../../lib/format'
import { allFoods, favoriteFoods, recentFoods } from '../../services/local/selectors'
import type { FoodItem, Meal } from '../../types'
import { Button } from '../ui/Button'
import { EmptyState } from '../ui/Card'
import { Sheet } from '../ui/Sheet'
import { CustomFoodSheet } from './CustomFoodSheet'
import { FoodDetail, useFoodDetailState } from './FoodDetail'

type Tab = 'recent' | 'favorite' | 'all'
const TABS: { id: Tab; label: string }[] = [
  { id: 'recent', label: 'Terakhir' },
  { id: 'favorite', label: 'Favorit' },
  { id: 'all', label: 'Semua' },
]

interface FoodPickerSheetProps {
  open: boolean
  onClose: () => void
  date: string
  meal: Meal
}

export function FoodPickerSheet({ open, onClose, date, meal }: FoodPickerSheetProps) {
  // Body is mounted only while open so search text, tab and selection reset every time.
  return open ? <Picker onClose={onClose} date={date} meal={meal} /> : null
}

function Picker({ onClose, date, meal }: Omit<FoodPickerSheetProps, 'open'>) {
  const state = useLocalState()
  const actions = useActions()
  const toast = useToast()
  const [query, setQuery] = useState('')
  const [tab, setTab] = useState<Tab>(() => (recentFoods(state, 1).length > 0 ? 'recent' : 'all'))
  const [selected, setSelected] = useState<{ food: FoodItem; quantity: number } | null>(null)
  const [creating, setCreating] = useState(false)

  const foods = useMemo(() => allFoods(state), [state])
  const recents = useMemo(() => recentFoods(state), [state])
  const favorites = useMemo(() => favoriteFoods(state), [state])
  const results = useMemo(() => (query.trim() ? searchFoods(foods, query) : null), [foods, query])

  async function quickAdd(food: FoodItem, quantity = 1) {
    const id = await actions.logFood(food, { date, meal, quantity })
    toast({
      message: `${food.name} ditambahkan ke ${MEAL_LABELS[meal].toLowerCase()}.`,
      action: { label: 'Urungkan', onClick: () => void actions.deleteFoodLog(id) },
    })
  }

  const list: { food: FoodItem; quantity: number }[] =
    results?.map((food) => ({ food, quantity: 1 })) ??
    (tab === 'recent' ? recents.map((r) => ({ food: r.food, quantity: r.quantity })) : tab === 'favorite' ? favorites.map((food) => ({ food, quantity: 1 })) : foods.map((food) => ({ food, quantity: 1 })))

  if (selected) {
    return (
      <SelectedFood
        food={selected.food}
        initialQuantity={selected.quantity}
        initialMeal={meal}
        date={date}
        onBack={() => setSelected(null)}
        onClose={onClose}
      />
    )
  }

  return (
    <>
      <Sheet open onClose={onClose} title={`Tambah ke ${MEAL_LABELS[meal].toLowerCase()}`}>
        <div className="flex flex-col gap-3">
          <div className="relative">
            <Search className="pointer-events-none absolute left-3.5 top-1/2 size-5 -translate-y-1/2 text-fg-subtle" aria-hidden />
            <input
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Cari makanan…"
              aria-label="Cari makanan"
              autoComplete="off"
              data-autofocus
              className="min-h-11 w-full rounded-control bg-surface-muted pl-11 pr-3.5 ring-1 ring-transparent focus:outline-none focus:ring-2 focus:ring-primary"
            />
          </div>

          {!results && (
            <div role="tablist" aria-label="Sumber makanan" className="flex gap-1 rounded-control bg-surface-muted p-1">
              {TABS.map((t) => (
                <button
                  key={t.id}
                  role="tab"
                  type="button"
                  aria-selected={tab === t.id}
                  onClick={() => setTab(t.id)}
                  className={`min-h-9 flex-1 rounded-[0.625rem] text-sm font-medium transition-colors ${tab === t.id ? 'bg-surface text-fg shadow-card' : 'text-fg-muted'}`}
                >
                  {t.label}
                </button>
              ))}
            </div>
          )}

          {list.length === 0 ? (
            <EmptyState
              icon={results ? <Search className="size-6" /> : <Star className="size-6" />}
              title={results ? 'Makanan tidak ditemukan' : tab === 'favorite' ? 'Belum ada favorit' : 'Belum ada riwayat'}
              action={
                results ? (
                  <Button variant="secondary" size="sm" onClick={() => setCreating(true)}>
                    Buat “{query.trim()}” sebagai makanan kustom
                  </Button>
                ) : undefined
              }
            >
              {results ? 'Coba kata lain, atau buat sendiri.' : tab === 'favorite' ? 'Ketuk bintang pada makanan untuk menyimpannya di sini.' : 'Makanan yang kamu catat akan muncul di sini.'}
            </EmptyState>
          ) : (
            <ul className="flex flex-col divide-y divide-border">
              {list.map(({ food, quantity }) => (
                <li key={food.id} className="flex items-center gap-1 py-1">
                  <button type="button" onClick={() => setSelected({ food, quantity })} className="flex min-h-14 min-w-0 flex-1 flex-col justify-center rounded-control px-2 text-left hover:bg-surface-muted">
                    <span className="truncate font-medium">{food.name}</span>
                    <span className="truncate text-sm text-fg-muted">
                      {food.serving_label} · <span className="tabular">{fmt.kcal(food.kcal)}</span>
                    </span>
                  </button>
                  <FavoriteToggle foodId={food.id} name={food.name} />
                  <button
                    type="button"
                    aria-label={`Tambah ${food.name} (${fmt.qty(quantity)} porsi)`}
                    onClick={() => void quickAdd(food, quantity)}
                    className="inline-flex size-11 shrink-0 items-center justify-center rounded-pill bg-primary-soft text-primary hover:bg-primary hover:text-on-primary"
                  >
                    <Plus className="size-5" aria-hidden />
                  </button>
                </li>
              ))}
            </ul>
          )}

          <Button variant="ghost" onClick={() => setCreating(true)} icon={<Plus className="size-5" aria-hidden />}>
            Buat makanan kustom
          </Button>
        </div>
      </Sheet>

      <CustomFoodSheet
        open={creating}
        initialName={results ? query.trim() : ''}
        onClose={() => setCreating(false)}
        onCreate={async (input) => {
          const foodId = await actions.createCustomFood(input)
          setCreating(false)
          const food: FoodItem = { ...input, id: foodId, category: 'kustom' }
          setSelected({ food, quantity: 1 })
        }}
      />
    </>
  )
}

function FavoriteToggle({ foodId, name }: { foodId: string; name: string }) {
  const state = useLocalState()
  const actions = useActions()
  const fav = foodId in state.favorites
  return (
    <button
      type="button"
      aria-label={fav ? `Hapus ${name} dari favorit` : `Jadikan ${name} favorit`}
      aria-pressed={fav}
      onClick={() => void actions.setFavorite(foodId, !fav)}
      className={`inline-flex size-11 shrink-0 items-center justify-center rounded-pill hover:bg-surface-muted ${fav ? 'text-carbs' : 'text-fg-subtle'}`}
    >
      <Star className={`size-5 ${fav ? 'fill-current' : ''}`} aria-hidden />
    </button>
  )
}

function SelectedFood({
  food,
  initialQuantity,
  initialMeal,
  date,
  onBack,
  onClose,
}: {
  food: FoodItem
  initialQuantity: number
  initialMeal: Meal
  date: string
  onBack: () => void
  onClose: () => void
}) {
  const actions = useActions()
  const toast = useToast()
  const { quantity, setQuantity, meal, setMeal } = useFoodDetailState(initialQuantity, initialMeal)
  const [saving, setSaving] = useState(false)

  return (
    <Sheet
      open
      onClose={onClose}
      title="Tambah makanan"
      footer={
        <>
          <Button variant="secondary" onClick={onBack} icon={<ArrowLeft className="size-5" aria-hidden />} aria-label="Kembali ke daftar" />
          <Button
            block
            loading={saving}
            onClick={() => {
              setSaving(true)
              void actions
                .logFood(food, { date, meal, quantity })
                .then(() => {
                  toast({ message: `${food.name} ditambahkan.` })
                  onClose()
                })
                .finally(() => setSaving(false))
            }}
          >
            Tambah ke {MEAL_LABELS[meal].toLowerCase()}
          </Button>
        </>
      }
    >
      <FoodDetail name={food.name} servingLabel={food.serving_label} perServing={food} quantity={quantity} meal={meal} onQuantity={setQuantity} onMeal={setMeal} />
    </Sheet>
  )
}
