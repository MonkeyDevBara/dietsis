// CSV building shared by the M1 preview export (from local data). The server export (M4,
// docs/DECISIONS.md D-019) follows the same rules: UTF-8 BOM, comma separator, CRLF, injection guard.

import type { FoodLog, Weight } from '../types'

/** Cells starting with = + - @ TAB CR are prefixed with ' so spreadsheets never evaluate them. */
export function csvCell(value: string | number | null): string {
  if (value === null) return ''
  let s = String(value)
  if (typeof value === 'string' && /^[=+\-@\t\r]/.test(s)) s = `'${s}`
  return /[",\r\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
}

export function toCsv(header: readonly string[], rows: readonly (readonly (string | number | null)[])[]): string {
  const lines = [header, ...rows].map((r) => r.map(csvCell).join(','))
  return `\uFEFF${lines.join('\r\n')}\r\n`
}

export function foodLogsCsv(logs: readonly FoodLog[]): string {
  const sorted = [...logs].filter((l) => l.deleted_at === null).sort((a, b) => a.date.localeCompare(b.date) || a.created_at - b.created_at)
  return toCsv(
    ['tanggal', 'waktu_makan', 'makanan', 'porsi', 'jumlah_porsi', 'kkal_per_porsi', 'protein_g_per_porsi', 'karbo_g_per_porsi', 'lemak_g_per_porsi', 'total_kkal', 'total_protein_g', 'total_karbo_g', 'total_lemak_g'],
    sorted.map((l) => [l.date, l.meal, l.name, l.serving_label, l.quantity, l.kcal, l.protein_g, l.carbs_g, l.fat_g, l.totals.kcal, l.totals.protein_g, l.totals.carbs_g, l.totals.fat_g]),
  )
}

export function weightsCsv(weights: readonly Weight[]): string {
  const sorted = [...weights].sort((a, b) => a.date.localeCompare(b.date))
  return toCsv(['tanggal', 'berat_kg', 'pinggang_cm'], sorted.map((w) => [w.date, w.weight_kg, w.waist_cm]))
}

export function downloadText(filename: string, text: string, mime = 'text/csv;charset=utf-8'): void {
  const url = URL.createObjectURL(new Blob([text], { type: mime }))
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.append(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
