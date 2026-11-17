import type { HTMLAttributes, ReactNode } from 'react'

export function Card({ className = '', children, ...rest }: HTMLAttributes<HTMLElement>) {
  return (
    <section className={`rounded-card bg-surface p-4 shadow-card ring-1 ring-border sm:p-5 ${className}`} {...rest}>
      {children}
    </section>
  )
}

export function CardTitle({ children, action }: { children: ReactNode; action?: ReactNode }) {
  return (
    <div className="mb-3 flex min-h-8 items-center justify-between gap-2">
      <h2 className="text-base font-semibold text-fg">{children}</h2>
      {action}
    </div>
  )
}

export function EmptyState({ icon, title, children, action }: { icon: ReactNode; title: string; children?: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center gap-2 px-4 py-8 text-center">
      <div className="mb-1 flex size-12 items-center justify-center rounded-pill bg-primary-soft text-primary" aria-hidden>
        {icon}
      </div>
      <p className="font-semibold text-fg">{title}</p>
      {children && <p className="max-w-xs text-sm text-fg-muted">{children}</p>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  )
}

export function Skeleton({ className = '' }: { className?: string }) {
  return <div className={`animate-pulse rounded-control bg-surface-muted ${className}`} aria-hidden />
}
