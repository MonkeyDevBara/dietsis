import { Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useActions } from '../../context/DataContext'
import { useToast } from '../../context/ToastContext'
import type { FoodLog } from '../../types'
import { Button } from '../ui/Button'
import { Sheet } from '../ui/Sheet'
import { FoodDetail, useFoodDetailState } from './FoodDetail'

interface FoodEntrySheetProps {
  log: FoodLog | null
  onClose: () => void
}

/** Edit or delete an existing food log. Delete is immediate with an "Urungkan" toast (restore). */
export function FoodEntrySheet({ log, onClose }: FoodEntrySheetProps) {
  return log ? <EntryForm key={log.id} log={log} onClose={onClose} /> : null
}

function EntryForm({ log, onClose }: { log: FoodLog; onClose: () => void }) {
  const actions = useActions()
  const toast = useToast()
  const { quantity, setQuantity, meal, setMeal } = useFoodDetailState(log.quantity, log.meal)
  const [saving, setSaving] = useState(false)
  const changed = quantity !== log.quantity || meal !== log.meal

  return (
    <Sheet
      open
      onClose={onClose}
      title="Ubah catatan"
      footer={
        <>
          <Button
            variant="secondary"
            aria-label="Hapus catatan"
            icon={<Trash2 className="size-5 text-danger" aria-hidden />}
            onClick={() => {
              void actions.deleteFoodLog(log.id)
              onClose()
              toast({ message: `${log.name} dihapus.`, action: { label: 'Urungkan', onClick: () => void actions.restoreFoodLog(log.id) } })
            }}
          />
          <Button
            block
            disabled={!changed}
            loading={saving}
            onClick={() => {
              setSaving(true)
              void actions
                .updateFoodLog(log.id, { quantity, meal })
                .then(() => {
                  toast({ message: 'Perubahan disimpan.' })
                  onClose()
                })
                .finally(() => setSaving(false))
            }}
          >
            Simpan
          </Button>
        </>
      }
    >
      <FoodDetail name={log.name} servingLabel={log.serving_label} perServing={log} quantity={quantity} meal={meal} onQuantity={setQuantity} onMeal={setMeal} />
    </Sheet>
  )
}
