# PM Status

**Diperbarui:** 2026-11-17 · **Branch kerja:** `develop` · **Milestone aktif:** M3 selesai → M4 berikutnya

## Ringkasan milestone

| # | Milestone | Status |
|---|---|---|
| M0 | Project foundation | ✅ Selesai |
| M1 | UI/UX | ✅ Selesai |
| M2 | Database & backend | ✅ Selesai |
| M3 | Authentication | ✅ Selesai |
| M4 | Business API | ⏳ Berikutnya |
| M5 | Frontend integration | ☐ |
| M6 | Offline engine | ☐ |
| M7 | QA & security | ☐ |
| M8 | Release preparation | ☐ |
| M9 | GitHub release | ☐ |

## Rencana implementasi

| # | Cakupan | Keluaran yang dapat diverifikasi |
|---|---|---|
| M1 | Komponen design system (DESIGN.md), app shell + bottom nav/side rail, PWA (manifest, ikon, service worker prompt), halaman Login, Onboarding, Dashboard, Diary + sheet makanan & makanan kustom, Progres (berat, grafik SVG, riwayat air), Profil, Settings; katalog makanan Indonesia (`src/data`); `lib/nutrition` (formula D-010/D-011) + vektor uji | UI berjalan di mobile/tablet/desktop memakai repository lokal in-memory; test komponen & nutrisi lulus |
| M2 | `store`: buka SQLite (pragma), runner migrasi (embed, checksum), `schema_migrations`; middleware (recover, request id, security headers, body limit, logging, client IP); health cek DB; `migrate` subcommand | Test store & middleware; `EXPLAIN QUERY PLAN` memakai index |
| M3 | `auth`: verifikasi Google ID token (JWKS, RS256) dengan test negatif; sesi (hash, sliding, revokasi); middleware auth & CSRF; `limit` rate limiter; `/auth/google`, `/auth/me`, `/auth/logout` | Test JWT/sesi/CSRF/rate limit |
| M4 | `validate`, `nutrition`, `sync.Engine` (ledger, versi, change log); endpoint profile, favorites, days, water, food logs, custom foods, weights, sync push/pull, export, delete data; job retensi | Test handler, otorisasi lintas user, idempotensi, konflik, epoch |
| M5 | API client, AuthContext (GIS), hook data, error/loading state; ganti repository in-memory dengan API | Login nyata end-to-end di lokal |
| M6 | IndexedDB local repo (`idb`), outbox + koalesensi, sync engine (backoff, Web Locks, rebase), status UI, logout guard, epoch reset | Test IndexedDB/queue/sync dengan `fake-indexeddb`; skenario OFFLINE_SYNC |
| M7 | Integration test (server nyata + klien sync), security checklist, backup/restore test, regression, build produksi, bundle audit | Semua test lulus; checklist SECURITY tercentang |
| M8 | `server/deploy/`: nginx conf, unit systemd, timer backup, skrip provision/deploy; panduan final | Konfigurasi lolos `nginx -t`/`systemd-analyze verify` (di VM/container lokal) |
| M9 | Review, commit bertahap, push, release branch, merge ke `main` setelah QA disetujui | PR/release; **deploy ke VPS hanya dengan persetujuan** |

## M0 — Project foundation ✅

Dikerjakan:
- Branch `develop` dari `main`.
- Frontend: Vite 8 + React 19 + TypeScript 6.0 (strict) + Tailwind CSS 4 + React Router; ESLint (typescript-eslint strict type-checked + react-hooks); Vitest.
- Design token (`src/index.css`): warna semantik terang/gelap, warna nutrisi, radius, elevasi, gerak, safe area; dokumentasi di `docs/DESIGN.md`.
- `src/lib/date.ts` (kunci tanggal lokal) + test.
- Backend: module Go `github.com/MonkeyDevBara/dietsis/server`; `internal/config` (env + validasi produksi) + test; `internal/httpx` (envelope JSON, router stdlib, `GET /api/v1/health`) + test; `main.go` (subcommand, slog, timeout server, graceful shutdown).
- Skema SQLite final `0001_init.sql` (STRICT, FK, index, ledger idempotensi, change log terkompaksi) divalidasi dengan `sqlite3` 3.51: FK cascade, CHECK, PK per tanggal, `EXPLAIN QUERY PLAN` memakai index, pola increment air atomik.
- Dokumentasi: BRIEF, ARCHITECTURE, DECISIONS (D-001…D-020), API, DATABASE, OFFLINE_SYNC, SECURITY, DEPLOYMENT (rancangan), DESIGN, PM_STATUS; README.

Verifikasi: lihat bagian "Verifikasi terakhir".

Penyimpangan dari rencana awal (disengaja):
- Config + server HTTP minimal + health (rencana M2) dibuat di M0 agar `main.go` nyata dan dapat dites; M2 menambah DB, middleware, dan health DB.
- Paket tambahan `internal/nutrition` (alasan di ARCHITECTURE.md).
- TypeScript 6.0 bukan 7 (D-004).

## M1 — UI/UX ✅

Dikerjakan:
- Komponen design system (`src/components/ui`): Button/IconButton, TextField/NumberField/TextareaField, SegmentedControl, Stepper, ProgressRing/ProgressBar, Sheet (modal dengan fokus terjebak, Esc, kembalikan fokus), ConfirmDialog (ketik `HAPUS`), Card/EmptyState/Skeleton, toast dengan aksi Urungkan.
- App shell responsif: bottom nav (< 1024 px) dan side rail (≥ 1024 px); route lazy-load; guard login dan profil.
- Halaman: Login, Onboarding, Beranda, Diary, Progres (grafik berat SVG, riwayat berat, air 7 hari), Profil, Pengaturan, 404.
- Food diary: 4 waktu makan, cari (alias, abaikan huruf besar/tanda baca), terakhir, favorit, makanan kustom, jumlah porsi, tambah cepat, ubah, hapus + Urungkan. Katalog 79 makanan Indonesia (`src/data/foods.ts`).
- Air: tambah/kurang, riwayat 7 hari. Berat: satu entri per tanggal, pinggang opsional. Aktivitas: langkah + catatan.
- Profil: rekomendasi target (D-011) hidup di form; target yang diedit menjadi manual dengan tombol "Pakai saran"; peringatan di bawah batas aman.
- `lib/nutrition` (TS) + `internal/nutrition` (Go) diuji dengan **vektor bersama** `server/internal/nutrition/testdata/vectors.json`; validasi D-012 di `lib/validation`.
- Lapisan data lokal: `LocalState` + reducer murni `applyOp` dengan bentuk operasi sama seperti protokol sync; `Repository` (implementasi memori) + selector; semua mutasi lewat `actions` (validasi di satu tempat).
- PWA: manifest, ikon (192/512/maskable/apple-touch), service worker Workbox (hanya app shell, `/api` tidak di-cache), update lewat prompt.
- Export CSV (preview, dari data lokal) dengan mitigasi CSV injection.

Batasan yang disengaja di M1 (dikerjakan milestone berikutnya):
- **Login adalah mode pratinjau** (tanpa Google/server) dan **data hanya di memori**: hilang saat reload. Diganti Google OAuth di M3/M5 dan IndexedDB di M6.
- Status sinkronisasi hanya menampilkan "Pratinjau"/"Offline"; mesin sync di M6.
- Export CSV memakai data lokal; versi server (`GET /export`, D-019) di M4.

Penyimpangan: warna protein diganti ke magenta (`#c0357a`) karena merah awal terlalu mirip warna bahaya (terlihat di uji browser).

## M2 — Database & backend ✅

Dikerjakan:
- `internal/store`: dua pool atas satu file WAL. **Write**: 1 koneksi, semua transaksi `BEGIN IMMEDIATE` (`_txlock=immediate`), `WithTx` helper. **Read**: 4 koneksi, `query_only`. Pragma per koneksi sesuai DATABASE.md; direktori data `0700`, file DB/WAL/SHM `0600`.
- Runner migrasi (`//go:embed`): maju saja, satu transaksi per migrasi bersama baris `schema_migrations`, checksum dibandingkan setiap start (migrasi yang sudah dirilis tidak boleh berubah), menolak database yang lebih baru dari binary, nomor harus berurutan tanpa celah, `foreign_key_check` setelahnya; `CheckIntegrity` untuk backup (M8).
- Middleware `httpx`: recover (500 JSON generik, detail hanya di log), request id (selalu dibuat server), client IP (`X-Forwarded-For` hanya dari proxy tepercaya, dibaca dari kanan), security headers, access log (pola route, tanpa query/header/body), `limitBody` per route (default 64 KiB).
- `GET /health` memeriksa kedua pool (503 `unavailable` tanpa membocorkan detail).
- `dietsis-server migrate`; `serve` menerapkan migrasi sebelum listen dan gagal jelas bila checksum berubah.

Temuan penting dari test: pernyataan increment air di DATABASE.md memakai `excluded.water_glasses` di `DO UPDATE`; nilai itu sudah dijepit ≥ 0 oleh bagian INSERT sehingga **pengurangan air diam-diam hilang**. Diperbaiki memakai parameter delta mentah (`?3`) dan dokumen dikoreksi; ada test batas 0/100 dan test 80 increment paralel (hasil tepat 80).

Belum di M2 (sesuai rencana): rate limit, CSRF, auth middleware (M3); decoder JSON ketat dan job retensi (M4); backup (M8).

## M3 — Authentication ✅

Dikerjakan:
- `internal/auth/google.go`: verifikasi ID token Google dengan stdlib. Algoritma **dikunci RS256** (tolak `none`, HS256 key-confusion, RS384), `kid` wajib, tanda tangan diverifikasi **sebelum** klaim dibaca, lalu `iss`, `aud`, `azp`, `exp` (tepat di `exp` = kedaluwarsa), `iat`/`nbf` (toleransi 60 dtk), `sub`, `email`, `email_verified`. Client ID kosong tidak pernah cocok dengan `aud` kosong.
- `internal/auth/jwks.go`: cache JWKS (ikut `Cache-Control`), refetch ≤ 1×/menit termasuk untuk `kid` tak dikenal, kunci lama tetap dipakai saat Google gagal, hanya RSA ≥ 2048 bit.
- `internal/auth/session.go`: token acak 32 byte, **hanya SHA-256 yang disimpan**, cookie `__Host-dietsis_session` (`HttpOnly`, `Secure`, `SameSite=Strict`, tanpa Domain; di dev tanpa HTTPS memakai `dietsis_session`), sliding renewal setelah separuh TTL, revokasi per sesi dan semua perangkat, token baru di setiap login.
- `internal/limit`: token bucket per kunci, memori terbatas (100.000 kunci + bucket overflow), sapu kunci idle.
- `internal/httpx`: decoder JSON ketat (413/415/400), CSRF guard (Origin / `Sec-Fetch-Site` + JSON-only), middleware autentikasi (identitas hanya dari sesi; `httpx.CurrentUser`), rate limit login/IP/user/gagal-auth, endpoint `POST /auth/google`, `GET /auth/me`, `POST /auth/logout`.
- `internal/store`: `UpsertGoogleUser` (identitas = `sub`), `UserHasProfile`, CRUD sesi, purge sesi kedaluwarsa (loop tiap jam di `serve`).
- Tanpa `DIETSIS_GOOGLE_CLIENT_ID`, server tetap jalan dan `POST /auth/google` menjawab 503 (dev).

Belum diuji dengan Google sungguhan: login end-to-end butuh Client ID (M5). Yang sudah diverifikasi dengan data nyata: JWKS publik Google berhasil diunduh dan di-parse (2 kunci RSA).

## Verifikasi terakhir (M3)

| Perintah | Hasil |
|---|---|
| `npm run check` | lulus: 57 test frontend; 159 test/subtest Go (auth 40+ kasus negatif JWT, sesi, CSRF, rate limit, otorisasi antar-user) |
| Live (binary, DB sementara) | tanpa client id → 503; token palsu → 401 generik; CSRF origin asing/tanpa origin → 403; form → 415; logout idempoten 204 + cookie dihapus; login ke-6 dalam burst → 429 + `Retry-After` |
| JWKS Google asli | diunduh dan di-parse (2 kunci, max-age ±5 jam) |

## Verifikasi M2

| Perintah | Hasil |
|---|---|
| `npm run check` | lulus: 57 test frontend; Go: config, httpx, nutrition, store |
| Live (binary lokal, DB sementara) | `migrate` dua kali idempoten; `serve` → health 200 dengan semua header; log akses tanpa data sensitif; file `0600`/dir `0700`; `sqlite3` membaca saat server jalan (WAL); `integrity_check` ok; checksum dirusak → server menolak start (exit 1) |

## Verifikasi M1

| Perintah | Hasil |
|---|---|
| `npm run lint` / `npm run typecheck` | lulus |
| `npm test` | 57 test lulus (nutrisi + vektor, validasi, pencarian, reducer/selector, CSV, Sheet a11y, alur aplikasi: login → onboarding → log makanan/air → hapus data) |
| `npm run server:lint` / `npm run server:test` | lulus (config, httpx, nutrition) |
| `npm run build` | lulus; 28 entri precache (±453 KiB), JS utama 87 kB gzip |
| Browser (WebKit headless, 390×844 dan 1280×800, terang/gelap) | onboarding, beranda, picker makanan, diary, progres, side rail tampil benar; 0 error konsol |

## Keputusan terbuka / butuh input pemilik

| Item | Dibutuhkan kapan | Default bila tidak ada input |
|---|---|---|
| Google OAuth Web Client ID (origin: domain produksi + `http://localhost:5178`) | M3/M5 untuk uji login nyata | Test memakai kunci RSA lokal & JWKS palsu; login nyata menunggu client ID |
| Arsitektur CPU & versi Ubuntu VPS | M8 | `amd64`, Ubuntu 24.04 |
| Persetujuan akses VPS | Setelah M8 | Tidak ada akses |

## Risiko

| Risiko | Mitigasi |
|---|---|
| Bug verifikasi JWT buatan sendiri | Test negatif menyeluruh (SECURITY checklist), kode kecil & terisolasi |
| Konflik sinkronisasi membingungkan pengguna | Aturan tunggal per entitas, toast eksplisit, skenario uji multi-perangkat |
| Safari/iOS menghapus IndexedDB PWA yang tidak dipakai lama | Data aman di server setelah sinkron; status "tertunda" mendorong sinkron; `navigator.storage.persist()` diminta |
| Kehilangan VPS | Backup harian + rekomendasi salinan off-site terenkripsi |

## Catatan repository

- Kode lama (versi Firebase) sudah dihapus dari working tree oleh pemilik sebelum M0; penghapusan itu ikut di commit M0 agar tree `develop` konsisten (tidak ada file lama yang mengimpor Firebase). Kode lama tetap tersedia di riwayat `main` (`869a929`).
- `.env` lokal masih berisi variabel Firebase lama (tidak dipakai lagi, di-`.gitignore`); variabel baru ada di `.env.example`.

## Definition of Done

- [ ] Seluruh fitur inti berfungsi
- [ ] Frontend React berjalan · [ ] Backend Go berjalan · [ ] SQLite berjalan
- [ ] Google OAuth berfungsi · [ ] Sesi aman · [ ] Data terisolasi per pengguna
- [ ] Offline-first berjalan · [ ] Sinkronisasi berfungsi · [ ] Konflik ditangani
- [ ] Export CSV · [ ] Hapus data · [ ] Backup otomatis
- [ ] Test penting lulus · [ ] Build produksi berhasil
- [ ] Dokumentasi lengkap · [ ] Deployment siap dijalankan
