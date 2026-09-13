# yt-to-mp3

Konverter audio YouTube menjadi MP3 berbentuk aplikasi desktop-lokal: satu binary Go yang menjalankan server di loopback dan menyajikan SPA React yang tersemat di dalamnya. Tidak ada layanan cloud; semua pekerjaan dan data tinggal di mesinmu.

> **Status: fungsi inti lengkap.** Analisis, antrean, konversi, riwayat, setelan, dan dua bahasa sudah berjalan. Yang belum: rilis biner siap pakai dan hardening (tahap 8–9 [roadmap](docs/yt-to-mp3-go-planning.md#26-roadmap)). Baca [Keterbatasan yang diketahui](#keterbatasan-yang-diketahui) sebelum memakai.

## Fitur

- **Analisis tanpa mengunduh** — judul, channel, durasi, dan codec sumber dibaca lebih dulu.
- **Antrean dengan progress live** per job lewat SSE. Membatalkan job menghentikan seluruh process tree, termasuk FFmpeg yang dijalankan yt-dlp, lalu membersihkan berkas sementara.
- **Riwayat** — unduh hasil, buka lokasinya di file manager, coba lagi yang gagal, atau hapus.
- **Lima preset MP3**: 128, 192, 256, 320 kbps CBR, dan VBR V0. Keluaran 48 kHz stereo dengan tag ID3v2.3 dan sampul tersemat.
- **Setelan dari UI**, dengan penanda untuk setelan yang baru berlaku setelah restart.
- **Bahasa Indonesia dan Inggris**, dipilih otomatis dari bahasa browser dan bisa diganti di footer.
- **Satu binary, satu instance** — menjalankannya dua kali membuka jendela yang sudah ada, bukan server kedua.

## Kebutuhan

| Perkakas | Versi | Dipakai untuk |
| --- | --- | --- |
| Go | 1.27+ | Membangun backend |
| Node.js | 24+ | Membangun SPA; tidak dibutuhkan saat aplikasi berjalan |
| yt-dlp | terbaru | Membaca metadata dan mengunduh audio |
| FFmpeg dan ffprobe | dengan `libmp3lame`; diuji pada 9.0.1 | Konversi dan verifikasi hasil |

Build pertama mengunduh modul Go dan paket npm, jadi butuh jaringan sekali. Seluruh dependensi Go murni Go (tanpa cgo), sehingga cross-compile tidak memerlukan toolchain C.

### Memasang yt-dlp dan FFmpeg

UI punya tombol **Pasang**, tetapi saat ini instalasi otomatis sengaja ditolak karena manifest versi tool belum di-pin (lihat [Keterbatasan](#keterbatasan-yang-diketahui)). Sementara itu, pasang lewat package manager — aplikasi menemukannya dari `PATH`.

Windows:

```bash
winget install yt-dlp.yt-dlp
```

```bash
winget install Gyan.FFmpeg
```

macOS:

```bash
brew install yt-dlp ffmpeg
```

Linux: pasang FFmpeg dari paket distro. Untuk yt-dlp, pakai binary dari [rilis resminya](https://github.com/yt-dlp/yt-dlp/releases) — paket distro sering tertinggal, padahal yt-dlp harus terus mengikuti perubahan di sisi YouTube.

Tool yang dipasang ke direktori yang sudah ada di `PATH` terdeteksi dalam 30 detik tanpa restart. Bila pemasangannya menambah direktori baru ke `PATH` — biasanya pada pemasangan pertama lewat winget — buka ulang aplikasi, karena proses yang sedang berjalan tidak melihat perubahan `PATH`.

## Mulai cepat

```bash
make install-web && make build && make run
```

Tanpa `make`:

```bash
npm --prefix web install && npm --prefix web run build && go build -o bin/yt-to-mp3 ./cmd/app && ./bin/yt-to-mp3
```

Aplikasi memilih port acak di `127.0.0.1` lalu membuka browser. Alamat pembukanya membawa session token sekali pakai; token itu langsung dipindahkan ke `sessionStorage` dan dihapus dari address bar.

Cara memakai:

1. Tempel URL video YouTube, lalu tekan **Analisis**.
2. Pilih preset, lalu **Konversi**.
3. Pantau progress di **Antrean**. Hasilnya muncul di **Riwayat** dan di folder keluaran.

Menghentikan aplikasi: tombol **Keluar** di footer, atau `Ctrl+C` di terminal. Tidak ada tray icon karena pustaka tray membutuhkan cgo, dan itu akan merusak target cross-compile ([ADR-021](docs/yt-to-mp3-go-planning.md#4-keputusan-teknis)).

## Konfigurasi

Setelan dapat diubah dari bagian **Setelan** di UI, atau lewat `config.json` di direktori data (dibuat otomatis saat pertama kali dijalankan), atau lewat variabel lingkungan:

| Variabel | Setara dengan |
| --- | --- |
| `YT2MP3_OUTPUT_DIR` | `output_dir` |
| `YT2MP3_LOG_LEVEL` | `log_level` |
| `YT2MP3_MAX_CONCURRENT_JOBS` | `max_concurrent_jobs` |

Urutan prioritasnya: nilai bawaan, lalu `config.json`, lalu variabel lingkungan, lalu perubahan dari UI.

Preset bawaan, pola nama berkas, dan kapasitas antrean berlaku seketika setelah disimpan dari UI. Setelan bertanda **perlu restart** — direktori keluaran, jumlah job paralel, dan tingkat log — untuk sementara ubahlah lewat `config.json` atau variabel lingkungan; lihat alasannya di [Keterbatasan](#keterbatasan-yang-diketahui).

## Pengembangan

Backend dan frontend dijalankan terpisah supaya hot reload Vite tetap hidup. Dua terminal:

```bash
make dev-api
```

```bash
make dev-web
```

Backend memakai port tetap 8799 di mode dev (produksi tetap acak) karena proxy Vite butuh target yang bisa ditebak. Flag `-dev` mengizinkan origin `localhost:5173` dan tidak pernah aktif pada build rilis.

Session token hanya bisa masuk lewat URL, jadi buka SPA dengan menempelkan token dari log `make dev-api` (baris `msg=siap`):

```
http://localhost:5173/?token=<token-dari-log>
```

### Pemeriksaan

| Perintah | Isi |
| --- | --- |
| `make check` | Format, `go vet`, dan seluruh test Go |
| `make typecheck` | Typecheck frontend |
| `make check-i18n` | Setiap kode error Go punya terjemahan `id` dan `en`, dan kunci kedua bahasa setara |

Ketiganya juga dijalankan CI. `make help` menampilkan seluruh target.

Sebagian test menjalankan proses sungguhan — test terminasi process tree men-spawn proses anak dan cucu untuk membuktikan keduanya mati — sehingga paket `process` butuh beberapa detik.

## Struktur

```text
cmd/app/              entry point dan wiring dependensi
internal/
  api/                HTTP handler, middleware keamanan, SSE hub
  application/        use case, port, orkestrasi pipeline
  domain/             entity, state machine, taksonomi error
  infrastructure/
    db/               SQLite, repository, runner migrasi
    ffmpeg/           transcode, parsing progress, verifikasi dengan ffprobe
    fs/               reservasi nama, commit atomik, sanitasi nama, ruang disk
    process/          proses anak dan terminasi process tree per OS
    tools/            discovery dan instalasi yt-dlp serta FFmpeg
    ytdlp/            metadata, unduhan, pemetaan error
  worker/             scheduler dan worker pool
  config/             konfigurasi dan lokasi penyimpanan per OS
  instance/           penjagaan single instance
  browser/            membuka browser dan file manager
  version/            identitas build
web/                  SPA React + TypeScript, di-embed lewat go:embed
  src/locales/        kamus terjemahan id dan en
  scripts/            pemeriksa kelengkapan terjemahan
migrations/           migrasi SQL, di-embed
scripts/              pemutakhiran manifest tool
docs/                 perencanaan, arsitektur, data model
```

Arah impor satu arah: `api` dan `infrastructure` bergantung pada `application`, `application` hanya pada `domain`, dan `domain` tidak bergantung pada apa pun. Detailnya di [architecture.md](docs/architecture.md).

## API

Seluruh endpoint berada di bawah `/api` dan mewajibkan header `X-Session-Token`, kecuali `GET /api/ping` yang dipakai untuk mendeteksi instance yang sudah berjalan. Kontrak lengkapnya di [planning §7](docs/yt-to-mp3-go-planning.md#7-kontrak-api).

| Endpoint | Fungsi |
| --- | --- |
| `GET /health` | Status aplikasi, tool, dan antrean |
| `GET /tools` · `POST /tools/install` | Status dan instalasi tool |
| `GET /presets` | Daftar preset |
| `POST /metadata` | Analisis URL tanpa mengunduh |
| `POST /jobs` · `GET /jobs` · `GET /jobs/{id}` | Buat, daftar, dan baca job |
| `GET /jobs/{id}/events` | Progress live (SSE, mendukung `Last-Event-ID`) |
| `POST /jobs/{id}/cancel` · `POST /jobs/{id}/retry` · `DELETE /jobs/{id}` | Batalkan, ulangi, hapus |
| `GET /files/{id}` · `POST /files/{id}/reveal` | Unduh hasil, buka lokasinya |
| `GET /settings` · `PUT /settings` | Baca dan ubah setelan |
| `POST /shutdown` | Hentikan aplikasi |

## Lokasi data

| OS | Data | Keluaran bawaan |
| --- | --- | --- |
| Windows | `%LOCALAPPDATA%\yt-to-mp3\` | `%USERPROFILE%\Music\yt-to-mp3\` |
| macOS | `~/Library/Application Support/yt-to-mp3/` | `~/Music/yt-to-mp3/` |
| Linux | `$XDG_DATA_HOME/yt-to-mp3/` | `$XDG_MUSIC_DIR` atau `~/Music/yt-to-mp3/` |

Direktori data berisi database SQLite (`db/app.db`), `config.json`, tool yang dipasang aplikasi, berkas sementara, dan `runtime.json` selama aplikasi berjalan.

## Keamanan

Server lokal bukan berarti server privat: situs web mana pun yang sedang dibuka pengguna dapat mengirim request ke `127.0.0.1`. Karena itu server ini menerapkan bind loopback eksplisit, allowlist header `Host` sebagai penangkal DNS rebinding, pemeriksaan `Origin` pada setiap request yang mengubah state, session token acak per proses, CORS deny-all, dan batas ukuran body. Path berkas tidak pernah diterima dari klien, dan argumen tool tidak pernah melewati shell. Perinciannya di [planning §15](docs/yt-to-mp3-go-planning.md#15-model-keamanan-lokal).

## Keterbatasan yang diketahui

- **Instalasi tool otomatis ditolak.** Manifest versi yt-dlp dan FFmpeg belum di-pin; checksum yang kosong sengaja menolak instalasi alih-alih menjalankan binary tanpa verifikasi ([ADR-033](docs/yt-to-mp3-go-planning.md#4-keputusan-teknis)). Pasang tool lewat package manager seperti di atas. Untuk mengaktifkannya, jalankan `scripts/update-tool-manifest.sh` (butuh `jq`) lalu commit manifest hasilnya.
- **Setelan bertanda "perlu restart" tersimpan, tetapi belum diterapkan.** Saat startup, direktori keluaran, jumlah job paralel, dan tingkat log masih dibaca dari `config.json` dan variabel lingkungan, bukan dari nilai yang disimpan lewat UI.
- **Berhenti otomatis saat idle belum diimplementasikan.** Setelan `idle_shutdown_minutes` tampil di UI tetapi belum berpengaruh; aplikasi hanya berhenti lewat **Keluar** atau `Ctrl+C`.
- **Log hanya ditulis ke terminal**, belum ke berkas.
- **Belum ada sumber FFmpeg untuk macOS arm64** di manifest; di sana pakai FFmpeg dari `PATH`.
- **Belum ada rilis biner**; build dari source.

Sengaja tidak didukung: playlist, siaran langsung, video yang butuh login atau dibatasi usia, dan keluaran video. Daftar lengkapnya di [non-goals](docs/yt-to-mp3-go-planning.md#3-non-goals).

## Dokumentasi

| Dokumen | Isi |
| --- | --- |
| [yt-to-mp3-go-planning.md](docs/yt-to-mp3-go-planning.md) | Ruang lingkup, 33 ADR, kontrak API, roadmap, kriteria selesai |
| [architecture.md](docs/architecture.md) | Aturan dependensi, port/adapter, model concurrency, siklus hidup |
| [data-model.md](docs/data-model.md) | Skema SQLite, invarian, retensi, migrasi |

## Catatan legal

Mengunduh konten YouTube umumnya melanggar Terms of Service platform tersebut, dan hak cipta atas materi yang diunduh tetap pada pemiliknya. Proyek ini ditujukan sebagai alat lokal-personal dan tidak menyediakan distribusi, sharing, maupun hosting konten. Konsekuensi praktisnya: yt-dlp rusak secara berkala mengikuti perubahan di sisi YouTube, sehingga tool sengaja dipisah dari siklus rilis aplikasi.

## Lisensi

[MIT](LICENSE)
