# Deployment

Target: satu VPS Ubuntu, `https://dietsis.transbara.my.id`. Nginx menyajikan SPA dan mem-proxy `/api/` ke binary Go (`127.0.0.1:8787`), SQLite WAL di `/var/lib/dietsis`, backup harian otomatis.

> **Status verifikasi.** Sudah diuji di MacBook: konfigurasi Nginx (`nginx -t` + dijalankan nyata di depan server Go dan build SPA: header, cache, fallback SPA, proxy, spoofing `X-Forwarded-For`, rate limit, 413, CSRF), paket rilis (binary Linux statis arm64/amd64), backup/restore dengan binary asli. **Belum dijalankan di Ubuntu**: unit systemd (opsi hardening) dan `install.sh`. Jalankan langkah 1–6 sambil memantau `journalctl`; bila satu opsi hardening menolak start, hapus opsi itu dan beri tahu saya.
>
> **Tidak ada perubahan pada VPS dari pihak saya.** Semua langkah di bawah dijalankan oleh pemilik.

## Topologi dan lokasi

| Item | Nilai |
|---|---|
| Binary | `/opt/dietsis/bin/dietsis-server` |
| Static | `/var/www/dietsis/releases/<versi>/`, symlink `current` (rollback = ganti symlink) |
| Data | `/var/lib/dietsis/dietsis.db` (`dietsis:dietsis`, 0700; file 0600) |
| Backup | `/var/backups/dietsis/` (0700) |
| Config | `/etc/dietsis/dietsis.env` (`root:dietsis`, 0640) |
| Service | `dietsis.service`; backup: `dietsis-backup.service` + `.timer` (02:30 + acak ≤ 10 mnt) |
| User | `dietsis` (sistem, tanpa shell) |

## Prasyarat (sekali)

1. **DNS**: A/AAAA `dietsis.transbara.my.id` → IP VPS.
2. **Paket**: `sudo apt install nginx certbot` (tidak perlu Go/Node/SQLite di VPS).
3. **Firewall**: `sudo ufw allow OpenSSH && sudo ufw allow 'Nginx Full' && sudo ufw enable`. API hanya listen di loopback.
4. **Google OAuth** (Cloud Console → Credentials → OAuth client ID → Web application): Authorized JavaScript origins = `https://dietsis.transbara.my.id` (tambah `http://localhost:5178` untuk dev). Tanpa redirect URI dan tanpa client secret.

## Build rilis (di MacBook)

```bash
VITE_GOOGLE_CLIENT_ID=<id>.apps.googleusercontent.com GOARCH=amd64 npm run release
# GOARCH=arm64 bila VPS ARM (cek: uname -m → aarch64)
```

Menjalankan `npm run check`, build SPA (tanpa source map), binary Linux statis dengan versi dari `git describe`, lalu `release/dietsis-<versi>-linux-<arch>.tar.gz` + `.sha256`. Build ditolak bila `VITE_GOOGLE_CLIENT_ID` kosong (hasilnya akan berupa mode pratinjau tanpa login dan sync); `ALLOW_PREVIEW=1` untuk memaksa.

## Instalasi pertama

```bash
# 1. Kirim dan buka
scp release/dietsis-*-linux-amd64.tar.gz* user@vps:/tmp/
ssh user@vps
cd /tmp && sha256sum -c dietsis-*.tar.gz.sha256 && tar xzf dietsis-*-linux-amd64.tar.gz && cd dietsis-*-linux-amd64

# 2. Provisioning (user, direktori, unit systemd, timer backup, binary, static)
sudo ./deploy/install.sh
#    Skrip membuat /etc/dietsis/dietsis.env dari contoh dan berhenti sebelum start bila client id kosong.

# 3. Isi konfigurasi
sudo nano /etc/dietsis/dietsis.env          # DIETSIS_GOOGLE_CLIENT_ID=<id>.apps.googleusercontent.com
sudo systemctl restart dietsis
curl -fsS http://127.0.0.1:8787/api/v1/health   # {"data":{"status":"ok",...}}

# 4. Sertifikat TLS (konfigurasi Nginx final butuh sertifikat, jadi mulai dari konfigurasi HTTP saja)
sudo tee /etc/nginx/sites-available/dietsis >/dev/null <<'CONF'
server {
    listen 80;
    server_name dietsis.transbara.my.id;
    location /.well-known/acme-challenge/ { root /var/www/certbot; }
    location / { return 404; }
}
CONF
sudo ln -sf /etc/nginx/sites-available/dietsis /etc/nginx/sites-enabled/dietsis
sudo rm -f /etc/nginx/sites-enabled/default
sudo nginx -t && sudo systemctl reload nginx
sudo certbot certonly --webroot -w /var/www/certbot -d dietsis.transbara.my.id
printf '#!/bin/sh\nsystemctl reload nginx\n' | sudo tee /etc/letsencrypt/renewal-hooks/deploy/reload-nginx.sh >/dev/null
sudo chmod +x /etc/letsencrypt/renewal-hooks/deploy/reload-nginx.sh

# 5. Konfigurasi Nginx final
sudo cp deploy/nginx-headers.conf /etc/nginx/snippets/dietsis-headers.conf
sudo cp deploy/nginx.conf /etc/nginx/sites-available/dietsis
sudo nginx -t && sudo systemctl reload nginx

# 6. Uji asap
curl -fsS https://dietsis.transbara.my.id/api/v1/health
systemctl list-timers dietsis-backup.timer
sudo systemctl start dietsis-backup.service && ls -l /var/backups/dietsis
```

Lalu buka `https://dietsis.transbara.my.id`, login dengan Google, isi profil, catat satu makanan, dan cek badge "Tersinkron".

## Update rilis

```bash
# di MacBook: npm run release  → kirim tarball → di VPS:
cd /tmp && tar xzf dietsis-<versi>-linux-amd64.tar.gz && cd dietsis-<versi>-linux-amd64
sudo ./deploy/install.sh        # backup DB dulu, ganti static (symlink atomik), ganti binary, restart, cek health
```

Migrasi database diterapkan otomatis saat service start. Service menolak start bila migrasi yang sudah diterapkan berubah atau database lebih baru dari binary.

**Rollback**

- Static: `sudo ln -sfn /var/www/dietsis/releases/<versi-lama> /var/www/dietsis/current` (5 rilis terakhir disimpan).
- Binary: jalankan `install.sh` dari tarball versi lama. Bila rilis baru sudah memigrasi database ke skema yang tidak kompatibel, restore backup yang dibuat otomatis sebelum update (lihat di bawah).

## Backup dan restore

Otomatis harian oleh `dietsis-backup.timer`: snapshot konsisten (`VACUUM INTO`) saat server tetap melayani, diverifikasi (`integrity_check`, `foreign_key_check`, tabel Dietsis ada), dikompres, izin 0600, lalu retensi `DIETSIS_BACKUP_KEEP` file terbaru (default 14).

```bash
# manual
sudo systemctl start dietsis-backup.service
ls -l /var/backups/dietsis/

# restore (service HARUS berhenti; perintah menolak bila API masih listen)
sudo systemctl stop dietsis
sudo -u dietsis env $(grep -v '^#' /etc/dietsis/dietsis.env | xargs) \
  /opt/dietsis/bin/dietsis-server restore /var/backups/dietsis/dietsis-YYYYMMDD-HHMMSS.db.gz
sudo systemctl start dietsis
curl -fsS http://127.0.0.1:8787/api/v1/health
```

Restore memverifikasi backup lebih dulu; backup rusak ditolak dan database aktif tidak disentuh. Database lama disimpan sebagai `dietsis.db.pre-restore-<waktu>` (hapus manual bila sudah yakin). Perangkat yang offline lama akan menyinkronkan ulang dari server; bila data lebih baru dari backup hilang, perubahan lokal yang belum terkirim di perangkat tetap dikirim.

**Salinan di luar VPS (sangat disarankan).** Backup di VPS yang sama tidak melindungi dari hilangnya VPS. Contoh dari MacBook (mingguan, via cron/launchd): `rsync -a user@vps:/var/backups/dietsis/ ~/Backups/dietsis/`. Backup berisi data kesehatan: simpan di disk terenkripsi. Uji restore ke mesin lain minimal sebulan sekali.

Catatan privasi: data yang dihapus lewat "Hapus seluruh data" masih ada di backup lama sampai terhapus oleh retensi (≤ 14 hari).

## Operasional

- Log: `journalctl -u dietsis -f` (JSON; tanpa token, cookie, atau isi permintaan).
- Status: `systemctl status dietsis`, `systemctl list-timers dietsis-backup.timer`.
- Uptime monitor eksternal (opsional): `https://dietsis.transbara.my.id/api/v1/health` (503 bila database tak terjangkau).
- Disk: pantau `/var/lib/dietsis` dan `/var/backups/dietsis`.
- Nginx membatasi `/api/` 10 req/dtk per IP (burst 40) dan body 1 MiB; proses Go membatasi lagi per IP/pengguna.
