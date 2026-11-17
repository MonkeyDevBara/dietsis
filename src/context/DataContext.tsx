import { createContext, useContext, useMemo, useSyncExternalStore } from 'react'
import type { ReactNode } from 'react'
import { createActions } from '../services/actions'
import type { Actions } from '../services/actions'
import type { Repository } from '../services/local/repository'
import type { LocalState } from '../services/local/state'

interface DataContextValue {
  repo: Repository
  actions: Actions
}

const DataContext = createContext<DataContextValue | null>(null)

export function DataProvider({ repo, children }: { repo: Repository; children: ReactNode }) {
  const value = useMemo(() => ({ repo, actions: createActions(repo) }), [repo])
  return <DataContext.Provider value={value}>{children}</DataContext.Provider>
}

function useData(): DataContextValue {
  const ctx = useContext(DataContext)
  if (!ctx) throw new Error('DataProvider belum dipasang')
  return ctx
}

/** Current local snapshot; re-renders on every change. Derive views with useMemo + selectors. */
export function useLocalState(): LocalState {
  const { repo } = useData()
  return useSyncExternalStore(repo.subscribe, repo.getSnapshot)
}

export function useActions(): Actions {
  return useData().actions
}
