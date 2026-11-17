import 'fake-indexeddb/auto'
import { beforeEach, describe, expect, it } from 'vitest'
import type { FoodLogInput } from '../../types'
import { refOf } from './entities'
import { IdbRepository } from './idbRepository'
import { nextBatch, planOutbox, toWire } from './outbox'
import type { OutboxItem } from './outbox'
import type { Op } from './state'

const T = 1_800_000_000_000
const ID_A = '11111111-1111-4111-8111-111111111111'
let n = 0
const freshUser = () => `u${++n}-${Math.random().toString(36).slice(2)}`

const log = (over: Partial<FoodLogInput> = {}): Op => ({
  type: 'food_log.create',
  payload: { id: ID_A, date: '2026-11-17', meal: 'lunch', food_id: 'b:nasi-putih', name: 'Nasi', serving_label: '1', serving_grams: null, quantity: 1, kcal: 200, protein_g: 4, carbs_g: 40, fat_g: 1, ...over },
})
const water = (delta: number, date = '2026-11-17'): Op => ({ type: 'day.water', payload: { date, delta } })

let user: string
beforeEach(() => {
  user = freshUser()
})

const item = (seq: number, op: Op): OutboxItem => ({ seq, op_id: `op${seq}`, op, ref: refOf(op), created_at: T })

describe('outbox planning', () => {
  it('merges repeated water taps and drops the op when they cancel out', () => {
    const q = [item(1, water(1))]
    const plan = planOutbox(q, water(2), () => false)
    expect(plan.add).toBeNull()
    expect(plan.update[0]?.op).toEqual(water(3))
    expect(planOutbox(q, water(-1), () => false)).toEqual({ remove: [1], update: [], add: null })
  })

  it('folds an edit into an unsent create', () => {
    const plan = planOutbox([item(1, log())], { type: 'food_log.update', payload: { id: ID_A, patch: { quantity: 2, serving_grams: undefined } } }, () => false)
    expect(plan.add).toBeNull()
    expect(plan.update[0]?.op).toMatchObject({ type: 'food_log.create', payload: { quantity: 2, name: 'Nasi' } })
  })

  it('merges consecutive patches of a sent entity into one update', () => {
    const q = [item(1, { type: 'food_log.update', payload: { id: ID_A, patch: { quantity: 2 } } })]
    const plan = planOutbox(q, { type: 'food_log.update', payload: { id: ID_A, patch: { meal: 'dinner' } } }, () => true)
    expect(plan.update[0]?.op).toEqual({ type: 'food_log.update', payload: { id: ID_A, patch: { quantity: 2, meal: 'dinner' } } })
  })

  it('deleting a never-sent create (and its edits) leaves nothing to send', () => {
    const q = [item(1, log()), item(2, { type: 'food_log.update', payload: { id: ID_A, patch: { quantity: 2 } } })]
    expect(planOutbox(q, { type: 'food_log.delete', payload: { id: ID_A } }, () => false)).toEqual({ remove: [1, 2], update: [], add: null })
  })

  it('restore cancels an unsent delete; delete of a sent entry is queued', () => {
    const del = item(3, { type: 'food_log.delete', payload: { id: ID_A } })
    expect(planOutbox([del], { type: 'food_log.restore', payload: { id: ID_A } }, () => true)).toEqual({ remove: [3], update: [], add: null })
    expect(planOutbox([], { type: 'food_log.delete', payload: { id: ID_A } }, () => true).add).not.toBeNull()
  })

  it('weight delete of an entry the server never saw sends nothing', () => {
    const put = item(1, { type: 'weight.put', payload: { date: '2026-11-17', weight_kg: 70, waist_cm: null } })
    const del: Op = { type: 'weight.delete', payload: { date: '2026-11-17' } }
    expect(planOutbox([put], del, () => false)).toEqual({ remove: [1], update: [], add: null })
    expect(planOutbox([put], del, () => true)).toEqual({ remove: [1], update: [], add: del })
  })

  it('computes base_version at send time from the latest server state', () => {
    const upd = item(1, { type: 'food_log.update', payload: { id: ID_A, patch: { quantity: 2 } } })
    expect(toWire(upd, { version: 4 })?.payload).toMatchObject({ id: ID_A, base_version: 4 })
    const put = item(2, { type: 'weight.put', payload: { date: '2026-11-17', weight_kg: 70, waist_cm: null } })
    expect(toWire(put, null)?.payload).toMatchObject({ base_version: 0 })
    expect(toWire(item(3, { type: 'weight.delete', payload: { date: '2026-11-17' } }), null)).toBeNull()
    expect(toWire(item(4, water(1)), null)?.payload).toEqual({ date: '2026-11-17', delta: 1 })
  })

  it('batches FIFO but stops before a second op on the same entity', () => {
    const q = [item(1, water(1, '2026-11-17')), item(2, water(1, '2026-11-16')), item(3, { type: 'day.update', payload: { date: '2026-11-17', patch: { steps: 1 } } }), item(4, water(1, '2026-11-15'))]
    expect(nextBatch(q, 200).map((i) => i.seq)).toEqual([1, 2])
    expect(nextBatch(q, 1).map((i) => i.seq)).toEqual([1])
  })
})

describe('IdbRepository', () => {
  it('persists entity and outbox together and survives a reopen', async () => {
    const repo = await IdbRepository.open(user, () => T)
    await repo.apply(log())
    await repo.apply(water(2))
    expect(repo.pendingCount()).toBe(2)
    repo.close()

    const again = await IdbRepository.open(user, () => T)
    expect(again.getSnapshot().foodLogs[ID_A]?.name).toBe('Nasi')
    expect(again.getSnapshot().days['2026-11-17']?.water_glasses).toBe(2)
    expect(again.pending().map((i) => i.op.type)).toEqual(['food_log.create', 'day.water'])
    again.close()
  })

  it('notifies subscribers and flags local changes for the sync engine', async () => {
    const repo = await IdbRepository.open(user, () => T)
    let seen = 0
    let local = 0
    repo.subscribe(() => seen++)
    repo.onLocalChange(() => local++)
    await repo.apply(water(1))
    expect([seen, local]).toEqual([1, 1])
    await repo.apply(water(0 + 1)) // coalesced into the same op, still a visible change
    expect(repo.pendingCount()).toBe(1)
    await repo.applyServer([], [])
    expect(local).toBe(2) // server changes are not local changes
    repo.close()
  })

  it('does nothing for no-op operations', async () => {
    const repo = await IdbRepository.open(user, () => T)
    await repo.apply({ type: 'food_log.delete', payload: { id: ID_A } })
    expect(repo.pendingCount()).toBe(0)
    repo.close()
  })

  it('rebases: server water plus unsent taps', async () => {
    const repo = await IdbRepository.open(user, () => T)
    await repo.apply(water(2)) // unsent, queued
    // Another device added 3 glasses; the server now says 3.
    await repo.applyServer([{ entity: 'day', key: '2026-11-17', state: { date: '2026-11-17', water_glasses: 3, steps: null, note: '', version: 0, updated_at: 1 } }], [])
    expect(repo.getSnapshot().days['2026-11-17']?.water_glasses).toBe(5)
    expect(repo.pendingCount()).toBe(1)
    repo.close()
  })

  it('acknowledged ops leave the outbox and the server state becomes the base', async () => {
    const repo = await IdbRepository.open(user, () => T)
    await repo.apply(log())
    const [it0] = repo.pending()
    const serverLog = { ...repo.getSnapshot().foodLogs[ID_A], version: 1 }
    await repo.applyServer([{ entity: 'food_log', key: ID_A, state: serverLog }], [it0?.seq ?? -1], { cursor: 7, epoch: 1 })
    expect(repo.pendingCount()).toBe(0)
    expect(repo.serverState({ entity: 'food_log', key: ID_A })).toMatchObject({ version: 1 })
    expect(repo.getMeta()).toMatchObject({ cursor: 7, epoch: 1 })
    repo.close()
    const again = await IdbRepository.open(user)
    expect(again.getMeta().cursor).toBe(7)
    expect(again.pendingCount()).toBe(0)
    expect(again.getSnapshot().foodLogs[ID_A]).toBeDefined()
    again.close()
  })

  it('a server conflict state replaces the optimistic value; null removes the entity', async () => {
    const repo = await IdbRepository.open(user, () => T)
    await repo.apply(log())
    const [it0] = repo.pending()
    await repo.applyServer([{ entity: 'food_log', key: ID_A, state: null }], [it0?.seq ?? -1])
    expect(repo.getSnapshot().foodLogs[ID_A]).toBeUndefined()
    repo.close()
    const again = await IdbRepository.open(user)
    expect(again.getSnapshot().foodLogs[ID_A]).toBeUndefined()
    again.close()
  })

  it('does not coalesce into in-flight ops', async () => {
    const repo = await IdbRepository.open(user, () => T)
    await repo.apply(water(1))
    repo.markInflight(repo.pending().map((i) => i.seq))
    await repo.apply(water(1))
    expect(repo.pendingCount()).toBe(2) // the in-flight tap and a new one: the sent tap must not be rewritten
    expect(repo.pending()).toHaveLength(1)
    repo.close()
  })

  it('reset wipes entities, outbox and cursor but keeps the new epoch', async () => {
    const repo = await IdbRepository.open(user, () => T)
    await repo.apply(log())
    await repo.setMeta({ cursor: 9, epoch: 1, synced: true })
    await repo.reset(2)
    expect(repo.getSnapshot().foodLogs).toEqual({})
    expect(repo.pendingCount()).toBe(0)
    expect(repo.getMeta()).toEqual({ cursor: 0, epoch: 2, synced: false })
    repo.close()
    const again = await IdbRepository.open(user)
    expect(again.getMeta().epoch).toBe(2)
    again.close()
  })

  it('keeps users in separate databases', async () => {
    const other = freshUser()
    const a = await IdbRepository.open(user, () => T)
    const b = await IdbRepository.open(other, () => T)
    await a.apply(log())
    expect(b.getSnapshot().foodLogs).toEqual({})
    a.close()
    b.close()
    await IdbRepository.deleteFor(user)
    const again = await IdbRepository.open(user)
    expect(again.getSnapshot().foodLogs).toEqual({})
    again.close()
  })
})
