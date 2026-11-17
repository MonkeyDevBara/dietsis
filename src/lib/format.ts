import { addDays, parseDateKey, todayKey } from './date'

const int = new Intl.NumberFormat('id-ID', { maximumFractionDigits: 0 })
const dec1 = new Intl.NumberFormat('id-ID', { maximumFractionDigits: 1 })
const dec2 = new Intl.NumberFormat('id-ID', { maximumFractionDigits: 2 })

export const fmt = {
  int: (n: number) => int.format(n),
  dec1: (n: number) => dec1.format(n),
  qty: (n: number) => dec2.format(n),
  kcal: (n: number) => `${int.format(n)} kkal`,
  gram: (n: number) => `${dec1.format(n)} g`,
  kg: (n: number) => `${dec1.format(n)} kg`,
  /** Signed change, e.g. "−1,6 kg" / "+0,4 kg". Uses a true minus sign. */
  kgDelta: (n: number) => `${n > 0 ? '+' : n < 0 ? '−' : ''}${dec1.format(Math.abs(n))} kg`,
}

const shortDate = new Intl.DateTimeFormat('id-ID', { day: 'numeric', month: 'short' })
const weekdayDate = new Intl.DateTimeFormat('id-ID', { weekday: 'long', day: 'numeric', month: 'long' })
const weekdayShort = new Intl.DateTimeFormat('id-ID', { weekday: 'short' })

export function formatShortDate(key: string): string {
  return shortDate.format(parseDateKey(key))
}

export function formatWeekdayShort(key: string): string {
  return weekdayShort.format(parseDateKey(key))
}

/** "Hari ini" / "Kemarin" / "Besok" / "Senin, 17 November". */
export function formatRelativeDay(key: string, now: Date = new Date()): string {
  const today = todayKey(now)
  if (key === today) return 'Hari ini'
  if (key === addDays(today, -1)) return 'Kemarin'
  if (key === addDays(today, 1)) return 'Besok'
  return weekdayDate.format(parseDateKey(key))
}
