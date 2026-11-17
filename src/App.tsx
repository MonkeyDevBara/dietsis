import { lazy, Suspense } from 'react'
import type { ReactNode } from 'react'
import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router'
import { AppShell } from './components/AppShell'
import { Skeleton } from './components/ui/Card'
import { useAuth } from './context/AuthContext'
import { useLocalState } from './context/DataContext'
import { useSession, useSyncSnapshot } from './context/SessionContext'
import { useOnline } from './hooks/useSyncStatus'
import { DashboardPage } from './pages/DashboardPage'

const LoginPage = lazy(() => import('./pages/LoginPage'))
const OnboardingPage = lazy(() => import('./pages/OnboardingPage'))
const DiaryPage = lazy(() => import('./pages/DiaryPage'))
const ProgressPage = lazy(() => import('./pages/ProgressPage'))
const ProfilePage = lazy(() => import('./pages/ProfilePage'))
const SettingsPage = lazy(() => import('./pages/SettingsPage'))
const NotFoundPage = lazy(() => import('./pages/NotFoundPage'))

function PageFallback() {
  return (
    <div className="flex flex-col gap-4 pt-6" aria-label="Memuat">
      <Skeleton className="h-8 w-40" />
      <Skeleton className="h-40" />
      <Skeleton className="h-24" />
    </div>
  )
}

function RequireAuth({ children }: { children: ReactNode }) {
  const { status } = useAuth()
  const location = useLocation()
  if (status === 'loading') return <PageFallback />
  if (status === 'anonymous') return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />
  return children
}

/**
 * A new device must not show onboarding before the first pull finishes: the account may already have
 * a profile on the server, and a second onboarding would overwrite it.
 */
function SyncGate() {
  const online = useOnline()
  const { engine } = useSession()
  return (
    <main className="mx-auto flex min-h-dvh max-w-sm flex-col items-center justify-center gap-3 px-6 text-center">
      <p className="font-semibold">{online ? 'Menyinkronkan datamu…' : 'Butuh internet sebentar'}</p>
      <p className="text-sm text-fg-muted">{online ? 'Mengambil data dari server.' : 'Perangkat ini belum punya data akunmu. Sambungkan internet untuk mengambilnya.'}</p>
      <button type="button" className="min-h-11 rounded-control px-4 font-semibold text-primary hover:bg-primary-soft" onClick={() => void engine?.syncNow()}>
        Coba lagi
      </button>
    </main>
  )
}

function RequireProfile({ children }: { children: ReactNode }) {
  const { profile } = useLocalState()
  const { engine } = useSession()
  const sync = useSyncSnapshot()
  if (profile) return children
  if (engine && !sync.synced) return <SyncGate />
  return <Navigate to="/onboarding" replace />
}

export function AppRoutes() {
  return (
    <Suspense fallback={<PageFallback />}>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route
          path="/onboarding"
          element={
            <RequireAuth>
              <OnboardingPage />
            </RequireAuth>
          }
        />
        <Route
          element={
            <RequireAuth>
              <RequireProfile>
                <AppShell />
              </RequireProfile>
            </RequireAuth>
          }
        >
          <Route index element={<DashboardPage />} />
          <Route path="diary" element={<DiaryPage />} />
          <Route path="progress" element={<ProgressPage />} />
          <Route path="profile" element={<ProfilePage />} />
          <Route path="settings" element={<SettingsPage />} />
          <Route path="*" element={<NotFoundPage />} />
        </Route>
      </Routes>
    </Suspense>
  )
}

export function App() {
  return (
    <BrowserRouter>
      <AppRoutes />
    </BrowserRouter>
  )
}
