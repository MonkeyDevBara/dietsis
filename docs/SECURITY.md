# Keamanan

## Aset & ancaman

| Aset | Ancaman utama |
|---|---|
| Data kesehatan pengguna (berat, makanan, profil) | Akses oleh pengguna lain (IDOR), kebocoran database/backup, perangkat bersama |
| Sesi | Pencurian token (XSS, jaringan), CSRF, fixation |
| Akun | Pemalsuan ID token Google, replay token |
| Ketersediaan VPS | Brute force/flood endpoint, body besar, disk penuh |

## Kontrol

### Autentikasi (Google)

1. Frontend memuat Google Identity Services, menerima **ID token** (JWT) lewat callback.
2. `POST /auth/google {credential}` → backend memverifikasi:
   - header `alg` **harus** `RS256` (menolak `none`/HS*), `kid` ada di JWKS Google (`https://www.googleapis.com/oauth2/v3/certs`, di-cache sesuai `Cache-Control`, refresh saat `kid` tak dikenal maks 1×/menit);
   - tanda tangan RSA valid;
   - `iss` ∈ {`accounts.google.com`, `https://accounts.google.com`};
   - `aud` = `DIETSIS_GOOGLE_CLIENT_ID` (dan `azp` bila ada);
   - `exp` > sekarang, `iat` ≤ sekarang + 60 s (toleransi jam 60 s);
   - `sub` tidak kosong, `email_verified = true`.
3. Upsert user berdasar `sub` → buat sesi baru (sesi lama tidak dipakai ulang → tidak ada session fixation).
4. Alasan penolakan detail hanya di log server; klien menerima `401` generik. Kegagalan mengambil kunci Google (jaringan/JWKS rusak) menghasilkan `503`, bukan `401`. Bila Google tidak terjangkau, kunci yang sudah di-cache tetap dipakai.
5. JWKS: hanya kunci RSA ≥ 2048 bit dengan `use=sig`; refetch maksimal 1×/menit, termasuk saat `kid` tak dikenal (mencegah serangan `kid` acak menjadi flood ke Google).

Risiko sisa: ID token curian dapat dipakai login sampai `exp` (≤ 1 jam). Mitigasi: token hanya diterbitkan ke origin terdaftar dan dikirim via HTTPS; CSP membatasi skrip pihak ketiga.

### Sesi

| Properti | Nilai |
|---|---|
| Token | 32 byte `crypto/rand`, base64url |
| Penyimpanan server | SHA-256 token (kebocoran DB tidak menghasilkan token yang bisa dipakai) |
| Cookie | `__Host-dietsis_session` · `HttpOnly` · `Secure` · `SameSite=Strict` · `Path=/` · tanpa `Domain` (dev non-HTTPS: `dietsis_session`, tanpa `Secure`) |
| Masa berlaku | Sliding 30 hari (`DIETSIS_SESSION_TTL`); diperpanjang bila sudah lewat separuh TTL |
| Revokasi | Logout menghapus baris sesi; `all_devices` menghapus semua sesi pengguna |
| Pembersihan | Sesi kedaluwarsa dihapus harian |

### Otorisasi & IDOR

- `user_id` hanya berasal dari sesi yang diverifikasi middleware; tidak pernah dari body/query/path.
- Setiap query menyertakan `WHERE user_id = ?`; primary key tabel milik pengguna diawali `user_id`.
- Resource milik pengguna lain → `404` (tidak membocorkan keberadaan).
- Test otorisasi: pengguna B mencoba membaca/mengubah/menghapus/restore ID milik A melalui REST dan `/sync/push` → selalu `404`/`rejected`, data A tidak berubah.

### CSRF

Sesi berbasis cookie → CSRF dicegah berlapis ([D-007](DECISIONS.md#d-007--csrf-samesitestrict--verifikasi-origin--json-only)):
1. `SameSite=Strict`.
2. Request non-GET wajib `Origin` = `DIETSIS_PUBLIC_ORIGIN`; tanpa `Origin` → wajib `Sec-Fetch-Site: same-origin`; lainnya `403`.
3. Body wajib `Content-Type: application/json` (form HTML lintas situs tidak dapat mengirimnya tanpa preflight CORS, yang tidak pernah diizinkan).
4. GET tidak pernah mengubah state.

Berlaku juga untuk `POST /auth/google` (mencegah login CSRF).

### CORS

Tidak ada header CORS sama sekali — API hanya untuk origin sendiri. Preflight lintas origin gagal secara default.

### Validasi input & SQL injection

- Decoder JSON ketat (`DisallowUnknownFields`, satu objek, batas ukuran).
- Batas domain terpusat di `internal/validate` ([D-012](DECISIONS.md#d-012--batas-validasi)); teks harus UTF-8 valid tanpa karakter kontrol.
- Semua SQL memakai parameter binding (`?`); tidak ada string concatenation untuk nilai. Nama kolom/tabel dinamis tidak ada.
- `STRICT` tables + `CHECK` sebagai pertahanan lapis kedua.

### Rate limiting

Token bucket in-memory (`internal/limit`), IP klien dari `X-Forwarded-For` **hanya** bila koneksi berasal dari `DIETSIS_TRUSTED_PROXIES` (Nginx loopback).

| Ruang | Kunci | Batas |
|---|---|---|
| `POST /auth/google` | IP | 10/menit, burst 5 |
| Endpoint terautentikasi | user | 300/menit, burst 60 |
| `POST /sync/push` | user | 60/menit, burst 10 |
| Lainnya tanpa sesi (health, logout) | IP | 60/menit, burst 20 |
| Sesi tidak valid pada endpoint terautentikasi | IP | 30/menit, burst 10 (mempersulit tebak token) |

Identifikasi IP: peer TCP; `X-Forwarded-For` hanya dipakai bila peer ada di `DIETSIS_TRUSTED_PROXIES`, dibaca dari kanan sehingga entri yang disisipkan klien diabaikan. Tabel limiter dibatasi 100.000 kunci (kunci baru berbagi satu bucket bila penuh) agar spoofing kunci tidak menghabiskan memori.

Melebihi → `429` + `Retry-After`. Nginx juga membatasi koneksi (`limit_req` sebagai lapis luar).

### Batas ukuran & timeout

- Body JSON 64 KiB (sync push 1 MiB) via `http.MaxBytesReader`; Nginx `client_max_body_size 1m`.
- Header maks 16 KiB; `ReadHeaderTimeout` 5 s, `ReadTimeout` 15 s, `WriteTimeout` 30 s, `IdleTimeout` 60 s.
- Batas jumlah: 200 operasi per push, 500 favorit, rentang query 366 hari, `limit` maks per endpoint.

### Header keamanan

API (Go, setiap response): `X-Content-Type-Options: nosniff`, `Cache-Control: no-store`, `Referrer-Policy: same-origin`, `X-Frame-Options: DENY`, `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'`.

Aset statis (Nginx):

```text
Strict-Transport-Security: max-age=31536000; includeSubDomains
Content-Security-Policy: default-src 'self'; script-src 'self' https://accounts.google.com/gsi/client;
  style-src 'self' 'unsafe-inline' https://accounts.google.com/gsi/style; frame-src https://accounts.google.com/gsi/;
  connect-src 'self' https://accounts.google.com/gsi/; img-src 'self' data: https://*.googleusercontent.com;
  font-src 'self'; manifest-src 'self'; worker-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'
Cross-Origin-Opener-Policy: same-origin-allow-popups
X-Content-Type-Options: nosniff
Referrer-Policy: strict-origin-when-cross-origin
Permissions-Policy: camera=(), microphone=(), geolocation=(), payment=()
```

CSP mengikuti kebutuhan resmi Google Identity Services; `same-origin-allow-popups` diperlukan untuk popup login Google.

### Penanganan error & logging

- Panic ditangkap middleware → `500 internal` generik + `request_id`.
- Log terstruktur (`slog` JSON di produksi): method, path pola, status, durasi, `request_id`, `user_id`. **Tidak pernah** mencatat token, cookie, ID token, body request, atau email.

### Secret & konfigurasi

- Tidak ada secret di repository; `.env*` di-`.gitignore` (kecuali `.env.example` tanpa nilai).
- Produksi: `/etc/dietsis/dietsis.env`, `root:dietsis 0640`, dibaca systemd `EnvironmentFile`.
- Google OAuth client ID bukan secret, namun tetap dari environment agar mudah dirotasi. Alur ID token tidak memerlukan client secret.

### Data at rest & backup

- Database `/var/lib/dietsis` dan backup `/var/backups/dietsis` milik user `dietsis`, mode `0700`/`0600`.
- Backup terkena retensi `DIETSIS_BACKUP_KEEP`; data yang dihapus pengguna hilang dari backup setelah periode itu (diinformasikan di Settings).
- Rekomendasi: salin backup off-site terenkripsi (mis. `age`/`gpg`) — prosedur di DEPLOYMENT.

### Sisi klien

- Tidak ada `dangerouslySetInnerHTML`; React meng-escape output.
- Data kesehatan di IndexedDB pengguna; dihapus saat logout (D-020).
- Export CSV: sel diawali `= + - @ \t \r` diberi prefiks `'` (CSV injection).

### Hardening proses (systemd)

User non-root `dietsis`, `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`, `PrivateDevices`, `ReadWritePaths` hanya direktori data & backup, `CapabilityBoundingSet=` kosong, `RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX`, `SystemCallFilter=@system-service`, `MemoryMax`. Detail di `server/deploy/` (M8).

## Checklist uji keamanan (M7)

Semua butir dibuktikan test otomatis (Go) kecuali dinyatakan lain.

- [x] JWT: alg none/HS256-confusion/RS384, `kid` tak dikenal, tanda tangan salah, payload diubah, iss/aud/azp salah, kedaluwarsa, `iat` masa depan, email belum terverifikasi → ditolak (`auth/google_test.go`, 30+ kasus).
- [x] Sesi: flag cookie benar; logout dan logout-semua mencabut di sisi server; sesi kedaluwarsa ditolak; hanya hash token di DB; token baru tiap login (`auth/session_test.go`, `httpx/auth_test.go`).
- [x] CSRF: Origin asing / `null` / tanpa Origin + `Sec-Fetch-Site` lain / content-type form → 403/415, termasuk login dan hapus-semua-data (`httpx/auth_test.go`, `httpx/api_test.go`).
- [x] IDOR: pengguna lain mengubah/menghapus/restore/membaca id orang lain lewat REST, sync push, pull, dan export → 404/state null, data korban utuh (`httpx/api_test.go`, `sync/engine_test.go`).
- [x] Setiap route bisnis tanpa sesi → 401; `user_id` di header/cookie/body tidak dipercaya (400/401).
- [x] Rate limit → 429 + `Retry-After`; `X-Forwarded-For` dari peer tak tepercaya diabaikan; memori limiter terbatas (`limit`, `httpx`). Nginx diuji langsung: spoof XFF ditimpa, burst → 429.
- [x] Body terlalu besar → 413 (64 KiB; push 1 MiB; Nginx 1 MiB); field tak dikenal → 400.
- [x] Error 500/503 tidak membocorkan detail internal (panic, DB mati, health).
- [x] CSV injection dimitigasi (`export/export_test.go`).
- [x] Backup/restore: backup korup/bukan gzip/bukan DB Dietsis ditolak tanpa menyentuh DB aktif; restore menolak bila server listen (`backup/backup_test.go` + latihan dengan binary asli).
- [x] `govulncheck ./...` bersih. `npm audit --omit=dev` = 0 kerentanan. `npm audit` penuh: 4 *moderate* di tooling build (`workbox-build → glob → minimatch`, DoS lewat pola glob), tidak ikut ke produksi dan hanya memproses pola glob tetap dari konfigurasi kita; dibiarkan agar tidak menurunkan versi `workbox-build`.
- [ ] Belum diuji: login Google asli end-to-end (butuh Client ID); header Nginx dari Ubuntu (konfigurasi sama telah diuji di macOS); opsi hardening systemd.
