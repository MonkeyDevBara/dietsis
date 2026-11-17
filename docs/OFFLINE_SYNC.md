# Offline-First & Sinkronisasi

## Tujuan

1. UI tidak pernah menunggu jaringan; setiap aksi langsung tampil.
2. Tidak ada data hilang saat offline, saat tab ditutup, atau saat aplikasi crash.
3. Retry tidak pernah menggandakan data.
4. Perubahan dari beberapa perangkat bertemu secara deterministik; konflik dilaporkan, bukan disembunyikan.

## Komponen klien

| Komponen | Tanggung jawab |
|---|---|
| IndexedDB `dietsis-<userId>` | Object store: `profile`, `favorites`, `days`, `food_logs`, `custom_foods`, `weights`, `outbox`, `meta` |
| `meta` | `cursor` (seq terakhir yang ditarik), `data_epoch`, `last_sync_at`, `user` terakhir yang terverifikasi |
| `outbox` | Antrean operasi FIFO: `{op_id, type, payload, entity, key, created_at, attempts, last_error}` |
| Actions | Satu transaksi IDB: ubah entitas lokal (optimistis) **dan** tambahkan operasi ke outbox |
| Sync engine | Push outbox → pull change feed; jadwal, backoff, status |
| Service worker | Hanya app shell (HTML/JS/CSS/ikon). Data **tidak** di-cache SW; data hidup di IndexedDB |

Setiap entitas lokal menyimpan `server_version` (versi terakhir yang dikonfirmasi server) di samping data optimistisnya.

## Alur penulisan

```mermaid
sequenceDiagram
  participant U as Pengguna
  participant A as Action
  participant L as IndexedDB
  participant E as Sync engine
  participant S as Server
  U->>A: + 1 gelas air
  A->>L: tx: days[date].water += 1 ; outbox.add({op_id, day.water, delta 1})
  L-->>U: UI langsung berubah
  A->>E: schedule()
  E->>S: POST /sync/push {data_epoch, ops[...]}
  S->>S: per op: BEGIN IMMEDIATE · cek ledger · terapkan · change_log · simpan hasil · COMMIT
  S-->>E: results [{op_id, status, state}]
  E->>L: tx: hapus op dari outbox ; terapkan state server (server_version)
  E->>S: GET /sync/pull?since=cursor
  S-->>E: changes, cursor
  E->>L: terapkan perubahan (rebase op tertunda)
```

## Operasi

Tipe operasi dan payload: [API.md → Sync](API.md#post-syncpush). Sifat tiap operasi:

| Operasi | Idempotensi | Konflik | Aturan |
|---|---|---|---|
| `day.water` | ledger `op_id` | tidak pernah | Delta komutatif; `water = clamp(water + delta, 0, 100)` atomik di SQL |
| `favorite.set` | alami (set) | tidak pernah | Per item; operasi terakhir yang tiba menang untuk `food_id` itu |
| `food_log.create` | `id` klien + ledger | tidak pernah | `id` sudah ada → kembalikan entri yang ada |
| `food_log.delete` | alami | tidak pernah | Soft delete; menghapus yang sudah terhapus = no-op |
| `food_log.restore` | alami | tidak pernah | Menghapus `deleted_at`; entri aktif = no-op |
| `custom_food.create` | `id` klien + ledger | tidak pernah | |
| `food_log.update` | ledger | `base_version` | Patch per field; entri terhapus → `conflict` |
| `day.update` | ledger | `base_version` | Patch `steps`/`note`; air tidak mempengaruhi versi |
| `weight.put` / `weight.delete` | ledger | `base_version` | Satu entri per tanggal; `base_version: 0` = membuat baru |
| `profile.put` | ledger | `base_version` | Target non-manual dihitung ulang server |

### Ledger idempotensi

Server menyimpan hasil setiap operasi di `sync_operations (user_id, op_id)` dalam transaksi yang sama dengan efeknya. Bila operasi dengan `op_id` sama tiba lagi (response sebelumnya hilang di jaringan), server mengembalikan hasil tersimpan **tanpa** menerapkan ulang. Karena efek dan ledger di-commit atomik, tidak ada keadaan "sudah diterapkan tapi belum tercatat".

Ledger disimpan 90 hari. Klien menandai operasi yang berumur > 60 hari tanpa berhasil terkirim sebagai *stale*: ditampilkan di Settings dan hanya dikirim setelah konfirmasi pengguna (operasi non-air tetap aman karena idempoten alami; `day.water` lama dibuang).

## Konflik

Penulisan berversi membawa `base_version` = `server_version` lokal saat operasi dibuat.

| Situasi di server | Hasil | Tindakan klien |
|---|---|---|
| `base_version` = versi server | `applied`, versi +1 | Hapus op; simpan `state` & `server_version` |
| `base_version` ≠ versi server | `conflict` + `state` terkini; **tidak diterapkan** | Hapus op; ganti entitas lokal dengan `state`; toast "Diperbarui dari perangkat lain — perubahanmu di perangkat ini tidak disimpan" dengan nilai yang ditolak ditampilkan agar bisa diulang |
| Entitas tidak ada (update/delete berversi) | `conflict` dengan `state: null` | Hapus entitas lokal; toast |
| Validasi gagal | `rejected` + error | Hapus op; ambil ulang entitas via pull; toast menjelaskan apa yang dibatalkan |

Alasan memilih "server terkini menang" daripada last-write-wins: perubahan perangkat lain tidak pernah tertimpa diam-diam, dan pengguna selalu diberi tahu ([D-015](DECISIONS.md#d-015--konflik-server-terkini-menang-untuk-penulisan-berversi)).

### Mencegah konflik dengan diri sendiri

- **Koalesensi:** bila outbox sudah berisi update berversi untuk entitas yang sama dan belum dikirim, patch baru digabung ke operasi itu (base_version tetap). Delete setelah create yang belum terkirim → keduanya dibuang. Operasi `day.water` untuk tanggal sama digabung (jumlah delta; dibuang bila 0).
- **Rebase saat ack:** bila operasi berversi diterapkan dan masih ada operasi berikutnya untuk entitas yang sama (dibuat saat request sedang terbang), `base_version` operasi berikutnya diperbarui ke versi hasil ack.
- Satu engine per tab, dikoordinasi dengan Web Locks API (`navigator.locks`, lock `dietsis-sync`) agar dua tab tidak mengirim outbox yang sama bersamaan.

## Pull & rebase

`GET /sync/pull?since=cursor` mengembalikan state terkini setiap entitas yang berubah setelah `cursor`, urut `seq`, berhalaman hingga `has_more=false`. Untuk setiap perubahan:

1. Entitas **tanpa** operasi tertunda → tulis `state` (atau hapus bila `null`), simpan `server_version`.
2. Entitas **dengan** operasi tertunda → simpan `state` sebagai basis, lalu terapkan ulang efek optimistis operasi tertunda di atasnya (untuk air: `water = server + Σ delta tertunda`). Konflik sesungguhnya diputuskan server saat operasi itu di-push.
3. Simpan `cursor` baru dalam transaksi IDB yang sama dengan perubahan → pull yang terputus aman diulang.

Change log terkompaksi (satu baris per entitas) sehingga pull inkremental selalu benar untuk cursor berapa pun, dan sinkron penuh (`since=0`) hanya mengirim state akhir.

## Jadwal, retry, backoff

Pemicu siklus sinkron (push lalu pull): aplikasi dibuka, event `online`, tab kembali terlihat, 1 detik setelah mutasi lokal (debounce), dan periodik tiap 60 detik saat tab terlihat.

| Kegagalan | Perilaku |
|---|---|
| Jaringan / timeout / 5xx / 429 | Operasi tetap di outbox; retry dengan exponential backoff `min(5 menit, 1 s × 2^n)` ± 20% jitter; `Retry-After` dihormati; reset saat berhasil atau event `online` |
| 401 | Sinkron berhenti; banner "Sesi berakhir — masuk lagi"; outbox & data lokal tetap utuh; lanjut setelah login ulang dengan akun yang sama |
| 409 `epoch_mismatch` | Data dihapus dari perangkat lain: hapus semua store lokal + outbox, set epoch baru, sinkron penuh; pengguna diberi tahu |
| 413 | Kirim ulang dengan batch lebih kecil (bagi dua) |

Push mengirim maksimal 200 operasi per request dalam urutan FIFO. Request terputus setelah server commit aman karena ledger.

## Status sinkronisasi (UI)

| Status | Kondisi |
|---|---|
| Tersinkron | Outbox kosong, pull terakhir sukses |
| Menyinkronkan… | Siklus berjalan |
| Offline · N tertunda | `navigator.onLine === false` atau gagal jaringan |
| Gagal · coba lagi dalam X | Backoff aktif setelah error server |
| Perlu login ulang | 401 |

Settings menampilkan jumlah operasi tertunda, waktu sinkron terakhir, dan tombol "Sinkronkan sekarang".

## Login, logout, dan banyak akun

- Database lokal dipisah per pengguna (`dietsis-<userId>`) — data dua akun tidak pernah tercampur.
- Saat offline, aplikasi membuka database pengguna terakhir yang terverifikasi (`meta.user`) dan tetap dapat dipakai; `/auth/me` diverifikasi saat online.
- Logout: outbox kosong → hapus database lokal. Outbox berisi → dialog: "Sinkronkan dulu" / "Keluar dan buang N perubahan".

## Skenario uji (M6/M7)

1. Offline: tambah, edit, hapus, undo makanan → online → server sama dengan lokal.
2. Response push hilang (server commit, klien timeout) → retry → tidak ada duplikat, air tidak terhitung dua kali.
3. Dua perangkat menambah air bersamaan offline → keduanya online → total = jumlah semua delta.
4. Dua perangkat mengedit entri yang sama → yang kedua menerima `conflict`, state konvergen, toast tampil.
5. Perangkat A menghapus entri, perangkat B (offline) mengeditnya → B menerima `conflict`, entri tetap terhapus.
6. Perangkat A "hapus semua data", perangkat B offline punya operasi tertunda → B menerima `epoch_mismatch`, data lama tidak hidup kembali.
7. Tab ditutup saat push berjalan → dibuka lagi → outbox dikirim ulang, hasil konsisten.
8. Dua tab terbuka → hanya satu yang push (Web Locks).
