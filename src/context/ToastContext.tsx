import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'

export interface ToastOptions {
  message: string
  tone?: 'default' | 'error'
  action?: { label: string; onClick: () => void }
  /** Milliseconds; default 4000, 6000 when there is an action. */
  duration?: number
}

interface Toast extends ToastOptions {
  id: number
}

const ToastContext = createContext<((t: ToastOptions) => void) | null>(null)

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toast, setToast] = useState<Toast | null>(null)
  const nextId = useRef(1)

  const show = useCallback((t: ToastOptions) => {
    setToast({ ...t, id: nextId.current++ })
  }, [])

  useEffect(() => {
    if (!toast) return
    const ms = toast.duration ?? (toast.action ? 6000 : 4000)
    const timer = setTimeout(() => {
      setToast((cur) => (cur?.id === toast.id ? null : cur))
    }, ms)
    return () => clearTimeout(timer)
  }, [toast])

  const value = useMemo(() => show, [show])

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div aria-live="polite" role="status" className="pointer-events-none fixed inset-x-0 bottom-[calc(5.5rem+env(safe-area-inset-bottom))] z-50 flex justify-center px-4 lg:bottom-6">
        {toast && (
          <div
            key={toast.id}
            className={`pointer-events-auto flex w-full max-w-md items-center gap-3 rounded-control px-4 py-3 text-sm shadow-sheet ${
              toast.tone === 'error' ? 'bg-danger text-on-primary' : 'bg-fg text-bg'
            }`}
          >
            <span className="flex-1">{toast.message}</span>
            {toast.action && (
              <button
                type="button"
                className="-my-2 min-h-11 rounded-control px-2 font-semibold underline-offset-2 hover:underline"
                onClick={() => {
                  toast.action?.onClick()
                  setToast(null)
                }}
              >
                {toast.action.label}
              </button>
            )}
          </div>
        )}
      </div>
    </ToastContext.Provider>
  )
}

export function useToast(): (t: ToastOptions) => void {
  const ctx = useContext(ToastContext)
  if (!ctx) throw new Error('ToastProvider belum dipasang')
  return ctx
}
