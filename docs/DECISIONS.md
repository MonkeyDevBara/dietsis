# Keputusan Arsitektur

Format: keputusan, alasan, konsekuensi. Keputusan baru ditambahkan di bawah; keputusan yang diganti ditandai **Digantikan oleh …**, tidak dihapus.

## D-001 — Frontend dan backend terpisah, satu origin

- **Keputusan:** SPA React disajikan Nginx; API Go di `127.0.0.1:8787` di-proxy pada `/api/v1`. Development memakai proxy Vite.
- **Alasan:** satu origin menghilangkan CORS dan memungkinkan cookie `SameSite=Strict`.
- **Konsekuensi:** tidak ada akses API lintas origin. Klien pihak ketiga tidak didukung (bukan kebutuhan).

## D-002 — SQLite via `modernc.org/sqlite` (pure Go)

- **Keputusan:** driver `modernc.org/sqlite`, bukan `mattn/go-sqlite3`.
- **Alasan:** tanpa CGO → binary statis, cross-compile dari macOS arm64 ke Linux amd64/arm64 dengan `GOOS/GOARCH` saja; tidak perlu toolchain C di VPS.
- **Konsekuensi:** sedikit lebih lambat dari driver CGO; tidak relevan untuk beban satu VPS personal.

## D-003 — Dependency backend minimal

- **Keputusan:** stdlib untuk router (`ServeMux` Go 1.22+), JWT RS256 + JWKS Google, rate limiter, CSV, logging (`slog`), migrasi (SQL di-`embed`). Satu-satunya dependency eksternal: driver SQLite.
- **Alasan:** `google.golang.org/api/idtoken` menarik puluhan modul; verifikasi ID token Google cukup ~150 baris yang bisa diaudit dan dites penuh.
- **Konsekuensi:** kode verifikasi JWT menjadi tanggung jawab kita → wajib test negatif (alg, iss, aud, exp, kid, signature).

## D-004 — TypeScript 6.0, bukan 7

- **Keputusan:** `typescript ~6.0.3`.
- **Alasan:** `typescript-eslint` (lint type-aware) mensyaratkan `typescript <6.1`. TS 7 (native) belum didukung tooling lint.
- **Konsekuensi:** naikkan ke TS 7 setelah `typescript-eslint` mendukungnya.

## D-005 — Identitas pengguna = Google `sub`

- **Keputusan:** `users.google_sub` UNIQUE adalah identitas; `email` disimpan untuk tampilan dan tidak unik.
- **Alasan:** email Google dapat berubah; `sub` stabil. Hanya token dengan `email_verified=true` yang diterima.

## D-006 — Sesi opaque di database, bukan JWT

- **Keputusan:** token acak 32 byte di cookie `HttpOnly`, `Secure`, `SameSite=Strict`; database menyimpan SHA-256-nya. Masa berlaku sliding (default 30 hari).
- **Alasan:** revokasi instan (logout, logout semua perangkat) dan kebocoran database tidak membocorkan token yang bisa dipakai.

## D-007 — CSRF: SameSite=Strict + verifikasi Origin + JSON-only

- **Keputusan:** request non-GET wajib `Origin` = `DIETSIS_PUBLIC_ORIGIN` (atau, bila `Origin` absen, `Sec-Fetch-Site: same-origin`) dan `Content-Type: application/json`.
- **Alasan:** pola sesi cookie same-origin; tiga lapis independen tanpa perlu menyimpan token CSRF. Sesuai OWASP CSRF Cheat Sheet (verifikasi origin + SameSite).

## D-008 — ID dibuat klien (UUID) untuk entitas yang bisa dibuat offline

- **Keputusan:** `food_logs.id` dan `custom_foods.id` adalah UUID dari `crypto.randomUUID()`; primary key `(user_id, id)`.
- **Alasan:** pembuatan offline butuh ID final sebelum server terlibat; create yang di-retry menjadi idempoten secara alami. PK komposit membuat ID yang sama milik pengguna lain tidak pernah bersinggungan (anti-IDOR struktural).

## D-009 — Snapshot nutrisi per porsi pada food log

- **Keputusan:** `food_logs.kcal/protein_g/carbs_g/fat_g` adalah nilai **per porsi** saat dicatat; total dihitung = per porsi × `quantity`. API menyertakan `totals` hasil hitung server.
- **Alasan:** mengubah jumlah porsi tidak menimbulkan drift pembulatan; mengubah makanan kustom tidak menulis ulang riwayat.

## D-010 — Aturan pembulatan nutrisi (identik Go & TS)

- Total kalori per entri: `round(kcal × quantity)` → bilangan bulat.
- Total makro per entri: `round(g × quantity × 10) / 10` → 1 desimal.
- Total harian: jumlah total per entri, lalu dibulatkan lagi dengan aturan yang sama (membuang noise floating point).
- `round` = setengah menjauhi nol (`math.Round` Go; `Math.round` JS identik untuk bilangan non-negatif).
- Vektor uji bersama: `server/internal/nutrition/testdata/vectors.json`.

## D-011 — Formula rekomendasi target

Bukan saran medis; ditampilkan sebagai "perkiraan".

1. **BMR (Mifflin-St Jeor):** `10 × berat_kg + 6,25 × tinggi_cm − 5 × usia + s`, dengan `s = +5` (pria) atau `−161` (wanita).
2. **TDEE:** `BMR × faktor aktivitas`: `sedentary` 1,2 · `light` 1,375 · `moderate` 1,55 · `active` 1,725 · `very_active` 1,9.
3. **Target kalori:** selisih = `goal_weight_kg − weight_kg`.
   - selisih ≤ −1 kg → turun: `TDEE − 500` (≈ 0,5 kg/minggu)
   - selisih ≥ +1 kg → naik: `TDEE + 300`
   - lainnya → jaga: `TDEE`
   - dibatasi bawah oleh **batas aman umum** 1.200 kkal (wanita) / 1.500 kkal (pria), lalu dibulatkan ke kelipatan 10 terdekat.
4. **Target protein:** `round(1,6 × min(weight_kg, goal_weight_kg))` gram — memakai berat target bila menurunkan berat agar tidak berlebihan pada berat badan tinggi.
5. **Target air:** `ceil(weight_kg × 30 ml / 250 ml)` gelas, dibatasi 6–16 gelas.
6. Target yang ditandai manual (`*_target_manual = 1`) **tidak pernah** ditimpa rekomendasi. Target non-manual dihitung ulang server setiap profil berubah.
7. Target kalori manual di bawah batas aman diizinkan (min 800) dengan peringatan di UI.

## D-012 — Batas validasi

| Field | Batas |
|---|---|
| `display_name` | 1–60 karakter |
| `age` | 18–100 (Dietsis untuk orang dewasa; formula tidak untuk anak/remaja) |
| `height_cm` | 100–250 |
| `weight_kg`, `goal_weight_kg` | 30–300 |
| `waist_cm` | 30–250 |
| `calorie_target` | 800–6.000 |
| `protein_target_g` | 10–400 |
| `water_target_glasses` | 1–30 |
| food `name` / `serving_label` | 1–80 / 1–40 karakter |
| `serving_grams` | 0,1–5.000 atau null |
| `kcal` per porsi | 0–5.000 |
| makro per porsi | 0–500 g |
| `quantity` | 0,1–50, maks 2 desimal |
| `steps` | 0–200.000 atau null |
| `note` | ≤ 500 karakter |
| delta air per operasi | −20…+20, ≠ 0; hasil dijepit 0–100 gelas |
| tanggal | tanggal kalender valid, 2000-01-01 s.d. (hari ini UTC + 1 hari) |

Teks di-trim, harus UTF-8 valid, tanpa karakter kontrol.

## D-013 — Tanggal = zona waktu lokal perangkat

- **Keputusan:** `date` adalah string `YYYY-MM-DD` hari kalender lokal perangkat; server hanya memvalidasi format dan rentang.
- **Alasan:** "hari ini" pengguna ditentukan jam di tangannya, bukan server. Batas atas UTC+1 hari mengakomodasi zona hingga UTC+14.

## D-014 — Strategi sinkronisasi: outbox operasi + change feed

Lihat [OFFLINE_SYNC.md](OFFLINE_SYNC.md). Ringkas: operasi ber-ID unik di-push berurutan; server mencatat hasil per `op_id` (idempoten); konflik dideteksi via `base_version` dengan aturan per entitas; perubahan ditarik inkremental lewat `change_log` yang terkompaksi (satu baris per entitas).

## D-015 — Konflik: "server terkini menang" untuk penulisan berversi

- **Keputusan:** operasi yang `base_version`-nya basi **tidak diterapkan**; server mengembalikan status `conflict` + state terkini; klien mengadopsi state server dan memberi tahu pengguna.
- **Alasan:** tidak pernah menimpa diam-diam perubahan dari perangkat lain; perilaku dapat dijelaskan dan dites. Konflik nyata jarang (satu pengguna, beberapa perangkat).
- **Pengecualian:** air (counter delta, komutatif), favorit (set per item), create/delete/restore (idempoten) tidak pernah konflik.

## D-016 — "Hapus semua data" memakai epoch

- **Keputusan:** `DELETE /account/data` menghapus seluruh data pengguna (kecuali akun & sesi) dan menaikkan `users.data_epoch`. Push dengan epoch lama ditolak `409 epoch_mismatch`; klien menghapus data lokal dan sinkron ulang dari nol.
- **Alasan:** perangkat lain yang offline tidak boleh "menghidupkan kembali" data yang sudah dihapus.
- **Konsekuensi:** backup lama masih berisi data hingga terhapus oleh retensi (≤ `DIETSIS_BACKUP_KEEP` hari) — dinyatakan di kebijakan privasi/Settings.

## D-017 — Timestamp = epoch milidetik

- **Keputusan:** kolom `*_at` dan field API `*_at` adalah integer Unix epoch ms (UTC) yang ditetapkan server.
- **Alasan:** satu representasi di DB, API, dan JS (`new Date(ms)`); urutan numerik sederhana; tanpa parsing string.

## D-018 — Grafik tanpa library

- **Keputusan:** grafik berat & air adalah SVG tulisan tangan.
- **Alasan:** library chart (mis. Recharts ±100 kB gzip) terlalu mahal untuk dua grafik sederhana di PWA yang mengutamakan ringan.

## D-019 — Export CSV dari server, per dataset

- **Keputusan:** `GET /export?dataset=food_logs|weights|days|custom_foods|profile` menghasilkan satu CSV per dataset (UTF-8 dengan BOM agar Excel membaca benar). Export memerlukan koneksi.
- **Alasan:** CSV tabel tunggal yang jarang (sparse) sulit dipakai; server memegang data lengkap. Sel yang diawali `= + - @` diberi prefiks `'` (mitigasi CSV injection).

## D-020 — Data lokal per pengguna & logout

- **Keputusan:** IndexedDB `dietsis-<userId>`. Logout dengan outbox kosong menghapus database lokal. Bila ada operasi tertunda, pengguna diperingatkan dan dapat memilih sinkron dulu atau tetap keluar (menghapus perubahan tertunda).
- **Alasan:** perangkat bersama tidak boleh menyisakan data kesehatan; data dua akun tidak pernah tercampur.

## D-024 — Baseline browser: tanpa API ES2024

- **Keputusan:** kode klien tidak memakai `Promise.withResolvers` dan API sejenis; pembungkus IndexedDB memakai executor `new Promise`.
- **Alasan:** `withResolvers` butuh Safari 17.4+; iPhone lama harus tetap bisa membuka database lokalnya (PWA offline-first).

## D-025 — Entitas lokal menyimpan `server` dan `value`

- **Keputusan:** setiap record IndexedDB menyimpan state server terakhir dan nilai tampilan; nilai tampilan selalu = state server + op outbox yang belum terkirim (di-*rebase* saat ack/pull).
- **Alasan:** satu algoritma untuk ack, konflik, penolakan, dan pull; penolakan/konflik cukup membuang op dan menghitung ulang, tanpa menebak.

## D-021 — Mesin operasi tunggal untuk REST dan sync

- **Keputusan:** paket `internal/sync` adalah satu-satunya jalur tulis. Endpoint REST membangun `Op` dan memanggil `Engine.Apply`; `/sync/push` memanggilnya per operasi. Setiap operasi berjalan dalam satu transaksi `BEGIN IMMEDIATE`: cek epoch → cek ledger → terapkan → `change_log` → simpan hasil ledger → commit.
- **Alasan:** aturan idempotensi, versi, konflik, dan log perubahan tidak boleh terduplikasi di dua tempat. Efek dan catatan ledger di-commit atomik, sehingga retry setelah respons hilang tidak pernah menerapkan dua kali.
- **Konsekuensi:** hasil `conflict`/`rejected` juga dicatat di ledger agar retry deterministik; entri ledger dipangkas setelah 90 hari.

## D-022 — Kunci idempotensi terikat pada jenis operasi

- **Keputusan:** satu `op_id` hanya valid untuk satu `type` per pengguna; pemakaian ulang untuk jenis lain ditolak (`bad_request`).
- **Alasan:** tanpa ini, kunci yang dipakai ulang secara tidak sengaja akan "menjawab" dengan hasil operasi yang berbeda.

## D-023 — `days.version` hanya untuk langkah/catatan

- **Keputusan:** air adalah counter komutatif di luar versioning; baris yang dibuat oleh air berversi 0.
- **Alasan:** lihat DATABASE.md; mencegah konflik palsu antara air dan pembaruan langkah.
