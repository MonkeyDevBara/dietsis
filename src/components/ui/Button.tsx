import type { ButtonHTMLAttributes, ReactNode } from 'react'
import { LoaderCircle } from 'lucide-react'

type Variant = 'primary' | 'secondary' | 'ghost' | 'danger'
type Size = 'md' | 'sm'

const VARIANTS: Record<Variant, string> = {
  primary: 'bg-primary text-on-primary hover:bg-primary-strong',
  secondary: 'bg-surface text-fg ring-1 ring-border hover:bg-surface-muted',
  ghost: 'text-primary hover:bg-primary-soft',
  danger: 'bg-danger text-on-primary hover:opacity-90',
}

const SIZES: Record<Size, string> = {
  md: 'min-h-11 px-4 text-base',
  sm: 'min-h-9 px-3 text-sm',
}

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant
  size?: Size
  loading?: boolean
  icon?: ReactNode
  block?: boolean
}

export function Button({ variant = 'primary', size = 'md', loading = false, icon, block, className = '', children, disabled, type = 'button', ...rest }: ButtonProps) {
  return (
    <button
      type={type}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      className={`inline-flex items-center justify-center gap-2 rounded-control font-semibold transition-colors duration-150 disabled:cursor-not-allowed disabled:opacity-50 ${VARIANTS[variant]} ${SIZES[size]} ${block ? 'w-full' : ''} ${className}`}
      {...rest}
    >
      {loading ? <LoaderCircle className="size-5 animate-spin" aria-hidden /> : icon}
      {children}
    </button>
  )
}

export interface IconButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  label: string
  tone?: 'default' | 'primary' | 'danger'
}

const TONES = {
  default: 'text-fg-muted hover:bg-surface-muted hover:text-fg',
  primary: 'text-primary hover:bg-primary-soft',
  danger: 'text-danger hover:bg-danger-soft',
}

export function IconButton({ label, tone = 'default', className = '', children, type = 'button', ...rest }: IconButtonProps) {
  return (
    <button
      type={type}
      aria-label={label}
      title={label}
      className={`inline-flex size-11 shrink-0 items-center justify-center rounded-pill transition-colors duration-150 disabled:opacity-40 ${TONES[tone]} ${className}`}
      {...rest}
    >
      {children}
    </button>
  )
}
