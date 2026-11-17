# Dietsis — Product Brief

**Tagline:** Teman diet harianmu.
**Domain produksi:** https://dietsis.transbara.my.id
**Repository:** https://github.com/MonkeyDevBara/dietsis

## Masalah

Mencatat makanan setiap hari itu melelahkan. Aplikasi yang ada sering lambat, penuh iklan, dan tidak berfungsi saat sinyal buruk — padahal momen makan sering terjadi di tempat dengan koneksi tidak stabil. Pengguna berhenti mencatat, lalu kehilangan gambaran asupan dan progresnya.

## Solusi

Dietsis adalah personal nutrition tracker berbentuk PWA yang ringan, mobile-first, dan **offline-first**: setiap pencatatan langsung tersimpan di perangkat dan disinkronkan ke server saat online. Tidak ada momen "gagal menyimpan".

## Pengguna sasaran

- Orang dewasa (18+) yang ingin menurunkan, menjaga, atau menaikkan berat badan.
- Menggunakan ponsel sebagai perangkat utama, kadang membuka di laptop.
- Bahasa antarmuka: Bahasa Indonesia; database makanan berfokus pada makanan Indonesia.

## Tujuan produk

1. Pencatatan makanan harian dalam ≤ 3 ketukan untuk makanan yang sering dimakan.
2. Pengguna memahami asupan kalori dan makro (protein, karbohidrat, lemak) terhadap target.
3. Membantu mencapai target berat badan lewat pemantauan berat dan lingkar pinggang.
4. Memantau konsumsi air.
5. Tetap dapat digunakan penuh tanpa internet; tidak ada data yang hilang.

## Cakupan fitur (v1)

| Area | Fitur |
|---|---|
| Dashboard | Kalori hari ini vs target, protein/karbo/lemak, air vs target, berat terakhir, ringkasan progres, aktivitas harian (langkah, catatan) |
| Food diary | Kategori sarapan/makan siang/makan malam/camilan; tambah, edit, hapus + undo; pencarian; makanan terakhir; favorit; makanan kustom; jumlah porsi; kalkulasi nutrisi otomatis |
| Nutrition calculator | Kalori, protein, karbo, lemak, berat per porsi; total = per porsi × jumlah porsi |
| Water tracker | Target harian, tambah/kurangi gelas (aman multi-perangkat), ringkasan harian, riwayat |
| Weight tracking | Berat + lingkar pinggang per tanggal (satu entri per tanggal), riwayat, grafik, target, ringkasan perubahan |
| Profil & onboarding | Nama, gender, usia, tinggi, berat, target berat, activity level, target kalori/protein/air (rekomendasi otomatis, bisa diubah manual) |
| Settings | Info akun, status sinkronisasi, export CSV, logout, hapus seluruh data |
| Auth | Login dengan Google; sesi aman via cookie HttpOnly |

## Bukan cakupan v1

Meal planner, barcode scanner, pengenalan foto/AI, fitur sosial, push notification, integrasi wearable/step counter otomatis, multi-bahasa. Langkah harian dicatat manual.

## Batasan kesehatan

Semua angka rekomendasi (kalori, protein, air) adalah **estimasi umum** berbasis formula yang terdokumentasi di [DECISIONS.md](DECISIONS.md#formula-rekomendasi-target). Dietsis bukan alat medis, tidak memberikan diagnosis, dan menampilkan pengingat untuk berkonsultasi dengan tenaga kesehatan bagi kondisi khusus (kehamilan, penyakit kronis, gangguan makan). Target kalori di bawah batas aman umum diberi peringatan.

## Prinsip

- **Cepat:** UI tidak pernah menunggu jaringan.
- **Sederhana:** satu tugas utama per layar; informasi sekunder di balik ketukan.
- **Jujur:** status sinkronisasi selalu terlihat; konflik dan kegagalan dilaporkan, tidak disembunyikan.
- **Privat:** data per pengguna terisolasi; pengguna dapat mengekspor dan menghapus seluruh datanya.
- **Murah dirawat:** satu binary Go + SQLite + Nginx di satu VPS; tanpa SaaS berbayar.

## Kriteria sukses rilis

Lihat Definition of Done di [PM_STATUS.md](PM_STATUS.md#definition-of-done).
