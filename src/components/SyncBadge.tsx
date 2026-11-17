import { CloudOff, CloudCheck, HardDrive, LoaderCircle, LogIn, TriangleAlert } from 'lucide-react'

export type SyncStatus =
  | { state: 'preview' }
  | { state: 'synced' }
  | { state: 'syncing' }
  | { state: 'offline'; pending: number }
  | { state: 'error'; pending: number }
  | { state: 'reauth' }

export function syncLabel(s: SyncStatus): string {
  switch (s.state) {
    case 'preview':
      return 'Pratinjau · lokal'
    case 'synced':
      return 'Tersinkron'
    case 'syncing':
      return 'Menyinkronkan…'
    case 'offline':
      return s.pending > 0 ? `Offline · ${s.pending} tertunda` : 'Offline'
    case 'error':
      return `Gagal sinkron · ${s.pending} tertunda`
    case 'reauth':
      return 'Perlu login ulang'
  }
}

const STYLE: Record<SyncStatus['state'], string> = {
  preview: 'bg-surface-muted text-fg-muted',
  synced: 'bg-primary-soft text-primary',
  syncing: 'bg-water-soft text-water',
  offline: 'bg-warning-soft text-warning',
  error: 'bg-danger-soft text-danger',
  reauth: 'bg-danger-soft text-danger',
}

export function SyncBadge({ status }: { status: SyncStatus }) {
  const Icon = {
    preview: HardDrive,
    synced: CloudCheck,
    syncing: LoaderCircle,
    offline: CloudOff,
    error: TriangleAlert,
    reauth: LogIn,
  }[status.state]
  return (
    <span className={`inline-flex items-center gap-1.5 rounded-pill px-2.5 py-1 text-xs font-medium ${STYLE[status.state]}`}>
      <Icon className={`size-3.5 ${status.state === 'syncing' ? 'animate-spin' : ''}`} aria-hidden />
      {syncLabel(status)}
    </span>
  )
}
