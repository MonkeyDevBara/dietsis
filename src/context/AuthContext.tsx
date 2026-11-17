import { createContext, useCallback, useContext, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import type { User } from '../types'

// M1: preview authentication so the UI can be exercised without the backend. It is explicitly
// labelled "Mode pratinjau" in the UI. M5 replaces signIn with Google Identity Services +
// POST /api/v1/auth/google and status with GET /api/v1/auth/me; the context shape stays the same.

export type AuthStatus = 'loading' | 'anonymous' | 'authenticated'

export interface AuthContextValue {
  status: AuthStatus
  user: User | null
  /** True while running without a backend session (M1 preview). */
  preview: boolean
  signIn: () => Promise<void>
  signOut: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue | null>(null)

const PREVIEW_KEY = 'dietsis.preview-user'

function loadPreviewUser(): User | null {
  try {
    const raw = localStorage.getItem(PREVIEW_KEY)
    return raw ? (JSON.parse(raw) as User) : null
  } catch {
    return null
  }
}

export function PreviewAuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(loadPreviewUser)

  const signIn = useCallback(() => {
    const u: User = { id: 'preview', email: 'pratinjau@dietsis.local', name: 'Pengguna Pratinjau', picture: '', created_at: Date.now() }
    localStorage.setItem(PREVIEW_KEY, JSON.stringify(u))
    setUser(u)
    return Promise.resolve()
  }, [])

  const signOut = useCallback(() => {
    localStorage.removeItem(PREVIEW_KEY)
    setUser(null)
    return Promise.resolve()
  }, [])

  const value = useMemo<AuthContextValue>(
    () => ({ status: user ? 'authenticated' : 'anonymous', user, preview: true, signIn, signOut }),
    [user, signIn, signOut],
  )
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('AuthProvider belum dipasang')
  return ctx
}
