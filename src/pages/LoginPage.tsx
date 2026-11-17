import { Droplets, Flame, WifiOff } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { Navigate, useLocation } from 'react-router'
import { Button } from '../components/ui/Button'
import { useAuth } from '../context/AuthContext'
import { loadGoogleIdentity } from '../lib/google'
import { ApiError } from '../services/api'

function GoogleButton({ clientId, onCredential }: { clientId: string; onCredential: (credential: string) => void }) {
  const box = useRef<HTMLDivElement>(null)
  const [error, setError] = useState<string | null>(null)
  const cb = useRef(onCredential)
  useEffect(() => {
    cb.current = onCredential
  })

  useEffect(() => {
    let cancelled = false
    loadGoogleIdentity()
      .then((gid) => {
        if (cancelled || !box.current) return
        gid.initialize({ client_id: clientId, callback: (r) => cb.current(r.credential) })
        gid.renderButton(box.current, { type: 'standard', theme: 'outline', size: 'large', text: 'signin_with', shape: 'pill', width: 280, logo_alignment: 'left' })
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(e instanceof Error ? e.message : 'Tidak dapat memuat Google.')
      })
    return () => {
      cancelled = true
    }
  }, [clientId])

  return (
    <div className="flex flex-col items-center gap-2">
      <div ref={box} className="min-h-11" />
      {error && (
        <p role="alert" className="text-center text-sm text-danger">
          {error}
        </p>
      )}
    </div>
  )
}

export default function LoginPage() {
  const { status, signInPreview, signInWithGoogle, preview, googleClientId, sessionExpired } = useAuth()
  const location = useLocation()
  const from = (location.state as { from?: string } | null)?.from ?? '/'
  const [error, setError] = useState<string | null>(null)

  if (status === 'authenticated') return <Navigate to={from} replace />

  function onCredential(credential: string) {
    setError(null)
    signInWithGoogle(credential).catch((e: unknown) => {
      setError(e instanceof ApiError ? e.message : 'Login gagal. Coba lagi.')
    })
  }

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
        {sessionExpired && (
          <p role="status" className="rounded-control bg-warning-soft px-3 py-2 text-center text-sm text-warning">
            Sesimu berakhir. Masuk lagi untuk melanjutkan sinkronisasi; data di perangkat ini tetap aman.
          </p>
        )}
        {status === 'loading' ? (
          <p className="text-center text-sm text-fg-muted">Memeriksa sesi…</p>
        ) : preview ? (
          <>
            <Button block onClick={() => void signInPreview()}>
              Masuk (mode pratinjau)
            </Button>
            <p className="text-center text-xs text-fg-subtle">Mode pratinjau: data hanya tersimpan di perangkat ini dan tidak disinkronkan. Login Google aktif setelah VITE_GOOGLE_CLIENT_ID diisi.</p>
          </>
        ) : (
          <GoogleButton clientId={googleClientId} onCredential={onCredential} />
        )}
        {error && (
          <p role="alert" className="text-center text-sm text-danger">
            {error}
          </p>
        )}
      </div>
    </main>
  )
}
