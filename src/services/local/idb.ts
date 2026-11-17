// Minimal promise wrappers over IndexedDB (no dependency). One database per user:
// `dietsis-<userId>` with stores `entities`, `outbox` and `meta`.

export const STORES = { entities: 'entities', outbox: 'outbox', meta: 'meta' } as const

export function dbName(userId: string): string {
  return `dietsis-${userId}`
}

// Promise executors are used instead of Promise.withResolvers: that API needs Safari 17.4+, and an
// older iPhone must still be able to open its local database.
export function openDb(userId: string): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(dbName(userId), 1)
    req.onupgradeneeded = () => {
      const db = req.result
      db.createObjectStore(STORES.entities, { keyPath: 'id' })
      db.createObjectStore(STORES.outbox, { keyPath: 'seq', autoIncrement: true })
      db.createObjectStore(STORES.meta, { keyPath: 'k' })
    }
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error ?? new Error('IndexedDB tidak dapat dibuka'))
    req.onblocked = () => reject(new Error('IndexedDB terblokir oleh tab lain'))
  })
}

export function reqP<T>(r: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    r.onsuccess = () => resolve(r.result)
    r.onerror = () => reject(r.error ?? new Error('IndexedDB error'))
  })
}

/** Resolves when the transaction has committed; rejects if it aborted. */
export function txDone(tx: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error ?? new Error('IndexedDB error'))
    tx.onabort = () => reject(tx.error ?? new Error('IndexedDB transaksi dibatalkan'))
  })
}

export function deleteDb(userId: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.deleteDatabase(dbName(userId))
    req.onsuccess = () => resolve()
    req.onerror = () => reject(req.error ?? new Error('Gagal menghapus database lokal'))
    req.onblocked = () => resolve() // another tab still holds it; it is deleted once that closes
  })
}
