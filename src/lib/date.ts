// Date keys are calendar days in the device's local timezone, formatted YYYY-MM-DD.
// The server stores and validates the same string format; it never converts timezones.

const DATE_KEY_RE = /^(\d{4})-(\d{2})-(\d{2})$/

export type DateKey = string

function pad(n: number): string {
  return n < 10 ? `0${n}` : String(n)
}

export function toDateKey(date: Date): DateKey {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

export function todayKey(now: Date = new Date()): DateKey {
  return toDateKey(now)
}

/** Returns true only for real calendar dates (rejects 2025-02-30, 2025-13-01, ...). */
export function isDateKey(value: string): boolean {
  const m = DATE_KEY_RE.exec(value)
  if (!m) return false
  const y = Number(m[1])
  const mo = Number(m[2])
  const d = Number(m[3])
  const date = new Date(y, mo - 1, d)
  return date.getFullYear() === y && date.getMonth() === mo - 1 && date.getDate() === d
}

/** Parses a key into a local Date at 00:00. Throws on invalid input. */
export function parseDateKey(key: DateKey): Date {
  if (!isDateKey(key)) throw new RangeError(`Tanggal tidak valid: ${key}`)
  const [y, m, d] = key.split('-').map(Number) as [number, number, number]
  return new Date(y, m - 1, d)
}

/** Adds calendar days; safe across DST transitions because it works on Y/M/D, not milliseconds. */
export function addDays(key: DateKey, days: number): DateKey {
  const date = parseDateKey(key)
  date.setDate(date.getDate() + days)
  return toDateKey(date)
}

const longFormatter = new Intl.DateTimeFormat('id-ID', {
  weekday: 'long',
  day: 'numeric',
  month: 'long',
  year: 'numeric',
})

export function formatDateLong(key: DateKey): string {
  return longFormatter.format(parseDateKey(key))
}
