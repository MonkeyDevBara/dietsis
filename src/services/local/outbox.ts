// Outbox: the queue of ops waiting to be sent. Pure functions only, so the rules are unit-testable.
//
// Coalescing (docs/OFFLINE_SYNC.md) keeps the queue short and prevents conflicts with ourselves:
// repeated edits merge into one unsent op, and a create that is deleted before it was ever sent
// disappears entirely. Ops already in flight are never touched.

import { refId, refOf, versionOf } from './entities'
import type { EntityRef } from './entities'
import type { Op } from './state'

export interface OutboxItem {
  seq: number
  op_id: string
  op: Op
  ref: EntityRef
  created_at: number
}

export interface OutboxPlan {
  remove: number[]
  update: { seq: number; op: Op }[]
  add: Op | null
}

const sameRef = (a: EntityRef, b: EntityRef) => refId(a) === refId(b)

/**
 * Decides how a new op changes the queue. `pending` must exclude in-flight items.
 * `serverKnows(ref)` tells whether the entity exists on the server (last known state).
 */
export function planOutbox(pending: readonly OutboxItem[], op: Op, serverKnows: (r: EntityRef) => boolean): OutboxPlan {
  const ref = refOf(op)
  const mine = pending.filter((i) => sameRef(i.ref, ref))
  const last = (type: Op['type']) => [...mine].reverse().find((i) => i.op.type === type)
  const plain: OutboxPlan = { remove: [], update: [], add: op }

  switch (op.type) {
    case 'food_log.update': {
      const create = last('food_log.create')
      if (create && create.op.type === 'food_log.create') {
        return { remove: [], update: [{ seq: create.seq, op: { type: 'food_log.create', payload: { ...create.op.payload, ...definedOnly(op.payload.patch) } } }], add: null }
      }
      const upd = last('food_log.update')
      if (upd && upd.op.type === 'food_log.update') {
        return { remove: [], update: [{ seq: upd.seq, op: { type: 'food_log.update', payload: { id: op.payload.id, patch: { ...upd.op.payload.patch, ...op.payload.patch } } } }], add: null }
      }
      return plain
    }

    case 'food_log.delete':
      // Never sent: the server never needs to know this entry existed.
      if (last('food_log.create')) return { remove: mine.map((i) => i.seq), update: [], add: null }
      return plain

    case 'food_log.restore': {
      const del = last('food_log.delete')
      return del ? { remove: [del.seq], update: [], add: null } : plain
    }

    case 'day.water': {
      const prev = last('day.water')
      if (prev && prev.op.type === 'day.water') {
        const delta = prev.op.payload.delta + op.payload.delta
        return delta === 0
          ? { remove: [prev.seq], update: [], add: null }
          : { remove: [], update: [{ seq: prev.seq, op: { type: 'day.water', payload: { date: op.payload.date, delta } } }], add: null }
      }
      return plain
    }

    case 'day.update': {
      const prev = last('day.update')
      if (prev && prev.op.type === 'day.update') {
        return { remove: [], update: [{ seq: prev.seq, op: { type: 'day.update', payload: { date: op.payload.date, patch: { ...prev.op.payload.patch, ...op.payload.patch } } } }], add: null }
      }
      return plain
    }

    case 'weight.put': {
      const prev = last('weight.put')
      return prev ? { remove: [], update: [{ seq: prev.seq, op }], add: null } : plain
    }

    case 'weight.delete': {
      const put = last('weight.put')
      if (put) return { remove: [put.seq], update: [], add: serverKnows(ref) ? op : null }
      return plain
    }

    case 'profile.put': {
      const prev = last('profile.put')
      return prev ? { remove: [], update: [{ seq: prev.seq, op }], add: null } : plain
    }

    case 'favorite.set': {
      const prev = last('favorite.set')
      return prev ? { remove: [], update: [{ seq: prev.seq, op }], add: null } : plain
    }

    case 'food_log.create':
    case 'custom_food.create':
      return plain
  }
}

function definedOnly<T extends object>(o: T): Partial<T> {
  return Object.fromEntries(Object.entries(o).filter(([, v]) => v !== undefined)) as Partial<T>
}

export interface WireOp {
  op_id: string
  type: string
  payload: Record<string, unknown>
}

/**
 * Builds the op sent to the server. base_version is computed now, from the latest server state we
 * know, so an op queued behind an in-flight one automatically uses the version that one produced.
 * Returns null for a weight.delete of an entry the server never had (nothing to delete).
 */
export function toWire(item: OutboxItem, server: unknown): WireOp | null {
  const v = versionOf(server)
  const base = (extra: Record<string, unknown> = {}) => ({ ...(item.op.payload as unknown as Record<string, unknown>), ...extra })
  const wire = (payload: Record<string, unknown>): WireOp => ({ op_id: item.op_id, type: item.op.type, payload })

  switch (item.op.type) {
    case 'food_log.update':
      return wire(base(v === undefined ? {} : { base_version: v }))
    case 'weight.put':
    case 'day.update':
    case 'profile.put':
      return wire(base({ base_version: v ?? 0 }))
    case 'weight.delete':
      return v === undefined ? null : wire(base({ base_version: v }))
    default:
      return wire(base())
  }
}

/**
 * Picks the next batch: FIFO, stopping before an op whose entity is already in the batch, because
 * the second op's base_version depends on the first one's result.
 */
export function nextBatch(pending: readonly OutboxItem[], max: number): OutboxItem[] {
  const out: OutboxItem[] = []
  const seen = new Set<string>()
  for (const item of pending) {
    const id = refId(item.ref)
    if (seen.has(id) || out.length >= max) break
    seen.add(id)
    out.push(item)
  }
  return out
}
