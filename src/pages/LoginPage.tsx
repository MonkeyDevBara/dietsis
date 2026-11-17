import { Droplets, Flame, WifiOff } from 'lucide-react'
import { Navigate, useLocation } from 'react-router'
import { Button } from '../components/ui/Button'
import { useAuth } from '../context/AuthContext'

export default function LoginPage() {
  const { status, signIn, preview } = useAuth()
  const location = useLocation()
  const from = (location.state as { from?: string } | null)?.from ?? '/'

  if (status === 'authenticated') return <Navigate to={from} replace />

  return (
    <main className="mx-auto flex min-h-dvh max-w-md flex-col justify-center gap-8 px-6 pb-safe pt-safe">
      <div className="flex flex-col items-center gap-3 text-center">
        <img src="/favicon.svg" alt="" className="size-20 rounded-card shadow-card" />
        <h1 className="text-3xl font-semibold tracking-tight">Dietsis</h1>
        <p className="text-fg-muted">Teman diet harianmu.</p>
      </div>

      <ul className="flex flex-col gap-3 text-sm text-fg-muted">
        <li className="flex items-center gap-3">
          <Flame className="size-5 shrink-0 text-kcal" aria-hidden /> Catat makanan dan lihat kalori serta makro otomatis.
        </li>
        <li className="flex items-center gap-3">
          <Droplets className="size-5 shrink-0 text-water" aria-hidden /> Pantau air minum dan progres berat badan.
        </li>
        <li className="flex items-center gap-3">
          <WifiOff className="size-5 shrink-0 text-carbs" aria-hidden /> Tetap jalan tanpa internet; data tersinkron saat online.
        </li>
      </ul>

      <div className="flex flex-col gap-3">
        <Button block onClick={() => void signIn()}>
          {preview ? 'Masuk (mode pratinjau)' : 'Masuk dengan Google'}
        </Button>
        {preview && (
          <p className="text-center text-xs text-fg-subtle">
            Mode pratinjau: data hanya tersimpan sementara di tab ini. Login Google dan sinkronisasi aktif setelah backend terhubung.
          </p>
        )}
      </div>
    </main>
  )
}
