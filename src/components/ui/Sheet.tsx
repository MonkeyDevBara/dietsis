import { X } from 'lucide-react'
import { useEffect, useId, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { Button } from './Button'
import { TextField } from './Field'

const FOCUSABLE = 'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])'

interface SheetProps {
  open: boolean
  onClose: () => void
  title: string
  children: ReactNode
  footer?: ReactNode
  /** 'sheet' slides from the bottom on mobile (centered card on desktop); 'dialog' is always centered. */
  variant?: 'sheet' | 'dialog'
}

/**
 * Modal surface: role=dialog + aria-modal, Escape and backdrop close, focus moves in on open,
 * Tab is trapped, focus returns to the opener on close, body scroll is locked.
 */
export function Sheet({ open, onClose, title, children, footer, variant = 'sheet' }: SheetProps) {
  const panel = useRef<HTMLDivElement>(null)
  const titleId = useId()
  const onCloseRef = useRef(onClose)
  useEffect(() => {
    onCloseRef.current = onClose
  })

  useEffect(() => {
    if (!open) return
    const opener = document.activeElement as HTMLElement | null
    const node = panel.current
    const first = node?.querySelector<HTMLElement>('[data-autofocus]') ?? node?.querySelector<HTMLElement>(FOCUSABLE)
    first?.focus()
    const prevOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'

    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.stopPropagation()
        onCloseRef.current()
        return
      }
      if (e.key !== 'Tab' || !node) return
      const items = [...node.querySelectorAll<HTMLElement>(FOCUSABLE)]
      const firstItem = items[0]
      const lastItem = items[items.length - 1]
      if (!firstItem || !lastItem) return
      if (e.shiftKey && document.activeElement === firstItem) {
        e.preventDefault()
        lastItem.focus()
      } else if (!e.shiftKey && document.activeElement === lastItem) {
        e.preventDefault()
        firstItem.focus()
      }
    }
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = prevOverflow
      opener?.focus()
    }
  }, [open])

  if (!open) return null

  const isDialog = variant === 'dialog'
  return createPortal(
    <div className={`fixed inset-0 z-40 flex justify-center ${isDialog ? 'items-center p-4' : 'items-end sm:items-center sm:p-4'}`}>
      <div className="absolute inset-0 bg-black/40 motion-safe:animate-[fade-in_150ms_ease-out]" onClick={onClose} aria-hidden />
      <div
        ref={panel}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className={`relative flex max-h-[92dvh] w-full flex-col bg-surface shadow-sheet motion-safe:animate-[sheet-in_200ms_cubic-bezier(0.2,0,0,1)] ${
          isDialog ? 'max-w-sm rounded-card' : 'max-w-lg rounded-t-sheet sm:rounded-sheet'
        }`}
      >
        <header className="flex items-center gap-2 border-b border-border py-2 pl-5 pr-2">
          <h2 id={titleId} className="flex-1 text-base font-semibold">
            {title}
          </h2>
          <button type="button" onClick={onClose} aria-label="Tutup" className="inline-flex size-11 items-center justify-center rounded-pill text-fg-muted hover:bg-surface-muted">
            <X className="size-5" aria-hidden />
          </button>
        </header>
        <div className="flex-1 overflow-y-auto overscroll-contain px-5 py-4">{children}</div>
        {footer && <footer className="flex gap-2 border-t border-border px-5 pb-safe pt-3">{footer}</footer>}
      </div>
    </div>,
    document.body,
  )
}

interface ConfirmDialogProps {
  open: boolean
  title: string
  message: ReactNode
  confirmLabel: string
  onConfirm: () => void | Promise<void>
  onClose: () => void
  danger?: boolean
  /** When set, the user must type this word exactly before confirming (irreversible actions). */
  typeToConfirm?: string
}

export function ConfirmDialog({ open, title, message, confirmLabel, onConfirm, onClose, danger, typeToConfirm }: ConfirmDialogProps) {
  const [typed, setTyped] = useState('')
  const [busy, setBusy] = useState(false)
  const ready = !typeToConfirm || typed.trim() === typeToConfirm

  function close() {
    setTyped('')
    onClose()
  }

  return (
    <Sheet
      open={open}
      onClose={close}
      title={title}
      variant="dialog"
      footer={
        <>
          <Button variant="secondary" block onClick={close}>
            Batal
          </Button>
          <Button
            variant={danger ? 'danger' : 'primary'}
            block
            disabled={!ready}
            loading={busy}
            onClick={() => {
              setBusy(true)
              void Promise.resolve(onConfirm()).finally(() => {
                setBusy(false)
                setTyped('')
              })
            }}
          >
            {confirmLabel}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4 text-fg-muted">
        <div>{message}</div>
        {typeToConfirm && (
          <TextField
            label={`Ketik ${typeToConfirm} untuk melanjutkan`}
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            autoCapitalize="characters"
            autoComplete="off"
            data-autofocus
          />
        )}
      </div>
    </Sheet>
  )
}
