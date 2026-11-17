import { useSyncExternalStore } from 'react'
import type { SyncStatus } from '../components/SyncBadge'
import { useAuth } from '../context/AuthContext'

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

/**
 * M1: there is no sync engine yet, so this reports preview mode (or offline). M6 replaces the body
 * with the engine's real state (pending count, backoff, reauth) without changing the return type.
 */
export function useSyncStatus(): SyncStatus {
  const { preview } = useAuth()
  const online = useOnline()
  if (!online) return { state: 'offline', pending: 0 }
  if (preview) return { state: 'preview' }
  return { state: 'synced' }
}
