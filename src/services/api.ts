// Thin fetch wrapper for /api/v1. Same-origin, so the session cookie is sent automatically and the
// browser adds the Origin header that the server's CSRF check expects.

export class ApiError extends Error {
  readonly status: number // 0 = network failure / timeout
  readonly code: string
  readonly details: Record<string, string> | undefined
  readonly current: unknown
  readonly retryAfterMs: number | undefined

  constructor(status: number, code: string, message: string, extra: { details?: Record<string, string>; current?: unknown; retryAfterMs?: number } = {}) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.details = extra.details
    this.current = extra.current
    this.retryAfterMs = extra.retryAfterMs
  }

  /** Worth retrying later: offline, timeout, throttled or a server-side failure. */
  get transient(): boolean {
    return this.status === 0 || this.status === 429 || this.status >= 500
  }
}

export interface ApiOptions {
  idempotencyKey?: string
  signal?: AbortSignal
  timeoutMs?: number
}

interface ErrorEnvelope {
  error?: { code?: string; message?: string; details?: Record<string, string>; current?: unknown }
}

export async function api<T>(method: string, path: string, body?: unknown, opts: ApiOptions = {}): Promise<T> {
  const ctrl = new AbortController()
  const timer = setTimeout(() => ctrl.abort(), opts.timeoutMs ?? 15_000)
  opts.signal?.addEventListener('abort', () => ctrl.abort(), { once: true })

  const headers: Record<string, string> = {}
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (opts.idempotencyKey) headers['Idempotency-Key'] = opts.idempotencyKey

  let res: Response
  try {
    res = await fetch(`/api/v1${path}`, {
      method,
      headers,
      credentials: 'same-origin',
      signal: ctrl.signal,
      ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
    })
  } catch {
    throw new ApiError(0, 'network', 'Tidak dapat terhubung ke server.')
  } finally {
    clearTimeout(timer)
  }

  if (res.status === 204) return undefined as T
  let json: unknown
  try {
    json = await res.json()
  } catch {
    if (res.ok) throw new ApiError(res.status, 'bad_response', 'Respons server tidak valid.')
    throw new ApiError(res.status, 'http_error', `Server menjawab ${res.status}.`)
  }
  if (!res.ok) {
    const e = (json as ErrorEnvelope).error
    const retry = Number(res.headers.get('Retry-After'))
    throw new ApiError(res.status, e?.code ?? 'http_error', e?.message ?? `Server menjawab ${res.status}.`, {
      ...(e?.details ? { details: e.details } : {}),
      ...(e && 'current' in e ? { current: e.current } : {}),
      ...(Number.isFinite(retry) && retry > 0 ? { retryAfterMs: retry * 1000 } : {}),
    })
  }
  return (json as { data: T }).data
}
