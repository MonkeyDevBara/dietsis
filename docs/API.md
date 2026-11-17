# API Contract — `/api/v1`

Semua endpoint berada di bawah `https://dietsis.transbara.my.id/api/v1`. Status implementasi per endpoint ada di [PM_STATUS.md](PM_STATUS.md).

## Konvensi

### Request

- Body selalu `application/json` (UTF-8). Request non-GET tanpa `Content-Type: application/json` → `415`. Endpoint tanpa body (DELETE, POST restore/logout) tetap boleh mengirim `{}` atau body kosong.
- JSON di-decode ketat: field tak dikenal → `400 bad_request`; hanya satu objek JSON per body.
- Batas body: 64 KiB; `/sync/push` 1 MiB → `413 payload_too_large`.
- Identitas pengguna **hanya** dari cookie sesi. Tidak ada endpoint yang menerima `user_id`.
- CSRF (non-GET): header `Origin` harus sama dengan origin aplikasi; bila tidak ada `Origin`, `Sec-Fetch-Site` harus `same-origin` → selain itu `403 csrf_failed`.
- Penamaan field `snake_case`. Tanggal `YYYY-MM-DD` (hari lokal pengguna). Timestamp integer epoch **milidetik**.

### Response sukses

```json
{ "data": { } }
```

List dengan paginasi:

```json
{ "data": [ ], "next_cursor": "opaque-string-or-null" }
```

`next_cursor` adalah string opaque; kirim kembali sebagai `?cursor=` untuk halaman berikutnya. `null` = halaman terakhir.

### Response error

```json
{
  "error": {
    "code": "validation_failed",
    "message": "Data tidak valid.",
    "details": { "weight_kg": "harus antara 30 dan 300" }
  }
}
```

| HTTP | `code` | Arti |
|---|---|---|
| 400 | `bad_request` | JSON rusak, field tak dikenal, parameter query salah |
| 401 | `unauthenticated` | Tidak ada sesi / sesi kedaluwarsa / dicabut |
| 403 | `csrf_failed` | Origin tidak valid |
| 404 | `not_found` | Resource tidak ada **untuk pengguna ini** (milik pengguna lain juga 404, bukan 403) |
| 409 | `conflict` | `base_version` basi; `error.current` berisi state server (objek entitas atau `null`) |
| 409 | `epoch_mismatch` | Data pengguna sudah dihapus dari perangkat lain; klien harus reset lokal |
| 413 | `payload_too_large` | Body melebihi batas |
| 415 | `unsupported_media_type` | Bukan `application/json` |
| 422 | `validation_failed` | Nilai di luar batas ([DECISIONS D-012](DECISIONS.md#d-012--batas-validasi)); `details` per field |
| 428 | `idempotency_key_required` | Endpoint non-idempoten tanpa `Idempotency-Key` |
| 429 | `rate_limited` | Terlalu banyak request; header `Retry-After` (detik) |
| 500 | `internal` | Kesalahan server; `message` generik, detail hanya di log (dengan `request_id`) |

Setiap response menyertakan header `X-Request-ID`.

### Idempotensi

- Endpoint yang tidak idempoten secara alami (`POST /days/{date}/water`) **wajib** header `Idempotency-Key: <uuid>`. Kunci dicatat di ledger `sync_operations`; request ulang dengan kunci sama mengembalikan hasil yang sama tanpa menerapkan ulang.
- `POST /food-logs` dan `POST /custom-foods` idempoten via `id` yang dibuat klien.
- Header `Idempotency-Key` opsional di semua endpoint mutasi lain dan berperilaku sama.

### Versi & konflik

Entitas yang dapat diubah membawa `version` (integer, naik setiap penulisan). Mutasi berversi menerima `base_version` opsional:

- tidak dikirim → tulis tanpa syarat (last write wins);
- dikirim dan sama dengan versi server → diterapkan;
- dikirim dan berbeda → `409 conflict`, `error.current` berisi state terkini. Klien sinkronisasi **selalu** mengirim `base_version`.

`base_version: 0` berarti "saya yakin entitas belum ada".

## Skema entitas

```ts
type User = {
  id: string; email: string; name: string; picture: string
  created_at: number
}

type Profile = {
  display_name: string
  gender: 'male' | 'female'
  age: number
  height_cm: number
  weight_kg: number
  goal_weight_kg: number
  activity_level: 'sedentary' | 'light' | 'moderate' | 'active' | 'very_active'
  calorie_target: number          // kkal/hari
  protein_target_g: number
  water_target_glasses: number    // 1 gelas = 250 ml
  calorie_target_manual: boolean
  protein_target_manual: boolean
  water_target_manual: boolean
  recommended: { calorie_target: number; protein_target_g: number; water_target_glasses: number } // read-only
  version: number; created_at: number; updated_at: number
}

type Day = {
  date: string
  water_glasses: number
  steps: number | null
  note: string
  version: number                 // naik hanya untuk steps/note; air adalah counter tanpa versi
  updated_at: number              // 0 bila hari belum pernah ditulis
}

type FoodLog = {
  id: string                      // UUID dari klien
  date: string
  meal: 'breakfast' | 'lunch' | 'dinner' | 'snack'
  food_id: string | null          // 'b:<slug>' katalog bawaan, 'c:<uuid>' makanan kustom
  name: string
  serving_label: string           // mis. "1 piring (200 g)"
  serving_grams: number | null
  quantity: number                // jumlah porsi
  kcal: number; protein_g: number; carbs_g: number; fat_g: number   // PER PORSI (snapshot)
  totals: { kcal: number; protein_g: number; carbs_g: number; fat_g: number } // read-only, dihitung server
  version: number; created_at: number; updated_at: number
  deleted_at: number | null
}

type CustomFood = {
  id: string; name: string; serving_label: string; serving_grams: number | null
  kcal: number; protein_g: number; carbs_g: number; fat_g: number   // per porsi
  version: number; created_at: number; updated_at: number
}

type Weight = {
  date: string; weight_kg: number; waist_cm: number | null
  version: number; updated_at: number
}
```

---

## Health

### `GET /health`

Publik. `200 {"data": {"status": "ok", "version": "v1.0.0"}}`. Mulai M2 juga memeriksa koneksi database (`503` bila gagal).

## Authentication

### `POST /auth/google`

Publik, rate limit ketat per IP. Body:

```json
{ "credential": "<Google ID token dari Google Identity Services>" }
```

Server memverifikasi tanda tangan RS256 (JWKS Google), `iss`, `aud` = client ID, `exp`/`iat`, `email_verified`, lalu membuat/memperbarui user dan sesi.

`200`, menyetel cookie sesi:

```json
{ "data": { "user": { "id": "…", "email": "…", "name": "…", "picture": "…", "created_at": 1763350000000 }, "has_profile": false, "data_epoch": 1 } }
```

Error: `400 bad_request` (credential kosong), `401 unauthenticated` (token tidak valid — alasan detail hanya di log), `429`.

### `GET /auth/me`

`200 {"data": {"user": User, "has_profile": boolean, "data_epoch": number}}` atau `401`. Memperpanjang sesi sliding bila sudah melewati separuh TTL.

### `POST /auth/logout`

Body opsional `{"all_devices": true}` mencabut semua sesi pengguna. `204`, cookie dihapus. Idempoten (tanpa sesi tetap `204`).

## Profile

### `GET /profile`

`200 {"data": Profile}` atau `404 not_found` (belum onboarding).

### `PUT /profile`

Body = semua field Profile yang dapat ditulis (`display_name` … `water_target_manual`) + `base_version` opsional. Target dengan flag manual `false` **dihitung ulang server** dari formula ([D-011](DECISIONS.md#d-011--formula-rekomendasi-target)); nilai target yang dikirim untuk field non-manual diabaikan.

`200 {"data": Profile}` (dibuat atau diperbarui) · `409 conflict` · `422`.

## Favorites

### `GET /favorites`

`200 {"data": [{"food_id": "b:nasi-putih", "created_at": 1763350000000}]}` — maksimal 500.

### `PUT /favorites`

Mengganti seluruh set favorit.

```json
{ "food_ids": ["b:nasi-putih", "c:0b9f…"] }
```

`200 {"data": [ … set baru … ]}` · `422` (format `food_id` salah, > 500 item). Klien sinkronisasi memakai operasi per item (`favorite.set`) agar dua perangkat tidak saling menimpa.

## Days

### `GET /days?from=YYYY-MM-DD&to=YYYY-MM-DD`

Rentang inklusif, maks 366 hari. Hanya hari yang pernah ditulis yang dikembalikan, urut tanggal naik. `200 {"data": [Day]}`.

### `GET /days/{date}`

`200 {"data": Day}` — hari yang belum ditulis dikembalikan dengan nilai default (`water_glasses: 0`, `steps: null`, `note: ""`, `version: 0`, `updated_at: 0`).

### `PUT /days/{date}`

Patch field aktivitas; field yang tidak dikirim tidak diubah.

```json
{ "steps": 5200, "note": "Jalan pagi", "base_version": 3 }
```

`200 {"data": Day}` · `409` · `422`.

### `POST /days/{date}/water`

**Wajib** `Idempotency-Key`. Menambah/mengurangi gelas secara atomik di database (`water_glasses = clamp(water_glasses + delta, 0, 100)`), aman untuk ketukan cepat dan beberapa perangkat sekaligus.

```json
{ "delta": 1 }
```

`200 {"data": Day}` · `422` (`delta` 0 atau di luar −20…20) · `428`.

## Food logs

### `GET /food-logs?from=&to=&limit=&cursor=&include_deleted=`

- `from`/`to` wajib, rentang maks 366 hari.
- Urutan: `date`, `created_at`, `id` naik. `limit` default 200, maks 500.
- `include_deleted=true` menyertakan entri ter-soft-delete (untuk undo lintas perangkat).

`200 {"data": [FoodLog], "next_cursor": …}`.

### `POST /food-logs`

```json
{
  "id": "6f1c2a4e-…",
  "date": "2026-11-17", "meal": "lunch",
  "food_id": "b:nasi-putih", "name": "Nasi putih",
  "serving_label": "1 piring (200 g)", "serving_grams": 200,
  "quantity": 1.5,
  "kcal": 260, "protein_g": 5.4, "carbs_g": 57.2, "fat_g": 0.6
}
```

`id` opsional (server membuat UUID bila kosong, tetapi tanpa `id` request tidak idempoten). `201 {"data": FoodLog}`; bila `id` sudah ada untuk pengguna ini → `200` dengan entri yang sudah ada (idempoten). `422`.

### `PATCH /food-logs/{id}`

Field yang dapat diubah: `date`, `meal`, `name`, `serving_label`, `serving_grams`, `quantity`, `kcal`, `protein_g`, `carbs_g`, `fat_g`; plus `base_version` opsional.

```json
{ "quantity": 2, "meal": "dinner", "base_version": 1 }
```

`200 {"data": FoodLog}` · `404` · `409` (termasuk bila entri sudah dihapus) · `422`.

### `DELETE /food-logs/{id}`

Soft delete (dapat di-undo). `200 {"data": FoodLog}` dengan `deleted_at` terisi. Idempoten: menghapus entri yang sudah terhapus → `200`. `404` bila tidak ada. Entri terhapus dibersihkan permanen setelah 30 hari.

### `POST /food-logs/{id}/restore`

Membatalkan soft delete. `200 {"data": FoodLog}`. Idempoten. `404` bila tidak ada / sudah dibersihkan permanen.

## Custom foods

### `GET /custom-foods?limit=&cursor=`

Urut `name`. `limit` default 200, maks 500. `200 {"data": [CustomFood], "next_cursor": …}`.

### `POST /custom-foods`

```json
{ "id": "0b9f…", "name": "Smoothie bayam", "serving_label": "1 gelas", "serving_grams": 300, "kcal": 180, "protein_g": 6, "carbs_g": 30, "fat_g": 3 }
```

`201 {"data": CustomFood}` · `200` bila `id` sudah ada (idempoten) · `422`.

## Weights

### `GET /weights?from=&to=&limit=&cursor=`

`from`/`to` opsional (default: seluruh riwayat). Urut tanggal naik. `limit` default 366, maks 1000. `200 {"data": [Weight], "next_cursor": …}`.

### `PUT /weights/{date}`

Upsert satu entri per tanggal.

```json
{ "weight_kg": 72.4, "waist_cm": 80, "base_version": 0 }
```

`200 {"data": Weight}` · `409` · `422`.

### `DELETE /weights/{date}`

Body opsional `{"base_version": n}`. `204`. Idempoten (tanggal tanpa entri → `204`).

## Sync

Detail semantik di [OFFLINE_SYNC.md](OFFLINE_SYNC.md).

### `POST /sync/push`

```json
{
  "data_epoch": 1,
  "ops": [
    { "op_id": "uuid", "type": "day.water", "payload": { "date": "2026-11-17", "delta": 1 } },
    { "op_id": "uuid", "type": "food_log.update", "payload": { "id": "…", "base_version": 2, "patch": { "quantity": 2 } } }
  ]
}
```

Maks 200 operasi per request, diproses berurutan, masing-masing dalam transaksi sendiri.

`200`:

```json
{
  "data": {
    "data_epoch": 1,
    "results": [
      { "op_id": "uuid", "status": "applied", "entity": "day", "key": "2026-11-17", "state": { } },
      { "op_id": "uuid", "status": "conflict", "entity": "food_log", "key": "…", "state": { } },
      { "op_id": "uuid", "status": "rejected", "error": { "code": "validation_failed", "message": "…", "details": { } } }
    ]
  }
}
```

- `status`: `applied` | `conflict` | `rejected`. Hasil disimpan per `op_id`; retry mengembalikan hasil identik.
- `state`: state entitas terkini di server setelah operasi (`null` bila entitas tidak ada/terhapus permanen).
- `409 epoch_mismatch` bila `data_epoch` ≠ epoch server; tidak ada operasi yang diterapkan.

Tipe operasi:

| `type` | `payload` | Konflik? |
|---|---|---|
| `profile.put` | Profile writable fields + `base_version` | ya |
| `favorite.set` | `{food_id, favorite: boolean}` | tidak |
| `day.water` | `{date, delta}` | tidak (counter) |
| `day.update` | `{date, base_version, patch: {steps?, note?}}` | ya |
| `food_log.create` | FoodLog writable fields termasuk `id` | tidak (idempoten) |
| `food_log.update` | `{id, base_version, patch: {...}}` | ya; juga bila entri terhapus |
| `food_log.delete` | `{id}` | tidak |
| `food_log.restore` | `{id}` | tidak |
| `custom_food.create` | CustomFood writable fields termasuk `id` | tidak (idempoten) |
| `weight.put` | `{date, weight_kg, waist_cm, base_version}` | ya |
| `weight.delete` | `{date, base_version}` | ya |

### `GET /sync/pull?since=<seq>&limit=<n>`

`since` default 0 (sinkron penuh). `limit` default 500, maks 1000.

```json
{
  "data": {
    "data_epoch": 1,
    "changes": [
      { "seq": 101, "entity": "food_log", "key": "6f1c…", "state": { } },
      { "seq": 102, "entity": "weight", "key": "2026-11-16", "state": null }
    ],
    "cursor": 102,
    "has_more": false
  }
}
```

`state: null` = entitas dihapus. `entity` ∈ `profile` (key `"profile"`), `favorite` (key `food_id`), `day` (key tanggal), `food_log` (key id), `custom_food` (key id), `weight` (key tanggal).

## Data

### `GET /export?dataset=<name>`

`dataset` ∈ `food_logs` (default), `weights`, `days`, `custom_foods`, `profile`. Response `200 text/csv; charset=utf-8` dengan `Content-Disposition: attachment; filename="dietsis-food_logs-2026-11-17.csv"`. UTF-8 BOM, pemisah koma, header baris pertama, mitigasi CSV injection. Food logs berisi kolom per porsi dan total.

### `DELETE /account/data`

```json
{ "confirm": "HAPUS" }
```

Menghapus profil, favorit, hari, food log, makanan kustom, berat, ledger sinkronisasi, dan change log milik pengguna dalam satu transaksi; menaikkan `data_epoch`. Akun dan sesi tetap (pengguna kembali ke onboarding). `200 {"data": {"data_epoch": 2}}` · `422` bila `confirm` salah.
