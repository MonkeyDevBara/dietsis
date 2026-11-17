import { applyOp, EMPTY_STATE } from './state'
import type { LocalState, Op } from './state'

/**
 * Local data source for the UI. Reads are synchronous snapshots (immutable; a new object after every
 * change) so React can subscribe with useSyncExternalStore. Writes go through `apply(op)`.
 *
 * M1 ships MemoryRepository. M6 adds an IndexedDB-backed implementation that keeps the same in-memory
 * snapshot (personal data is small), persists each op atomically and queues it for sync.
 */
export interface Repository {
  getSnapshot: () => LocalState
  subscribe: (listener: () => void) => () => void
  apply: (op: Op) => Promise<void>
  /** Removes all local data (logout, "hapus semua data"). */
  clear: () => Promise<void>
}

export class MemoryRepository implements Repository {
  private state: LocalState
  private readonly listeners = new Set<() => void>()
  private readonly clock: () => number

  constructor(initial: LocalState = EMPTY_STATE, clock: () => number = Date.now) {
    this.state = initial
    this.clock = clock
  }

  getSnapshot = (): LocalState => this.state

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }

  apply(op: Op): Promise<void> {
    const next = applyOp(this.state, op, this.clock())
    if (next !== this.state) {
      this.state = next
      this.emit()
    }
    return Promise.resolve()
  }

  clear(): Promise<void> {
    this.state = EMPTY_STATE
    this.emit()
    return Promise.resolve()
  }

  private emit() {
    for (const l of this.listeners) l()
  }
}
