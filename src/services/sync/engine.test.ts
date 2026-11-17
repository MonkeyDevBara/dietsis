import 'fake-indexeddb/auto'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../api'
import type { api } from '../api'
import { IdbRepository } from '../local/idbRepository'
import type { Op } from '../local/state'
import { SyncEngine } from './engine'

const ID_A = '11111111-1111-4111-8111-111111111111'
const T = 1_800_000_000_000
let n = 0

const logOp = (over: Record<string, unknown> = {}): Op => ({
  type: 'food_log.create',
  payload: { id: ID_A, date: '2026-11-17', meal: 'lunch', food_id: 'b:nasi-putih', name: 'Nasi', serving_label: '1', serving_grams: null, quantity: 1, kcal: 200, protein_g: 4, carbs_g: 40, fat_g: 1, ...over } as never,
})
const water = (delta: number): Op => ({ type: 'day.water', payload: { date: '2026-11-17', delta } })

interface Call {
  method: string
  path: string
  body?: { data_epoch: number; ops: { op_id: string; type: string; payload: Record<string, unknown> }[] }
}

/** Scripted server: `push` decides each push response, pulls return what `pullQueue` holds. */
class FakeServer {
  calls: Call[] = []
  epoch = 1
  pullData: { changes: unknown[]; cursor: number; has_more: boolean }[] = []
  pushHandler: (body: NonNullable<Call['body']>) => unknown = (b) => ({ data_epoch: b.data_epoch, results: b.ops.map((o) => ({ op_id: o.op_id, status: 'applied', state: null })) })
  failNextPush: ApiError[] = []

  call = ((method: string, path: string, body?: unknown) => {
    this.calls.push({ method, path, ...(body ? { body: body as Call['body'] } : {}) })
    if (path.startsWith('/sync/pull')) {
      const since = Number(new URL(path, 'http://x').searchParams.get('since'))
      const page = this.pullData.shift() ?? { changes: [], cursor: since, has_more: false }
      return Promise.resolve({ data_epoch: this.epoch, ...page })
    }
    if (path === '/sync/push') {
      const err = this.failNextPush.shift()
      if (err) return Promise.reject(err)
      return Promise.resolve(this.pushHandler(body as NonNullable<Call['body']>))
    }
    return Promise.reject(new Error(`unexpected ${method} ${path}`))
  }) as unknown as typeof api

  pushes = () => this.calls.filter((c) => c.path === '/sync/push')
}

let user: string
let repo: IdbRepository
let server: FakeServer
let clock: number
const hooks = { onConflicts: vi.fn(), onRejected: vi.fn(), onUnauthorized: vi.fn(), onReset: vi.fn() }

function engine() {
  return new SyncEngine(repo, hooks, { call: server.call, now: () => clock, random: () => 0.5 })
}

beforeEach(async () => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval'] })
  user = `eng-${++n}-${Math.random().toString(36).slice(2)}`
  repo = await IdbRepository.open(user, () => T)
  server = new FakeServer()
  clock = 1000
  for (const h of Object.values(hooks)) h.mockClear()
})
afterEach(() => {
  repo.close()
  vi.useRealTimers()
})

describe('push and pull', () => {
  it('learns the epoch first, pushes with base versions, and adopts the server state on ack', async () => {
    await repo.apply(logOp())
    server.pushHandler = (b) => ({
      data_epoch: 1,
      results: b.ops.map((o) => ({ op_id: o.op_id, status: 'applied', state: { ...(o.payload as object), totals: { kcal: 200, protein_g: 4, carbs_g: 40, fat_g: 1 }, version: 1, created_at: 5, updated_at: 5, deleted_at: null } })),
    })
    const e = engine()
    await e.syncNow()

    expect(server.calls.map((c) => c.path.split('?')[0])).toEqual(['/sync/pull', '/sync/push', '/sync/pull'])
    expect(server.pushes()[0]?.body?.data_epoch).toBe(1)
    expect(server.pushes()[0]?.body?.ops[0]).toMatchObject({ type: 'food_log.create', payload: { id: ID_A } })
    expect(repo.pendingCount()).toBe(0)
    expect(repo.serverState({ entity: 'food_log', key: ID_A })).toMatchObject({ version: 1 })
    expect(e.getSnapshot()).toMatchObject({ phase: 'idle', pending: 0, synced: true, lastSyncAt: 1000, retryAt: null })
  })

  it('pulls pages until has_more is false and stores the cursor', async () => {
    const day = (d: string) => ({ entity: 'day', key: d, state: { date: d, water_glasses: 1, steps: null, note: '', version: 0, updated_at: 1 } })
    server.pullData = [
      { changes: [{ seq: 1, ...day('2026-11-15') }], cursor: 1, has_more: true },
      { changes: [{ seq: 2, ...day('2026-11-16') }], cursor: 2, has_more: false },
    ]
    await engine().syncNow()
    expect(Object.keys(repo.getSnapshot().days).sort()).toEqual(['2026-11-15', '2026-11-16'])
    expect(repo.getMeta()).toMatchObject({ cursor: 2, epoch: 1, synced: true })
    expect(server.calls.filter((c) => c.path.startsWith('/sync/pull')).map((c) => c.path)).toEqual(['/sync/pull?since=0&limit=500', '/sync/pull?since=1&limit=500', '/sync/pull?since=2&limit=500'])
  })

  it('applies server deletions (state null) and keeps unsent local changes on top', async () => {
    await repo.apply(water(2))
    server.pullData = [{ changes: [{ seq: 1, entity: 'day', key: '2026-11-17', state: { date: '2026-11-17', water_glasses: 3, steps: null, note: '', version: 0, updated_at: 1 } }], cursor: 1, has_more: false }]
    server.pushHandler = (b) => ({ data_epoch: 1, results: b.ops.map((o) => ({ op_id: o.op_id, status: 'applied', state: { date: '2026-11-17', water_glasses: 5, steps: null, note: '', version: 0, updated_at: 2 } })) })
    await engine().syncNow()
    expect(repo.getSnapshot().days['2026-11-17']?.water_glasses).toBe(5)
  })
})

describe('failures and retries', () => {
  it('keeps ops queued on network failure and resends the SAME op_id on retry', async () => {
    await repo.apply(water(1))
    const e = engine()
    await e.syncNow() // epoch pull ok
    expect(repo.pendingCount()).toBe(0)

    await repo.apply(water(1))
    server.failNextPush = [new ApiError(0, 'network', 'offline')]
    await e.syncNow()
    expect(repo.pendingCount()).toBe(1)
    expect(e.getSnapshot()).toMatchObject({ phase: 'offline', pending: 1 })
    const failedId = server.pushes()[1]?.body?.ops[0]?.op_id
    expect(failedId).toBeTruthy()

    // The response may have been lost AFTER the server applied it: the retry carries the same id,
    // so the server answers from its ledger instead of applying again.
    await vi.advanceTimersByTimeAsync(10_000)
    expect(server.pushes().length).toBe(3)
    expect(server.pushes()[2]?.body?.ops[0]?.op_id).toBe(failedId)
    await vi.waitFor(() => expect(e.getSnapshot().phase).toBe('idle'))
    expect(repo.pendingCount()).toBe(0)
    e.stop()
  })

  it('backs off exponentially with a cap and honors Retry-After', async () => {
    await repo.setMeta({ epoch: 1 })
    await repo.apply(water(1))
    const e = engine()
    const delays: number[] = []
    for (let i = 0; i < 10; i++) {
      server.failNextPush = [new ApiError(503, 'unavailable', 'x')]
      clock += 10_000_000
      await e.syncNow()
      delays.push((e.getSnapshot().retryAt ?? 0) - clock)
      await vi.advanceTimersByTimeAsync(0)
      vi.clearAllTimers()
    }
    // random()=0.5 → factor 1.0; 2^attempt seconds, capped at 5 minutes.
    expect(delays.slice(0, 4)).toEqual([2000, 4000, 8000, 16000])
    expect(Math.max(...delays)).toBe(300_000)
    expect(e.getSnapshot().phase).toBe('error')

    server.failNextPush = [new ApiError(429, 'rate_limited', 'x', { retryAfterMs: 7000 })]
    clock += 10_000_000
    await e.syncNow()
    expect((e.getSnapshot().retryAt ?? 0) - clock).toBe(7000)
    vi.clearAllTimers()
    e.stop()
  })

  it('a successful cycle resets the backoff', async () => {
    await repo.setMeta({ epoch: 1 })
    await repo.apply(water(1))
    const e = engine()
    server.failNextPush = [new ApiError(500, 'internal', 'x')]
    await e.syncNow()
    expect(e.getSnapshot().retryAt).not.toBeNull()
    vi.clearAllTimers()
    clock += 1_000_000
    await e.syncNow()
    expect(e.getSnapshot()).toMatchObject({ phase: 'idle', retryAt: null })
    e.stop()
  })

  it('a 413 halves the batch and resends', async () => {
    await repo.setMeta({ epoch: 1 })
    for (const d of ['2026-11-10', '2026-11-11', '2026-11-12', '2026-11-13']) await repo.apply({ type: 'day.water', payload: { date: d, delta: 1 } })
    server.failNextPush = [new ApiError(413, 'payload_too_large', 'x')]
    await engine().syncNow()
    expect(server.pushes().map((p) => p.body?.ops.length)).toEqual([4, 2, 2])
    expect(repo.pendingCount()).toBe(0)
  })

  it('401 stops syncing and tells the app, without losing data', async () => {
    await repo.setMeta({ epoch: 1 })
    await repo.apply(water(1))
    server.failNextPush = [new ApiError(401, 'unauthenticated', 'x')]
    const e = engine()
    await e.syncNow()
    expect(hooks.onUnauthorized).toHaveBeenCalledOnce()
    expect(e.getSnapshot().reauth).toBe(true)
    expect(repo.pendingCount()).toBe(1)
    const before = server.calls.length
    await e.syncNow()
    expect(server.calls.length).toBe(before)
  })
})

describe('conflicts, rejections and epochs', () => {
  it('adopts the server state on conflict and reports it', async () => {
    await repo.setMeta({ epoch: 1 })
    await repo.applyServer([{ entity: 'food_log', key: ID_A, state: { ...(logOp().payload as object), totals: {}, version: 1, created_at: 1, updated_at: 1, deleted_at: null } }], [])
    await repo.apply({ type: 'food_log.update', payload: { id: ID_A, patch: { quantity: 9 } } })
    expect(repo.getSnapshot().foodLogs[ID_A]?.quantity).toBe(9)

    const winner = { ...(logOp().payload as object), quantity: 2, totals: {}, version: 2, created_at: 1, updated_at: 2, deleted_at: null }
    server.pushHandler = (b) => ({ data_epoch: 1, results: b.ops.map((o) => ({ op_id: o.op_id, status: 'conflict', state: winner })) })
    await engine().syncNow()

    expect(server.pushes()[0]?.body?.ops[0]?.payload).toMatchObject({ id: ID_A, base_version: 1, patch: { quantity: 9 } })
    expect(repo.getSnapshot().foodLogs[ID_A]?.quantity).toBe(2)
    expect(repo.pendingCount()).toBe(0)
    expect(hooks.onConflicts).toHaveBeenCalledWith(1)
  })

  it('a conflict with state null (deleted elsewhere) removes the local entry', async () => {
    await repo.setMeta({ epoch: 1 })
    await repo.apply(logOp())
    await repo.applyServer([{ entity: 'food_log', key: ID_A, state: { ...(logOp().payload as object), totals: {}, version: 1, created_at: 1, updated_at: 1, deleted_at: null } }], repo.pending().map((i) => i.seq))
    await repo.apply({ type: 'food_log.update', payload: { id: ID_A, patch: { quantity: 3 } } })
    server.pushHandler = (b) => ({ data_epoch: 1, results: b.ops.map((o) => ({ op_id: o.op_id, status: 'conflict', state: null })) })
    await engine().syncNow()
    expect(repo.getSnapshot().foodLogs[ID_A]).toBeUndefined()
  })

  it('a rejected op is dropped and the entity reverts to the server state', async () => {
    await repo.setMeta({ epoch: 1 })
    await repo.apply(logOp())
    server.pushHandler = (b) => ({ data_epoch: 1, results: b.ops.map((o) => ({ op_id: o.op_id, status: 'rejected', error: { code: 'validation_failed' } })) })
    await engine().syncNow()
    expect(repo.pendingCount()).toBe(0)
    expect(repo.getSnapshot().foodLogs[ID_A]).toBeUndefined()
    expect(hooks.onRejected).toHaveBeenCalledWith(1)
  })

  it('epoch mismatch on push resets local data, then pulls from scratch', async () => {
    await repo.setMeta({ epoch: 1, cursor: 5, synced: true })
    await repo.apply(logOp())
    server.epoch = 2
    server.failNextPush = [new ApiError(409, 'epoch_mismatch', 'x', { details: { data_epoch: '2' } })]
    const e = engine()
    await e.syncNow()
    expect(hooks.onReset).toHaveBeenCalled()
    expect(repo.getSnapshot().foodLogs).toEqual({})
    expect(repo.pendingCount()).toBe(0)
    expect(repo.getMeta()).toMatchObject({ epoch: 2, synced: true })
    expect(server.calls[server.calls.length - 1]?.path).toContain('since=0')
  })

  it('epoch mismatch discovered on pull also resets', async () => {
    await repo.setMeta({ epoch: 1, cursor: 5, synced: true })
    await repo.applyServer([{ entity: 'weight', key: '2026-11-17', state: { date: '2026-11-17', weight_kg: 70, waist_cm: null, version: 1, updated_at: 1 } }], [])
    server.epoch = 3
    await engine().syncNow()
    expect(repo.getSnapshot().weights).toEqual({})
    expect(repo.getMeta().epoch).toBe(3)
  })

  it('a weight delete of an entry the server never had sends nothing', async () => {
    await repo.setMeta({ epoch: 1 })
    await repo.apply({ type: 'weight.put', payload: { date: '2026-11-17', weight_kg: 70, waist_cm: null } })
    await repo.apply({ type: 'weight.delete', payload: { date: '2026-11-17' } })
    await engine().syncNow()
    expect(server.pushes()).toHaveLength(0)
    expect(repo.pendingCount()).toBe(0)
  })
})

describe('changes made while a request is in flight', () => {
  it('are queued as their own op and never lost', async () => {
    await repo.setMeta({ epoch: 1 })
    await repo.apply(water(1))

    // A server that really accumulates water, and a first push that we can hold open.
    let total = 0
    server.pushHandler = (b) => ({
      data_epoch: 1,
      results: b.ops.map((o) => {
        total += o.payload.delta as number
        return { op_id: o.op_id, status: 'applied', state: { date: '2026-11-17', water_glasses: total, steps: null, note: '', version: 0, updated_at: 1 } }
      }),
    })
    let release: () => void = () => undefined
    const gate = new Promise<void>((r) => (release = r))
    const inner = server.call
    let first = true
    server.call = ((m: string, p: string, b: unknown) => {
      if (p === '/sync/push' && first) {
        first = false
        return gate.then(() => inner(m, p, b))
      }
      return inner(m, p, b)
    })

    const e = engine()
    const running = e.syncNow()
    await vi.advanceTimersByTimeAsync(0)
    await repo.apply(water(1)) // the user taps again while the first tap is on the wire
    expect(repo.pendingCount()).toBe(2)
    release()
    await running
    vi.clearAllTimers()

    // The in-flight tap was not rewritten (that would have lost the second tap when its ack removed it).
    expect(server.pushes().map((p) => p.body?.ops.map((o) => o.payload.delta))).toEqual([[1], [1]])
    expect(total).toBe(2)
    expect(repo.getSnapshot().days['2026-11-17']?.water_glasses).toBe(2)
    expect(repo.pendingCount()).toBe(0)
    e.stop()
  })
})
