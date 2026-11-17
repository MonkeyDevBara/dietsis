import { ArrowLeft, Download, LogOut, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { PageHeader } from '../components/AppShell'
import { SyncBadge } from '../components/SyncBadge'
import { Button } from '../components/ui/Button'
import { Card, CardTitle } from '../components/ui/Card'
import { ConfirmDialog } from '../components/ui/Sheet'
import { useAuth } from '../context/AuthContext'
import { useActions, useLocalState } from '../context/DataContext'
import { useToast } from '../context/ToastContext'
import { todayKey } from '../lib/date'
import { downloadText, foodLogsCsv, weightsCsv } from '../lib/csv'
import { useSyncStatus } from '../hooks/useSyncStatus'

export default function SettingsPage() {
  const { user, signOut, preview } = useAuth()
  const state = useLocalState()
  const actions = useActions()
  const toast = useToast()
  const navigate = useNavigate()
  const sync = useSyncStatus()
  const [confirm, setConfirm] = useState<'logout' | 'delete' | null>(null)

  function exportCsv(kind: 'food' | 'weight') {
    const date = todayKey()
    if (kind === 'food') downloadText(`dietsis-makanan-${date}.csv`, foodLogsCsv(Object.values(state.foodLogs)))
    else downloadText(`dietsis-berat-${date}.csv`, weightsCsv(Object.values(state.weights)))
    toast({ message: 'File CSV diunduh.' })
  }

  return (
    <>
      <PageHeader
        title="Pengaturan"
        actions={
          <Link to="/profile" aria-label="Kembali ke profil" title="Kembali" className="inline-flex size-11 items-center justify-center rounded-pill text-fg-muted hover:bg-surface-muted">
            <ArrowLeft className="size-5" aria-hidden />
          </Link>
        }
      />

      <div className="flex flex-col gap-4">
        <Card>
          <CardTitle>Akun</CardTitle>
          <div className="flex items-center gap-3">
            {user?.picture ? (
              <img src={user.picture} alt="" referrerPolicy="no-referrer" className="size-12 rounded-pill" />
            ) : (
              <div className="flex size-12 items-center justify-center rounded-pill bg-primary-soft text-lg font-semibold text-primary" aria-hidden>
                {user?.name.charAt(0).toUpperCase()}
              </div>
            )}
            <div className="min-w-0">
              <p className="truncate font-medium">{user?.name}</p>
              <p className="truncate text-sm text-fg-muted">{user?.email}</p>
            </div>
          </div>
          {preview && <p className="mt-3 rounded-control bg-warning-soft px-3 py-2 text-sm text-warning">Mode pratinjau: belum terhubung ke akun Google dan server.</p>}
        </Card>

        <Card>
          <CardTitle action={<SyncBadge status={sync} />}>Sinkronisasi</CardTitle>
          <p className="text-sm text-fg-muted">
            Perubahan disimpan di perangkat lebih dulu, lalu dikirim ke server saat online. Jumlah perubahan tertunda dan waktu sinkron terakhir akan tampil di sini setelah sinkronisasi aktif.
          </p>
        </Card>

        <Card>
          <CardTitle>Ekspor data</CardTitle>
          <div className="flex flex-col gap-2 sm:flex-row">
            <Button variant="secondary" icon={<Download className="size-5" aria-hidden />} onClick={() => exportCsv('food')}>
              Catatan makanan (CSV)
            </Button>
            <Button variant="secondary" icon={<Download className="size-5" aria-hidden />} onClick={() => exportCsv('weight')}>
              Berat badan (CSV)
            </Button>
          </div>
        </Card>

        <Card>
          <CardTitle>Sesi dan data</CardTitle>
          <div className="flex flex-col gap-2">
            <Button variant="secondary" icon={<LogOut className="size-5" aria-hidden />} onClick={() => setConfirm('logout')}>
              Keluar
            </Button>
            <Button variant="danger" icon={<Trash2 className="size-5" aria-hidden />} onClick={() => setConfirm('delete')}>
              Hapus seluruh data
            </Button>
          </div>
        </Card>

        <p className="px-1 text-center text-xs text-fg-subtle">Dietsis · Estimasi nutrisi bersifat perkiraan, bukan saran medis.</p>
      </div>

      <ConfirmDialog
        open={confirm === 'logout'}
        title="Keluar dari Dietsis?"
        message={preview ? 'Di mode pratinjau, data sementara di tab ini tidak akan tersimpan setelah keluar.' : 'Data di perangkat ini akan dibersihkan setelah semua perubahan tersinkron.'}
        confirmLabel="Keluar"
        onClose={() => setConfirm(null)}
        onConfirm={async () => {
          await actions.deleteAllData()
          await signOut()
          void navigate('/login', { replace: true })
        }}
      />
      <ConfirmDialog
        open={confirm === 'delete'}
        title="Hapus seluruh data?"
        message="Profil, catatan makanan, air, berat badan, favorit, dan makanan kustom akan dihapus permanen dan tidak bisa dikembalikan. Akun Google-mu tetap ada."
        confirmLabel="Hapus semua"
        danger
        typeToConfirm="HAPUS"
        onClose={() => setConfirm(null)}
        onConfirm={async () => {
          await actions.deleteAllData()
          setConfirm(null)
          toast({ message: 'Semua data dihapus.' })
          void navigate('/onboarding', { replace: true })
        }}
      />
    </>
  )
}
