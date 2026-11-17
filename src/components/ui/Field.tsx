import { useId } from 'react'
import type { InputHTMLAttributes, ReactNode, TextareaHTMLAttributes } from 'react'

const INPUT =
  'w-full min-h-11 rounded-control bg-surface-muted px-3.5 text-fg placeholder:text-fg-subtle ring-1 ring-transparent transition-shadow focus:outline-none focus:ring-2 focus:ring-primary aria-[invalid=true]:ring-danger'

interface FieldProps {
  label: string
  error?: string | undefined
  hint?: ReactNode
  suffix?: string
  children: (props: { id: string; 'aria-invalid': boolean; 'aria-describedby': string | undefined; className: string }) => ReactNode
}

/** Label above, error/hint below, wired with aria-describedby. */
export function Field({ label, error, hint, suffix, children }: FieldProps) {
  const id = useId()
  const descId = `${id}-desc`
  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={id} className="text-sm font-medium text-fg-muted">
        {label}
      </label>
      <div className="relative">
        {children({
          id,
          'aria-invalid': Boolean(error),
          'aria-describedby': error || hint ? descId : undefined,
          className: `${INPUT} ${suffix ? 'pr-14' : ''}`,
        })}
        {suffix && <span className="pointer-events-none absolute inset-y-0 right-3.5 flex items-center text-sm text-fg-subtle">{suffix}</span>}
      </div>
      {(error || hint) && (
        <p id={descId} className={`text-sm ${error ? 'text-danger' : 'text-fg-subtle'}`}>
          {error ?? hint}
        </p>
      )}
    </div>
  )
}

type InputProps = Omit<InputHTMLAttributes<HTMLInputElement>, 'id' | 'className'> & {
  label: string
  error?: string | undefined
  hint?: ReactNode
  suffix?: string
}

export function TextField({ label, error, hint, suffix, ...rest }: InputProps) {
  return (
    <Field label={label} error={error} hint={hint} {...(suffix ? { suffix } : {})}>
      {(p) => <input type="text" {...p} {...rest} />}
    </Field>
  )
}

/** Text input tuned for numbers: decimal keypad on mobile, accepts "72,5". Parse with parseNumber(). */
export function NumberField({ label, error, hint, suffix, ...rest }: InputProps) {
  return (
    <Field label={label} error={error} hint={hint} {...(suffix ? { suffix } : {})}>
      {(p) => <input type="text" inputMode="decimal" autoComplete="off" {...p} {...rest} />}
    </Field>
  )
}

type TextareaProps = Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, 'id' | 'className'> & { label: string; error?: string | undefined; hint?: ReactNode }

export function TextareaField({ label, error, hint, ...rest }: TextareaProps) {
  return (
    <Field label={label} error={error} hint={hint}>
      {(p) => <textarea {...p} className={`${p.className} min-h-24 py-2.5`} {...rest} />}
    </Field>
  )
}
