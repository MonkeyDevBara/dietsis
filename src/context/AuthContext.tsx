import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { api, ApiError } from '../services/api'
import type { User } from '../types'

// Real mode (VITE_GOOGLE_CLIENT_ID set): the server session cookie is the source of truth, checked via
// GET /auth/me. The last known user is cached so the app still opens offline.
// Preview mode (no client id): a local pretend user, local-only data, no sync. Clearly labelled in the UI.

export type AuthStatus = 'loading' | 'anonymous' | 'authenticated'

export interface AuthContextValue {
  status: AuthStatus
  user: User | null
  /** No Google client id configured: local-only preview, no server session, no sync. */
  preview: boolean
  /** The session ended while using the app (server answered 401); local data is kept. */
  sessionExpired: boolean
  googleClientId: string
  signInWithGoogle: (credential: string) => Promise<void>
  signInPreview: () => Promise<void>
  signOut: () => Promise<void>
  markExpired: () => void
}

const AuthContext = createContext<AuthContextValue | null>(null)

const PREVIEW_KEY = 'dietsis.preview-user'
const LAST_USER_KEY = 'dietsis.last-user'

interface SessionResponse {
  user: User
}

function readUser(key: string): User | null {
  try {
    const raw = localStorage.getItem(key)
    return raw ? (JSON.parse(raw) as User) : null
  } catch {
    return null
  }
}

export function AuthProvider({ children, googleClientId = import.meta.env.VITE_GOOGLE_CLIENT_ID ?? '' }: { children: ReactNode; googleClientId?: string }) {
  const preview = googleClientId === ''
  const [user, setUser] = useState<User | null>(() => (preview ? readUser(PREVIEW_KEY) : null))
  const [status, setStatus] = useState<AuthStatus>(preview ? (user ? 'authenticated' : 'anonymous') : 'loading')
  const [sessionExpired, setSessionExpired] = useState(false)

  useEffect(() => {
    if (preview) return
    let cancelled = false
    api<SessionResponse>('GET', '/auth/me')
      .then((s) => {
        if (cancelled) return
        localStorage.setItem(LAST_USER_KEY, JSON.stringify(s.user))
        setUser(s.user)
        setStatus('authenticated')
      })
      .catch((e: unknown) => {
        if (cancelled) return
        // Unreachable server (offline, timeout, 5xx from the proxy): fall back to the last known user.
        const cached = e instanceof ApiError && e.transient ? readUser(LAST_USER_KEY) : null
        // With a known user the app opens from local data; sync reports 401 later if the session died.
        setUser(cached)
        setStatus(cached ? 'authenticated' : 'anonymous')
      })
    return () => {
      cancelled = true
    }
  }, [preview])

  const signInWithGoogle = useCallback(async (credential: string) => {
    const s = await api<SessionResponse>('POST', '/auth/google', { credential })
    localStorage.setItem(LAST_USER_KEY, JSON.stringify(s.user))
    setSessionExpired(false)
    setUser(s.user)
    setStatus('authenticated')
  }, [])

  const signInPreview = useCallback(() => {
    const u: User = { id: 'preview', email: 'pratinjau@dietsis.local', name: 'Pengguna Pratinjau', picture: '', created_at: Date.now() }
    localStorage.setItem(PREVIEW_KEY, JSON.stringify(u))
    setUser(u)
    setStatus('authenticated')
    return Promise.resolve()
  }, [])

  const signOut = useCallback(async () => {
    if (!preview) {
      try {
        await api<undefined>('POST', '/auth/logout')
      } catch {
        // Offline: the local session is dropped anyway; the server session expires on its own.
      }
    }
    localStorage.removeItem(PREVIEW_KEY)
    localStorage.removeItem(LAST_USER_KEY)
    setUser(null)
    setStatus('anonymous')
  }, [preview])

  const markExpired = useCallback(() => {
    localStorage.removeItem(LAST_USER_KEY)
    setSessionExpired(true)
    setUser(null)
    setStatus('anonymous')
  }, [])

  const value = useMemo<AuthContextValue>(
    () => ({ status, user, preview, sessionExpired, googleClientId, signInWithGoogle, signInPreview, signOut, markExpired }),
    [status, user, preview, sessionExpired, googleClientId, signInWithGoogle, signInPreview, signOut, markExpired],
  )
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('AuthProvider belum dipasang')
  return ctx
}
