import { describe, expect, it } from 'vitest'
import { csvCell, toCsv, weightsCsv } from './csv'

describe('csv', () => {
  it('neutralises spreadsheet formulas but leaves numbers alone', () => {
    expect(csvCell('=HYPERLINK("x")')).toBe(`"'=HYPERLINK(""x"")"`)
    expect(csvCell('+1')).toBe("'+1")
    expect(csvCell('@SUM(A1)')).toBe("'@SUM(A1)")
    expect(csvCell(-1.5)).toBe('-1.5')
  })

  it('quotes commas, quotes and newlines; null is empty', () => {
    expect(csvCell('a,b')).toBe('"a,b"')
    expect(csvCell('say "hi"')).toBe('"say ""hi"""')
    expect(csvCell('l1\nl2')).toBe('"l1\nl2"')
    expect(csvCell(null)).toBe('')
  })

  it('writes BOM, CRLF and a header row', () => {
    expect(toCsv(['a', 'b'], [[1, 'x']])).toBe('\uFEFFa,b\r\n1,x\r\n')
  })

  it('sorts weights by date', () => {
    const out = weightsCsv([
      { date: '2026-11-17', weight_kg: 72, waist_cm: null, version: 1, updated_at: 0 },
      { date: '2026-11-10', weight_kg: 73, waist_cm: 80, version: 1, updated_at: 0 },
    ])
    expect(out.split('\r\n').slice(1, 3)).toEqual(['2026-11-10,73,80', '2026-11-17,72,'])
  })
})
