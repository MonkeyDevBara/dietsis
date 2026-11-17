import { useCallback } from 'react'
import { useSearchParams } from 'react-router'
import { isDateKey, todayKey } from '../lib/date'

/** Active day lives in ?date=YYYY-MM-DD so refresh and back/forward keep it. Default: today. */
export function useSelectedDate(): [string, (date: string) => void] {
  const [params, setParams] = useSearchParams()
  const raw = params.get('date')
  const today = todayKey()
  const date = raw && isDateKey(raw) && raw <= today ? raw : today

  const setDate = useCallback(
    (next: string) => {
      setParams(
        (prev) => {
          const p = new URLSearchParams(prev)
          if (next === todayKey()) p.delete('date')
          else p.set('date', next)
          return p
        },
        { replace: true },
      )
    },
    [setParams],
  )

  return [date, setDate]
}
