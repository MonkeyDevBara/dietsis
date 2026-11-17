import { ChartLine, House, NotebookPen, UserRound } from 'lucide-react'
import type { ReactNode } from 'react'
import { NavLink, Outlet } from 'react-router'

const TABS = [
  { to: '/', label: 'Beranda', icon: House, end: true },
  { to: '/diary', label: 'Diary', icon: NotebookPen, end: false },
  { to: '/progress', label: 'Progres', icon: ChartLine, end: false },
  { to: '/profile', label: 'Profil', icon: UserRound, end: false },
] as const

/** Bottom navigation below 1024px, side rail above (docs/DESIGN.md → Layout responsif). */
export function AppShell() {
  return (
    <div className="min-h-dvh lg:pl-56">
      <nav aria-label="Navigasi utama" className="fixed inset-y-0 left-0 z-30 hidden w-56 flex-col gap-1 border-r border-border bg-surface px-3 py-6 lg:flex">
        <div className="mb-6 flex items-center gap-2.5 px-3">
          <img src="/favicon.svg" alt="" className="size-8 rounded-[0.6rem]" />
          <span className="text-lg font-semibold tracking-tight">Dietsis</span>
        </div>
        {TABS.map(({ to, label, icon: Icon, end }) => (
          <NavLink
            key={to}
            to={to}
            end={end}
            className={({ isActive }) =>
              `flex min-h-11 items-center gap-3 rounded-control px-3 text-sm font-medium transition-colors ${
                isActive ? 'bg-primary-soft text-primary' : 'text-fg-muted hover:bg-surface-muted hover:text-fg'
              }`
            }
          >
            <Icon className="size-5" aria-hidden />
            {label}
          </NavLink>
        ))}
      </nav>

      <main className="mx-auto w-full max-w-xl px-5 pb-28 pt-safe md:max-w-2xl lg:max-w-3xl lg:pb-10">
        <Outlet />
      </main>

      <nav
        aria-label="Navigasi utama"
        className="fixed inset-x-0 bottom-0 z-30 border-t border-border bg-surface/95 pb-[env(safe-area-inset-bottom)] backdrop-blur lg:hidden"
      >
        <ul className="mx-auto flex max-w-xl">
          {TABS.map(({ to, label, icon: Icon, end }) => (
            <li key={to} className="flex-1">
              <NavLink
                to={to}
                end={end}
                className={({ isActive }) =>
                  `flex min-h-16 flex-col items-center justify-center gap-1 text-xs font-medium transition-colors ${isActive ? 'text-primary' : 'text-fg-muted hover:text-fg'}`
                }
              >
                {({ isActive }) => (
                  <>
                    <span className={`flex h-7 w-14 items-center justify-center rounded-pill transition-colors ${isActive ? 'bg-primary-soft' : ''}`}>
                      <Icon className="size-5" aria-hidden />
                    </span>
                    {label}
                  </>
                )}
              </NavLink>
            </li>
          ))}
        </ul>
      </nav>
    </div>
  )
}

export function PageHeader({ title, subtitle, actions }: { title: string; subtitle?: ReactNode; actions?: ReactNode }) {
  return (
    <header className="mb-5 flex items-start justify-between gap-3 pt-2">
      <div className="min-w-0">
        <h1 className="truncate text-2xl font-semibold tracking-tight">{title}</h1>
        {subtitle && <div className="mt-1 text-sm text-fg-muted">{subtitle}</div>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-1">{actions}</div>}
    </header>
  )
}
