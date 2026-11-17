import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react'
import type { ReactNode } from 'react'
import { Skeleton } from '../components/ui/Card'
import { api } from '../services/api'
import { IdbRepository } from '../services/local/idbRepository'
import { MemoryRepository } from '../services/local/repository'
import type { Repository } from '../services/local/repository'
import { SyncEngine } from '../services/sync/engine'
import type { SyncSnapshot } from '../services/sync/engine'
import { useAuth } from './AuthContext'
import { DataProvider } from './DataContext'
import { useToast } from './ToastContext'

// Owns the lifecycle of the signed-in user's local database and sync engine:
// open IndexedDB `dietsis-<userId>` → start syncing (real accounts only) → stop and close on logout.

interface SessionValue {
  engine: SyncEngine | null
  /** Local storage failed to open (e.g. private mode): data lives in memory only. */
  memoryOnly: boolean
  /** Syncs what is pending, ends the session, and deletes this user's local database. */
  logout: () => Promise<void>
  /** Deletes all data on the server (when syncing) and locally. Throws when the server cannot be reached. */
  deleteAllData: (() => Promise<void>) | null
}

const DEFAULT: SessionValue = { engine: null, memoryOnly: false, logout: () => Promise.resolve(), deleteAllData: null }
const SessionContext = createContext<SessionValue>(DEFAULT)

export const IDLE_SYNC: SyncSnapshot = { phase: 'idle', pending: 0, lastSyncAt: null, retryAt: null, synced: true, reauth: false }
const noopSubscribe = () => () => undefined
const getIdle = () => IDLE_SYNC

function PageSkeleton() {
  return (
    <div className="mx-auto flex max-w-xl flex-col gap-4 px-5 pt-10" aria-label="Memuat data">
      <Skeleton className="h-8 w-40" />
      <Skeleton className="h-40" />
      <Skeleton className="h-24" />
    </div>
  )
}

interface Open {
  userId: string
  repo: Repository
  idb: IdbRepository | null
  engine: SyncEngine | null
}

export function SessionProvider({ children }: { children: ReactNode }) {
  const auth = useAuth()
  const toast = useToast()
  const toastRef = useRef(toast)
  const authRef = useRef(auth)
  useEffect(() => {
    toastRef.current = toast
    authRef.current = auth
  })
  const memory = useMemo(() => new MemoryRepository(), [])
  const [open, setOpen] = useState<Open | null>(null)
  const [memoryOnly, setMemoryOnly] = useState(false)
  const discard = useRef<string | null>(null)

  const userId = auth.status === 'authenticated' ? (auth.user?.id ?? null) : null
  const preview = auth.preview

  useEffect(() => {
    if (!userId) return
    let cancelled = false
    let current: Open | null = null

    IdbRepository.open(userId)
      .then((idb) => {
        if (cancelled) {
          idb.close()
          return
        }
        const engine = preview
          ? null
          : new SyncEngine(
              idb,
              {
                onConflicts: (n) => toastRef.current({ message: n === 1 ? 'Satu perubahanmu bentrok dengan perangkat lain; data terbaru dari server dipakai.' : `${n} perubahanmu bentrok dengan perangkat lain; data terbaru dari server dipakai.` }),
                onRejected: (n) => toastRef.current({ message: `${n} perubahan ditolak server dan dibatalkan.`, tone: 'error' }),
                onUnauthorized: () => authRef.current.markExpired(),
                onReset: () => toastRef.current({ message: 'Data akun dihapus dari perangkat lain. Data di perangkat ini direset.' }),
              },
              { lockName: `dietsis-sync-${userId}` },
            )
        engine?.start()
        current = { userId, repo: idb, idb, engine }
        setMemoryOnly(false)
        setOpen(current)
      })
      .catch(() => {
        if (cancelled) return
        toastRef.current({ message: 'Penyimpanan lokal tidak tersedia. Data hanya tersimpan sementara dan tidak akan disinkronkan.', tone: 'error', duration: 10_000 })
        setMemoryOnly(true)
        current = { userId, repo: memory, idb: null, engine: null }
        setOpen(current)
      })

    return () => {
      cancelled = true
      current?.engine?.stop()
      current?.idb?.close()
      setOpen(null)
      if (discard.current === userId) {
        discard.current = null
        void IdbRepository.deleteFor(userId)
      }
    }
  }, [userId, preview, memory])

  const engine = open?.engine ?? null
  const logout = useCallback(async () => {
    try {
      // Best effort: push what is pending before the local copy is deleted.
      await engine?.syncNow()
    } finally {
      discard.current = userId
      await authRef.current.signOut()
    }
  }, [engine, userId])

  const deleteAllData = useMemo(() => {
    const idb = open?.idb
    if (!idb || !engine) return null
    return async () => {
      const res = await api<{ data_epoch: number }>('DELETE', '/account/data', { confirm: 'HAPUS' })
      await idb.reset(res.data_epoch)
    }
  }, [open, engine])

  const value = useMemo<SessionValue>(() => ({ engine, memoryOnly, logout, deleteAllData }), [engine, memoryOnly, logout, deleteAllData])

  if (userId && !open) return <PageSkeleton />
  return (
    <SessionContext.Provider value={value}>
      <DataProvider repo={open?.repo ?? memory}>{children}</DataProvider>
    </SessionContext.Provider>
  )
}

export function useSession(): SessionValue {
  return useContext(SessionContext)
}

/** Live sync state; a constant "idle, synced" snapshot when there is no engine (preview mode). */
export function useSyncSnapshot(): SyncSnapshot {
  const { engine } = useSession()
  return useSyncExternalStore(engine ? engine.subscribe : noopSubscribe, engine ? engine.getSnapshot : getIdle)
}
