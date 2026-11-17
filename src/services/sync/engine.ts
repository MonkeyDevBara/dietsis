// Sync engine (docs/OFFLINE_SYNC.md): push the outbox, then pull the change feed.
//
//  - Ops carry a client-generated op_id, so a retry after a lost response is answered from the
//    server's ledger and never applied twice.
//  - Failures keep the ops queued and retry with exponential backoff (+ jitter, Retry-After honored).
//  - Conflicts are explicit: the server keeps its state, we adopt it and tell the user.
//  - A changed data epoch (account data deleted elsewhere) resets local data instead of pushing.

import { api, ApiError } from '../api'
import type { ServerChange } from '../local/idbRepository'
import type { IdbRepository } from '../local/idbRepository'
import { nextBatch, toWire } from '../local/outbox'
import type { OutboxItem, WireOp } from '../local/outbox'

export type SyncPhase = 'idle' | 'syncing' | 'offline' | 'error'

export interface SyncSnapshot {
  phase: SyncPhase
  /** Unsent operations (including any currently in flight). */
  pending: number
  lastSyncAt: number | null
  retryAt: number | null
  /** A full pull has completed at least once for this database. */
  synced: boolean
  /** The session is gone: syncing stopped until the user logs in again. */
  reauth: boolean
}

export interface SyncHooks {
  onConflicts?: (count: number) => void
  onRejected?: (count: number) => void
  onUnauthorized?: () => void
  onReset?: () => void
}

export interface SyncDeps {
  call?: typeof api
  now?: () => number
  random?: () => number
  /** Web Locks name so only one tab syncs a user's database at a time. */
  lockName?: string
}

interface PushResult {
  op_id: string
  status: 'applied' | 'conflict' | 'rejected'
  state?: unknown
}
interface PushData {
  data_epoch: number
  results: PushResult[]
}
interface PullData {
  data_epoch: number
  cursor: number
  has_more: boolean
  changes: { seq: number; entity: ServerChange['entity']; key: string; state: unknown }[]
}

const MAX_BATCH = 200
const PULL_LIMIT = 500
const DEBOUNCE_MS = 1000
const POLL_MS = 60_000
const MAX_BACKOFF_MS = 5 * 60_000

class Restart extends Error {}

export class SyncEngine {
  private snap: SyncSnapshot
  private readonly listeners = new Set<() => void>()
  private readonly call: typeof api
  private readonly now: () => number
  private readonly random: () => number
  private running: Promise<void> | null = null
  private again = false
  private attempts = 0
  private batchMax = MAX_BATCH
  private timer: ReturnType<typeof setTimeout> | undefined
  private poll: ReturnType<typeof setInterval> | undefined
  private stopped = false
  private cleanups: (() => void)[] = []

  constructor(
    private readonly repo: IdbRepository,
    private readonly hooks: SyncHooks = {},
    deps: SyncDeps = {},
  ) {
    this.call = deps.call ?? api
    this.now = deps.now ?? Date.now
    this.random = deps.random ?? Math.random
    this.lockName = deps.lockName
    this.snap = { phase: 'idle', pending: repo.pendingCount(), lastSyncAt: null, retryAt: null, synced: repo.getMeta().synced, reauth: false }
    this.cleanups.push(repo.subscribe(() => this.update({ pending: repo.pendingCount(), synced: repo.getMeta().synced })))
  }

  private readonly lockName: string | undefined

  getSnapshot = (): SyncSnapshot => this.snap

  subscribe = (l: () => void): (() => void) => {
    this.listeners.add(l)
    return () => this.listeners.delete(l)
  }

  /** Starts background syncing: now, after local changes, on reconnect, on tab focus, and every minute. */
  start(): void {
    this.cleanups.push(this.repo.onLocalChange(() => this.kick()))
    const onOnline = () => {
      this.attempts = 0
      void this.syncNow()
    }
    const onVisible = () => {
      if (document.visibilityState === 'visible') this.kick()
    }
    window.addEventListener('online', onOnline)
    document.addEventListener('visibilitychange', onVisible)
    this.cleanups.push(() => {
      window.removeEventListener('online', onOnline)
      document.removeEventListener('visibilitychange', onVisible)
    })
    this.poll = setInterval(() => {
      if (document.visibilityState === 'visible') this.kick()
    }, POLL_MS)
    void this.syncNow()
  }

  stop(): void {
    this.stopped = true
    clearTimeout(this.timer)
    clearInterval(this.poll)
    for (const c of this.cleanups) c()
    this.cleanups = []
  }

  /** Schedules a sync soon (debounced). Ignored while backing off from an error. */
  kick(): void {
    if (this.stopped || this.snap.reauth) return
    if (this.snap.retryAt !== null && this.now() < this.snap.retryAt) return
    clearTimeout(this.timer)
    this.timer = setTimeout(() => void this.syncNow(), DEBOUNCE_MS)
  }

  /** Runs a full cycle now (manual button, reconnect, logout). Resolves when it finishes. */
  syncNow(): Promise<void> {
    if (this.stopped || this.snap.reauth) return Promise.resolve()
    clearTimeout(this.timer)
    if (this.running) {
      this.again = true
      return this.running
    }
    this.running = this.cycle().finally(() => {
      this.running = null
      if (this.again && !this.stopped) {
        this.again = false
        void this.syncNow()
      }
    })
    return this.running
  }

  private update(patch: Partial<SyncSnapshot>): void {
    const next = { ...this.snap, ...patch }
    if (Object.keys(patch).every((k) => this.snap[k as keyof SyncSnapshot] === next[k as keyof SyncSnapshot])) return
    this.snap = next
    for (const l of this.listeners) l()
  }

  private async cycle(): Promise<void> {
    const locks = (navigator as { locks?: LockManager }).locks
    const body = async () => {
      this.update({ phase: 'syncing' })
      try {
        for (let restarts = 0; ; restarts++) {
          try {
            if (this.repo.getMeta().epoch === null) await this.pull()
            await this.push()
            await this.pull()
            break
          } catch (e) {
            if (e instanceof Restart && restarts < 2) continue
            throw e
          }
        }
        this.attempts = 0
        this.update({ phase: 'idle', lastSyncAt: this.now(), retryAt: null })
      } catch (e) {
        this.fail(e)
      }
    }
    if (locks && this.lockName) {
      await locks.request(this.lockName, { ifAvailable: true }, (lock) => (lock ? body() : undefined))
    } else {
      await body()
    }
  }

  private fail(e: unknown): void {
    if (e instanceof ApiError && e.status === 401) {
      this.update({ phase: 'idle', reauth: true, retryAt: null })
      this.hooks.onUnauthorized?.()
      return
    }
    // A permanent-looking error (e.g. 400) is retried too, slowly: the data stays safe in the outbox.
    this.attempts++
    const base = e instanceof ApiError && e.retryAfterMs ? e.retryAfterMs : Math.min(MAX_BACKOFF_MS, 1000 * 2 ** this.attempts)
    const delay = Math.min(MAX_BACKOFF_MS, Math.round(base * (0.8 + 0.4 * this.random())))
    const retryAt = this.now() + delay
    const offline = !navigator.onLine || (e instanceof ApiError && e.status === 0)
    this.update({ phase: offline ? 'offline' : 'error', retryAt })
    clearTimeout(this.timer)
    this.timer = setTimeout(() => void this.syncNow(), delay)
  }

  // ---- push ----

  private async push(): Promise<void> {
    for (;;) {
      const batch = nextBatch(this.repo.pending(), this.batchMax)
      if (batch.length === 0) return

      const wires: WireOp[] = []
      const sent: OutboxItem[] = []
      const dropped: OutboxItem[] = []
      for (const item of batch) {
        const w = toWire(item, this.repo.serverState(item.ref))
        if (w) {
          wires.push(w)
          sent.push(item)
        } else dropped.push(item)
      }
      if (dropped.length > 0) {
        await this.repo.applyServer(
          dropped.map((i) => ({ entity: i.ref.entity, key: i.ref.key, state: this.repo.serverState(i.ref) })),
          dropped.map((i) => i.seq),
        )
      }
      if (sent.length === 0) continue

      // In-flight items must not be rewritten by coalescing; they are released only after the ack is
      // stored, so a tap made during the request is queued as its own op instead of being merged
      // into an op that is about to be removed.
      this.repo.markInflight(sent.map((i) => i.seq))
      try {
        const data = await this.call<PushData>('POST', '/sync/push', { data_epoch: this.repo.getMeta().epoch, ops: wires }, { timeoutMs: 30_000 })
        await this.acknowledge(sent, data)
      } catch (e) {
        if (e instanceof ApiError && e.status === 413 && sent.length > 1) {
          this.batchMax = Math.max(1, Math.floor(sent.length / 2))
          continue
        }
        if (e instanceof ApiError && e.status === 409 && e.code === 'epoch_mismatch') {
          await this.repo.reset(Number(e.details?.data_epoch ?? Number.NaN) || null)
          this.hooks.onReset?.()
          throw new Restart()
        }
        throw e
      } finally {
        this.repo.clearInflight()
      }
    }
  }

  private async acknowledge(sent: OutboxItem[], data: PushData): Promise<void> {
    const byId = new Map(data.results.map((r) => [r.op_id, r]))
    const changes: ServerChange[] = []
    const acked: number[] = []
    let conflicts = 0
    let rejected = 0
    for (const item of sent) {
      const r = byId.get(item.op_id)
      if (!r) continue // no answer: keep it queued, it is retried next cycle
      acked.push(item.seq)
      if (r.status === 'rejected') {
        rejected++
        changes.push({ entity: item.ref.entity, key: item.ref.key, state: this.repo.serverState(item.ref) })
      } else {
        if (r.status === 'conflict') conflicts++
        changes.push({ entity: item.ref.entity, key: item.ref.key, state: r.state ?? null })
      }
    }
    await this.repo.applyServer(changes, acked)
    if (conflicts > 0) this.hooks.onConflicts?.(conflicts)
    if (rejected > 0) this.hooks.onRejected?.(rejected)
  }

  // ---- pull ----

  private async pull(): Promise<void> {
    for (;;) {
      const meta = this.repo.getMeta()
      const res = await this.call<PullData>('GET', `/sync/pull?since=${meta.cursor}&limit=${PULL_LIMIT}`)
      if (meta.epoch !== null && res.data_epoch !== meta.epoch) {
        // The account's data was deleted from another device: drop everything and start over.
        await this.repo.reset(res.data_epoch)
        this.hooks.onReset?.()
        continue
      }
      await this.repo.applyServer(
        res.changes.map((c) => ({ entity: c.entity, key: c.key, state: c.state })),
        [],
        { cursor: res.cursor, epoch: res.data_epoch },
      )
      if (!res.has_more) {
        if (!this.repo.getMeta().synced) await this.repo.setMeta({ synced: true })
        return
      }
    }
  }
}
