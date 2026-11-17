# Dietsis

**Teman diet harianmu.** Personal nutrition tracker — catat makanan, nutrisi, air, dan berat badan. PWA ringan, mobile-first, offline-first.

- Frontend: React 19 · TypeScript · Vite · Tailwind CSS 4 · React Router · PWA · IndexedDB
- Backend: Go (stdlib `net/http`) · SQLite (WAL, `modernc.org/sqlite`) · Google OAuth · sesi cookie
- Infrastruktur: Nginx · systemd · Ubuntu VPS · backup otomatis

Produksi: https://dietsis.transbara.my.id

## Prasyarat

- Node.js ≥ 22 dan npm
- Go ≥ 1.27
- (opsional) `sqlite3` CLI untuk inspeksi database

## Setup

```bash
git clone https://github.com/MonkeyDevBara/dietsis.git
cd dietsis
npm install
cp .env.example .env   # isi VITE_GOOGLE_CLIENT_ID / DIETSIS_GOOGLE_CLIENT_ID bila menguji login
```

## Development

```bash
npm run server:dev   # API Go di http://127.0.0.1:8787
npm run dev          # Frontend di http://localhost:5178 (proxy /api → :8787)
```

Backend membaca konfigurasi dari environment variable `DIETSIS_*` (lihat `.env.example`), misalnya:

```bash
set -a; source .env; set +a; npm run server:dev
```

## Test, lint, build

| Perintah | Fungsi |
|---|---|
| `npm run check` | Semua: lint + typecheck + test frontend, `go vet` + `gofmt` + test backend |
| `npm run lint` / `npm run typecheck` / `npm test` | Frontend |
| `npm run server:lint` / `npm run server:test` | Backend |
| `npm run build` | Build frontend produksi → `dist/` |
| `npm run server:build` | Binary Linux statis → `dist-server/dietsis-server` (`GOARCH=arm64` untuk VPS ARM) |

## Dokumentasi

| Dokumen | Isi |
|---|---|
| [docs/BRIEF.md](docs/BRIEF.md) | Produk, cakupan, batasan kesehatan |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Arsitektur, struktur folder, konfigurasi |
| [docs/DECISIONS.md](docs/DECISIONS.md) | Keputusan arsitektur & formula |
| [docs/DESIGN.md](docs/DESIGN.md) | Design system & UX |
| [docs/API.md](docs/API.md) | Kontrak REST API `/api/v1` |
| [docs/DATABASE.md](docs/DATABASE.md) | Skema SQLite, migrasi, backup |
| [docs/OFFLINE_SYNC.md](docs/OFFLINE_SYNC.md) | Offline-first, sinkronisasi, konflik |
| [docs/SECURITY.md](docs/SECURITY.md) | Model ancaman & kontrol keamanan |
| [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) | Deploy VPS, Nginx, systemd, backup/restore |
| [docs/PM_STATUS.md](docs/PM_STATUS.md) | Status milestone |

## Disclaimer

Angka rekomendasi Dietsis adalah estimasi umum, bukan saran medis. Konsultasikan dengan tenaga kesehatan untuk kondisi khusus.
