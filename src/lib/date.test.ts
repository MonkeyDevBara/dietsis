import { describe, expect, it } from 'vitest'
import { addDays, isDateKey, parseDateKey, toDateKey, todayKey } from './date'

describe('date keys', () => {
  it('formats local calendar date with zero padding', () => {
    expect(toDateKey(new Date(2026, 0, 5, 23, 59))).toBe('2026-01-05')
  })

  it('uses local midnight boundaries, not UTC', () => {
    // 00:30 local time is still the same local day regardless of the UTC offset.
    expect(todayKey(new Date(2026, 10, 17, 0, 30))).toBe('2026-11-17')
  })

  it('rejects impossible or malformed dates', () => {
    expect(isDateKey('2026-02-29')).toBe(false)
    expect(isDateKey('2028-02-29')).toBe(true)
    expect(isDateKey('2026-13-01')).toBe(false)
    expect(isDateKey('2026-1-01')).toBe(false)
    expect(isDateKey('2026-01-01T00:00')).toBe(false)
  })

  it('round-trips parse and format', () => {
    expect(toDateKey(parseDateKey('2026-03-31'))).toBe('2026-03-31')
    expect(() => parseDateKey('2026-04-31')).toThrow(RangeError)
  })

  it('adds days across month and year boundaries', () => {
    expect(addDays('2026-12-31', 1)).toBe('2027-01-01')
    expect(addDays('2026-03-01', -1)).toBe('2026-02-28')
    expect(addDays('2028-03-01', -1)).toBe('2028-02-29')
  })
})
