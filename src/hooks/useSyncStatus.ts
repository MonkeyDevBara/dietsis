import { useSyncExternalStore } from 'react'
import type { SyncStatus } from '../components/SyncBadge'
import { useAuth } from '../context/AuthContext'
import { useSyncSnapshot } from '../context/SessionContext'

function subscribeOnline(cb: () => void): () => void {
  window.addEventListener('online', cb)
  window.addEventListener('offline', cb)
  return () => {
    window.removeEventListener('online', cb)
    window.removeEventListener('offline', cb)
  }
}

/** Network reachability hint from the browser (not proof the server is reachable). */
export function useOnline(): boolean {
  return useSyncExternalStore(subscribeOnline, () => navigator.onLine)
}

/** The status shown by the badge, derived from the sync engine (docs/OFFLINE_SYNC.md → Status). */
export function useSyncStatus(): SyncStatus {
  const { preview } = useAuth()
  const online = useOnline()
  const s = useSyncSnapshot()
  if (preview) return online ? { state: 'preview' } : { state: 'offline', pending: 0 }
  if (s.reauth) return { state: 'reauth' }
  if (!online || s.phase === 'offline') return { state: 'offline', pending: s.pending }
  if (s.phase === 'error') return { state: 'error', pending: s.pending }
  if (s.phase === 'syncing' || s.pending > 0) return { state: 'syncing' }
  return { state: 'synced' }
}
