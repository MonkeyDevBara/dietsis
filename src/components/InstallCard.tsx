import { Download, Share, SquarePlus, X } from 'lucide-react'
import { dismissInstallHint, installable, promptInstall, useInstall } from '../lib/install'
import { Button, IconButton } from './ui/Button'
import { Card } from './ui/Card'

/**
 * "Pasang di layar utama". `hint` (Beranda) is shown until dismissed; `settings` stays available
 * for anyone who dismissed it. Renders nothing once the app is installed or cannot be installed.
 */
export function InstallCard({ variant = 'hint' }: { variant?: 'hint' | 'settings' }) {
  const s = useInstall()
  if (!installable(s) || (variant === 'hint' && s.dismissed)) return null

  return (
    <Card aria-label="Pasang aplikasi" className="relative bg-primary-soft ring-primary/30">
      {variant === 'hint' && (
        <IconButton label="Tutup saran pasang aplikasi" onClick={dismissInstallHint} className="absolute right-1 top-1">
          <X className="size-5" aria-hidden />
        </IconButton>
      )}
      <div className="flex items-start gap-3 pr-8">
        <span className="flex size-10 shrink-0 items-center justify-center rounded-pill bg-primary text-on-primary" aria-hidden>
          <Download className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <h2 className="font-semibold">Pasang Dietsis di layar utama</h2>
          <p className="mt-0.5 text-sm text-fg-muted">Buka lebih cepat seperti aplikasi biasa, layar penuh, dan tetap jalan saat offline.</p>

          {s.canPrompt ? (
            <Button className="mt-3" size="sm" icon={<Download className="size-4" aria-hidden />} onClick={() => void promptInstall()}>
              Pasang sekarang
            </Button>
          ) : (
            <ol className="mt-3 flex flex-col gap-2 text-sm">
              <li className="flex items-center gap-2">
                <Share className="size-5 shrink-0 text-primary" aria-hidden />
                <span>
                  Ketuk tombol <b>Bagikan</b> di bilah Safari.
                </span>
              </li>
              <li className="flex items-center gap-2">
                <SquarePlus className="size-5 shrink-0 text-primary" aria-hidden />
                <span>
                  Pilih <b>Tambahkan ke Layar Utama</b>, lalu <b>Tambah</b>.
                </span>
              </li>
            </ol>
          )}
        </div>
      </div>
    </Card>
  )
}
