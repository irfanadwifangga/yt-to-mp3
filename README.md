# yt-to-mp3

Konverter audio YouTube menjadi MP3 berbentuk aplikasi desktop-lokal: satu binary Go yang menjalankan server di loopback dan menyajikan SPA React yang sudah tersemat di dalamnya.

> **Status: tahap 1 dari 9.** Fondasi sudah berjalan — server, keamanan lokal, single instance, health, dan SPA ter-embed. Pipeline konversi (yt-dlp + FFmpeg) belum ada. Lihat [roadmap](docs/yt-to-mp3-go-planning.md#26-roadmap).

## Kebutuhan

| Perkakas | Versi | Keterangan |
| --- | --- | --- |
| Go | 1.27+ | Membangun backend |
| Node.js | 24+ | Membangun SPA |
| yt-dlp | — | Diunduh otomatis saat runtime (tahap 2) |
| FFmpeg | — | Diunduh otomatis saat runtime (tahap 2) |

Tidak ada dependensi Go pihak ketiga; `go build` berjalan tanpa jaringan.

## Mulai cepat

```bash
make install-web && make build && make run
```

Tanpa `make`:

```bash
npm --prefix web install && npm --prefix web run build && go build -o bin/yt-to-mp3 ./cmd/app && ./bin/yt-to-mp3
```

Aplikasi memilih port acak di `127.0.0.1`, menulis `runtime.json`, lalu membuka browser dengan session token di URL. Menjalankannya untuk kedua kali tidak membuat instance baru — cukup membuka kembali jendela yang sudah ada.

Menghentikan aplikasi: tombol **Keluar** di UI, `Ctrl+C` di terminal, atau biarkan idle 30 menit. Tidak ada tray icon, karena pustaka tray membutuhkan cgo dan itu akan merusak target cross-compile ([ADR-021](docs/yt-to-mp3-go-planning.md#4-keputusan-teknis)).

## Pengembangan

Backend dan frontend berjalan terpisah supaya hot reload Vite tetap hidup. Dua terminal:

```bash
make dev-api
```

```bash
make dev-web
```

Backend memakai port tetap 8799 di mode dev (produksi tetap acak) karena proxy Vite butuh target yang bisa ditebak. Flag `-dev` mengizinkan origin `localhost:5173`; flag ini tidak pernah aktif pada build rilis.

Buka `http://localhost:5173`. Token diambil otomatis dari backend lewat proxy.

### Perintah lain

```bash
make check
```

Menjalankan pemeriksaan format, `go vet`, dan seluruh test. Jalankan `make help` untuk daftar lengkap.

## Struktur

```text
cmd/app/          entry point, seluruh wiring dependensi
internal/
  api/            HTTP handler, middleware keamanan, SSE hub
  application/    use case dan port
  domain/         entity, state machine, taksonomi error
  infrastructure/ adapter: db, ytdlp, ffmpeg, process, fs, tools
  worker/         scheduler dan worker pool
  config/         konfigurasi dan lokasi penyimpanan per OS
  instance/       penjagaan single instance
web/              SPA React + TypeScript, di-embed lewat go:embed
docs/             perencanaan, arsitektur, data model
migrations/       migrasi SQL
```

Arah impor satu arah: `api` dan `infrastructure` boleh bergantung pada `application`, `application` hanya pada `domain`, dan `domain` tidak bergantung pada apa pun. Detailnya di [architecture.md](docs/architecture.md).

## Lokasi data

| OS | Data | Output default |
| --- | --- | --- |
| Windows | `%LOCALAPPDATA%\yt-to-mp3\` | `%USERPROFILE%\Music\yt-to-mp3\` |
| macOS | `~/Library/Application Support/yt-to-mp3/` | `~/Music/yt-to-mp3/` |
| Linux | `$XDG_DATA_HOME/yt-to-mp3/` | `$XDG_MUSIC_DIR` atau `~/Music/yt-to-mp3/` |

Konfigurasi ada di `config.json` dalam direktori data, dibuat otomatis saat pertama kali dijalankan.

## Keamanan

Server lokal bukan berarti server privat: situs web mana pun yang sedang dibuka pengguna dapat mengirim request ke `127.0.0.1`. Karena itu server ini menerapkan bind loopback eksplisit, allowlist header `Host` sebagai penangkal DNS rebinding, pemeriksaan `Origin` pada setiap request yang mengubah state, session token acak per proses, CORS deny-all, dan batas ukuran body. Perinciannya ada di [planning §15](docs/yt-to-mp3-go-planning.md).

## Dokumentasi

| Dokumen | Isi |
| --- | --- |
| [yt-to-mp3-go-planning.md](docs/yt-to-mp3-go-planning.md) | Ruang lingkup, 30 ADR, kontrak API, roadmap, kriteria selesai |
| [architecture.md](docs/architecture.md) | Aturan dependensi, port/adapter, model concurrency, siklus hidup |
| [data-model.md](docs/data-model.md) | Skema SQLite, invarian, retensi, migrasi |

## Catatan legal

Mengunduh konten YouTube umumnya melanggar Terms of Service platform tersebut, dan hak cipta atas materi yang diunduh tetap pada pemiliknya. Proyek ini ditujukan sebagai alat lokal-personal dan tidak menyediakan distribusi, sharing, maupun hosting konten. Konsekuensi praktisnya: yt-dlp rusak secara berkala mengikuti perubahan sisi YouTube, sehingga tool sengaja dipisah dari siklus rilis aplikasi.

## Lisensi

[MIT](LICENSE)
