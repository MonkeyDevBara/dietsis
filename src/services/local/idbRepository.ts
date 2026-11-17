// IndexedDB-backed Repository with the sync outbox (docs/OFFLINE_SYNC.md).
//
// Every user change is ONE IndexedDB transaction that writes the entity and updates the outbox, so
// there is never a local change without its op, nor an op without its change. The in-memory
// snapshot is updated only after that transaction commits.
//
// Each entity record keeps two states: `server` (last state the server told us) and `value`
// (what the UI shows = server + our unsent ops). Rebasing recomputes `value` from `server` whenever
// the server state or the outbox changes.

import { uuid } from '../../lib/id'
import { getValue, refId, refOf, setValue } from './entities'
import type { EntityRef } from './entities'
import { deleteDb, openDb, reqP, STORES, txDone } from './idb'
import { planOutbox } from './outbox'
import type { OutboxItem } from './outbox'
import type { Repository } from './repository'
import { applyOp, EMPTY_STATE } from './state'
import type { LocalState, Op } from './state'

interface EntityRecord {
  id: string
  entity: EntityRef['entity']
  key: string
  server: unknown
  value: unknown
}

interface StoredOutbox {
  seq?: number
  op_id: string
  op: Op
  ref: EntityRef
  created_at: number
}

export interface Meta {
  cursor: number
  /** Server data epoch this database belongs to; null until the first pull. */
  epoch: number | null
  /** True once a full pull has completed at least once. */
  synced: boolean
}

export interface ServerChange {
  entity: EntityRef['entity']
  key: string
  state: unknown
}

const DEFAULT_META: Meta = { cursor: 0, epoch: null, synced: false }

export class IdbRepository implements Repository {
  private db: IDBDatabase
  private state: LocalState = EMPTY_STATE
  private servers = new Map<string, unknown>()
  private outbox: OutboxItem[] = []
  private inflight = new Set<number>()
  private meta: Meta = DEFAULT_META
  private readonly listeners = new Set<() => void>()
  private readonly localListeners = new Set<() => void>()
  private chain: Promise<unknown> = Promise.resolve()
  private channel: BroadcastChannel | null = null

  private constructor(
    db: IDBDatabase,
    private readonly clock: () => number,
  ) {
    this.db = db
  }

  static async open(userId: string, clock: () => number = Date.now): Promise<IdbRepository> {
    const repo = new IdbRepository(await openDb(userId), clock)
    await repo.load()
    if (typeof BroadcastChannel !== 'undefined') {
      // Another tab changed this database: reload before our next operation so we never act on stale memory.
      repo.channel = new BroadcastChannel(`dietsis-${userId}`)
      repo.channel.onmessage = () => {
        void repo.run(() => repo.load().then(() => repo.emit()))
      }
    }
    return repo
  }

  /** Deletes a user's local database (logout). */
  static deleteFor(userId: string): Promise<void> {
    return deleteDb(userId)
  }

  // ---- Repository ----

  getSnapshot = (): LocalState => this.state

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }

  apply(op: Op): Promise<void> {
    return this.run(async () => {
      const now = this.clock()
      const next = applyOp(this.state, op, now)
      if (next === this.state) return

      const ref = refOf(op)
      const id = refId(ref)
      const pending = this.outbox.filter((i) => !this.inflight.has(i.seq))
      const plan = planOutbox(pending, op, (r) => this.servers.get(refId(r)) != null)
      const value = getValue(next, ref)
      const server = this.servers.get(id) ?? null

      const tx = this.db.transaction([STORES.entities, STORES.outbox], 'readwrite')
      const done = txDone(tx)
      this.putEntity(tx, ref, server, value)
      const outStore = tx.objectStore(STORES.outbox)
      for (const seq of plan.remove) outStore.delete(seq)
      for (const u of plan.update) {
        const cur = this.outbox.find((i) => i.seq === u.seq)
        if (cur) outStore.put({ seq: cur.seq, op_id: cur.op_id, op: u.op, ref: cur.ref, created_at: cur.created_at } satisfies StoredOutbox)
      }
      const opId = uuid()
      let addReq: IDBRequest<IDBValidKey> | null = null
      if (plan.add) addReq = outStore.add({ op_id: opId, op: plan.add, ref: refOf(plan.add), created_at: now } satisfies StoredOutbox)
      await done

      // Committed: mirror it in memory.
      let items = this.outbox.filter((i) => !plan.remove.includes(i.seq))
      items = items.map((i) => {
        const u = plan.update.find((x) => x.seq === i.seq)
        return u ? { ...i, op: u.op } : i
      })
      if (plan.add && addReq) items.push({ seq: Number(addReq.result), op_id: opId, op: plan.add, ref: refOf(plan.add), created_at: now })
      this.outbox = items
      this.state = next
      this.afterCommit(true)
    })
  }

  /** Wipes everything local (preview mode, or after the server data was deleted). Keeps epoch meta if given. */
  clear(): Promise<void> {
    return this.reset(this.meta.epoch)
  }

  // ---- sync-facing ----

  pending(): OutboxItem[] {
    return this.outbox.filter((i) => !this.inflight.has(i.seq))
  }

  pendingCount(): number {
    return this.outbox.length
  }

  markInflight(seqs: number[]): void {
    for (const s of seqs) this.inflight.add(s)
  }

  clearInflight(): void {
    this.inflight.clear()
  }

  serverState(ref: EntityRef): unknown {
    return this.servers.get(refId(ref)) ?? null
  }

  getMeta(): Meta {
    return this.meta
  }

  /** Called after a local user change (not after server changes): the sync engine schedules a push. */
  onLocalChange(cb: () => void): () => void {
    this.localListeners.add(cb)
    return () => this.localListeners.delete(cb)
  }

  /**
   * Atomically applies server knowledge: replaces `server` for each changed entity, rebases `value`
   * over the remaining unsent ops, removes acknowledged outbox items and updates meta.
   */
  applyServer(changes: ServerChange[], removeSeqs: number[], meta?: Partial<Meta>): Promise<void> {
    return this.run(async () => {
      const remaining = this.outbox.filter((i) => !removeSeqs.includes(i.seq))
      let next = this.state
      const records: { ref: EntityRef; server: unknown; value: unknown }[] = []
      const servers = new Map(this.servers)

      for (const c of changes) {
        const ref: EntityRef = { entity: c.entity, key: c.key }
        const id = refId(ref)
        const server = c.state ?? null
        servers.set(id, server)
        const value = rebase(ref, server, remaining)
        next = setValue(next, ref, value)
        records.push({ ref, server, value })
      }
      const newMeta = { ...this.meta, ...meta }

      const tx = this.db.transaction([STORES.entities, STORES.outbox, STORES.meta], 'readwrite')
      const done = txDone(tx)
      for (const r of records) this.putEntity(tx, r.ref, r.server, r.value)
      const outStore = tx.objectStore(STORES.outbox)
      for (const seq of removeSeqs) outStore.delete(seq)
      tx.objectStore(STORES.meta).put({ k: 'main', ...newMeta })
      await done

      this.outbox = remaining
      for (const r of records) if (r.server === null) servers.delete(refId(r.ref))
      this.servers = servers
      this.state = next
      this.meta = newMeta
      this.afterCommit(false)
    })
  }

  setMeta(meta: Partial<Meta>): Promise<void> {
    return this.run(async () => {
      this.meta = { ...this.meta, ...meta }
      const tx = this.db.transaction(STORES.meta, 'readwrite')
      const done = txDone(tx)
      tx.objectStore(STORES.meta).put({ k: 'main', ...this.meta })
      await done
    })
  }

  /** Drops all local data and the outbox, and starts fresh at the given server epoch. */
  reset(epoch: number | null): Promise<void> {
    return this.run(async () => {
      const tx = this.db.transaction([STORES.entities, STORES.outbox, STORES.meta], 'readwrite')
      const done = txDone(tx)
      tx.objectStore(STORES.entities).clear()
      tx.objectStore(STORES.outbox).clear()
      const meta: Meta = { cursor: 0, epoch, synced: false }
      tx.objectStore(STORES.meta).clear()
      tx.objectStore(STORES.meta).put({ k: 'main', ...meta })
      await done
      this.state = EMPTY_STATE
      this.servers = new Map()
      this.outbox = []
      this.inflight.clear()
      this.meta = meta
      this.afterCommit(false)
    })
  }

  close(): void {
    this.channel?.close()
    this.db.close()
  }

  // ---- internals ----

  private run<T>(fn: () => Promise<T>): Promise<T> {
    const p = this.chain.then(fn, fn)
    this.chain = p.catch(() => undefined)
    return p
  }

  private putEntity(tx: IDBTransaction, ref: EntityRef, server: unknown, value: unknown): void {
    const store = tx.objectStore(STORES.entities)
    const id = refId(ref)
    if (server == null && value == null) store.delete(id)
    else store.put({ id, entity: ref.entity, key: ref.key, server: server ?? null, value: value ?? null } satisfies EntityRecord)
  }

  private afterCommit(local: boolean): void {
    this.channel?.postMessage('changed')
    this.emit()
    if (local) for (const l of this.localListeners) l()
  }

  private emit(): void {
    for (const l of this.listeners) l()
  }

  private async load(): Promise<void> {
    const tx = this.db.transaction([STORES.entities, STORES.outbox, STORES.meta], 'readonly')
    const [entities, outbox, meta] = await Promise.all([
      reqP(tx.objectStore(STORES.entities).getAll() as IDBRequest<EntityRecord[]>),
      reqP(tx.objectStore(STORES.outbox).getAll() as IDBRequest<Required<StoredOutbox>[]>),
      reqP(tx.objectStore(STORES.meta).get('main') as IDBRequest<(Meta & { k: string }) | undefined>),
    ])
    let state = EMPTY_STATE
    const servers = new Map<string, unknown>()
    for (const r of entities) {
      const ref: EntityRef = { entity: r.entity, key: r.key }
      if (r.value != null) state = setValue(state, ref, r.value)
      if (r.server != null) servers.set(r.id, r.server)
    }
    this.state = state
    this.servers = servers
    this.outbox = outbox.sort((a, b) => a.seq - b.seq).map((o) => ({ seq: o.seq, op_id: o.op_id, op: o.op, ref: o.ref, created_at: o.created_at }))
    this.meta = meta ? { cursor: meta.cursor, epoch: meta.epoch, synced: meta.synced } : DEFAULT_META
    this.inflight.clear()
  }
}

/** Recomputes an entity's displayed value: the server state with every unsent op for it applied in order. */
function rebase(ref: EntityRef, server: unknown, ops: readonly OutboxItem[]): unknown {
  const id = refId(ref)
  let st = setValue(EMPTY_STATE, ref, server)
  for (const item of ops) if (refId(item.ref) === id) st = applyOp(st, item.op, item.created_at)
  return getValue(st, ref)
}
