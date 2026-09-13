# Arsitektur Internal

Dokumen desain untuk lapisan dalam aplikasi. Perencanaan tingkat produk ada di [yt-to-mp3-go-planning.md](yt-to-mp3-go-planning.md); skema database ada di [data-model.md](data-model.md).

## 1. Prinsip dan aturan dependensi

Arah impor bersifat satu arah dan ditegakkan di review:

```text
api ──────────┐
              ├──> application ──> domain
infrastructure┘
```

| Layer | Boleh mengimpor | Dilarang |
| --- | --- | --- |
| `domain` | stdlib saja | apa pun dari layer lain |
| `application` | `domain`, stdlib | `api`, `infrastructure`, `net/http`, driver DB |
| `infrastructure` | `application` (untuk memenuhi port), `domain` | `api` |
| `api` | `application`, `domain` | `infrastructure` |

Konsekuensi praktis:

- `application` mendefinisikan **port** (interface); `infrastructure` menyediakan **adapter**. Wiring terjadi hanya di `cmd/app/main.go`.
- `domain` tidak boleh tahu soal HTTP, SQL, proses OS, atau path filesystem.
- Tidak ada variabel global dan tidak ada singleton. Semua dependensi diteruskan lewat konstruktor.
- `context.Context` selalu parameter pertama pada operasi yang bisa lama atau dibatalkan.

## 2. Peta komponen

| Komponen | Paket | Tanggung jawab |
| --- | --- | --- |
| HTTP handler | `api` | Decode/encode, status code, tidak ada logika bisnis |
| Middleware keamanan | `api` | Host allowlist, Origin, token, Content-Type |
| SSE hub | `api` | Fan-out event ke subscriber, replay, heartbeat |
| Job service | `application` | Use case: create, cancel, retry, delete, list |
| Job runner | `application` | Orkestrasi satu job dari resolving sampai commit |
| Scheduler + pool | `worker` | Claim job, batasi konkurensi, kelola siklus goroutine |
| State machine | `domain` | Aturan transisi, klasifikasi error |
| Repository | `infrastructure/db` | Persistensi, transaksi |
| Resolver/Downloader | `infrastructure/ytdlp` | Bangun argv, parse progress, petakan stderr |
| Transcoder | `infrastructure/ffmpeg` | Bangun argv, parse progress, tagging |
| Process runner | `infrastructure/process` | Spawn, terminasi seluruh process tree |
| Output store | `infrastructure/fs` | Reservasi nama, commit atomik, GC temp |
| Tool manager | `infrastructure/tools` | Discovery, unduh, verifikasi checksum |

## 3. Kontrak antar-layer

Sketsa berikut adalah bentuk yang dituju, bukan kode final.

### 3.1 Domain

```go
package domain

type JobStatus string

const (
	StatusQueued      JobStatus = "queued"
	StatusResolving   JobStatus = "resolving"
	StatusDownloading JobStatus = "downloading"
	StatusConverting  JobStatus = "converting"
	StatusVerifying   JobStatus = "verifying"
	StatusCompleted   JobStatus = "completed"
	StatusFailed      JobStatus = "failed"
	StatusCancelling  JobStatus = "cancelling"
	StatusCancelled   JobStatus = "cancelled"
)

func (s JobStatus) IsTerminal() bool

// CanTransition adalah satu-satunya sumber kebenaran transisi.
// Tidak ada tempat lain di codebase yang boleh menulis kolom status.
func CanTransition(from, to JobStatus) bool

type ErrorClass string

const (
	ClassTransient    ErrorClass = "transient"
	ClassThrottled    ErrorClass = "throttled"
	ClassToolOutdated ErrorClass = "tool_outdated"
	ClassPermanent    ErrorClass = "permanent"
	ClassLocal        ErrorClass = "local"
)

type Error struct {
	Code    ErrorCode  // set tertutup, lihat planning "Kode error dan lokalisasi"
	Class   ErrorClass // menentukan kebijakan retry
	Detail  string     // untuk log dan job_events, tidak pernah untuk UI
	Cause   error
}

func (e *Error) Retryable() bool
```

### 3.2 Port yang didefinisikan `application`

```go
package application

type JobRepository interface {
	Create(ctx context.Context, j *domain.Job) error
	Get(ctx context.Context, id string) (*domain.Job, error)
	List(ctx context.Context, q ListQuery) (items []*domain.Job, nextCursor string, err error)

	// Transition memvalidasi lewat domain.CanTransition lalu menulis
	// baris jobs dan job_events dalam satu transaksi.
	Transition(ctx context.Context, id string, from, to domain.JobStatus, ev domain.Event) error

	// ClaimNextQueued bersifat atomik (UPDATE ... RETURNING) sehingga
	// aman walau kelak ada lebih dari satu scheduler.
	ClaimNextQueued(ctx context.Context) (*domain.Job, error)

	SweepNonTerminal(ctx context.Context) (int, error)
}

type MediaResolver interface {
	Resolve(ctx context.Context, url string) (*domain.MediaInfo, error)
}

type Downloader interface {
	Download(ctx context.Context, in DownloadInput, progress chan<- Progress) (path string, err error)
}

type Transcoder interface {
	Transcode(ctx context.Context, in TranscodeInput, progress chan<- Progress) (path string, err error)
}

type Verifier interface {
	Verify(ctx context.Context, path string, expect domain.Expectation) (*domain.FileInfo, error)
}

type OutputStore interface {
	// Reserve membuat placeholder dengan O_CREATE|O_EXCL sehingga dua
	// worker tidak pernah memenangkan nama yang sama.
	Reserve(ctx context.Context, name string) (finalPath string, release func(), err error)
	Commit(ctx context.Context, tmpPath, finalPath string) error
	TempDirFor(jobID string) (string, error)
}

type EventSink interface {
	Publish(jobID string, ev domain.Event)
}

type ToolProvider interface {
	Resolve(name string) (binPath string, version string, err error)
}
```

### 3.3 Port proses

```go
package process

type Spec struct {
	Bin  string
	Args []string
	Dir  string
	Env  []string
}

type Handle struct {
	Stdout io.ReadCloser
	Stderr io.ReadCloser
}

func (h *Handle) Wait() error

// Terminate mengirim sinyal lembut, menunggu grace, lalu membunuh
// SELURUH process tree. Implementasi per-OS di proc_unix.go /
// proc_windows.go. Lihat planning "Process management dan cancellation".
func (h *Handle) Terminate(grace time.Duration) error

type Runner interface {
	Start(ctx context.Context, s Spec) (*Handle, error)
}
```

## 4. Model concurrency dan kepemilikan

### 4.1 Goroutine yang ada

| Goroutine             | Jumlah                   | Umur               |
| --------------------- | ------------------------ | ------------------ |
| HTTP server           | 1 + per-request          | selama app hidup   |
| Scheduler             | 1                        | selama app hidup   |
| Job runner            | 0..`max_concurrent_jobs` | selama satu job    |
| Pembaca stdout/stderr | 2 per proses anak        | selama proses anak |
| SSE hub               | 1                        | selama app hidup   |
| SSE writer            | 1 per koneksi            | selama koneksi     |
| Housekeeper           | 1                        | tick periodik      |

### 4.2 Aturan kepemilikan

Aturan ini yang mencegah sebagian besar race:

1. **Satu job dimiliki tepat satu goroutine.** Hanya goroutine itu yang boleh menulis ke temp dir job tersebut, memegang process handle, dan menulis transisi status.
2. **Handler HTTP tidak pernah menulis status terminal.** `POST /cancel` hanya menulis transisi ke `cancelling` dan memanggil `cancelFunc`; yang menulis `cancelled` adalah job runner setelah proses benar-benar mati.
3. **Registry job aktif hanya di memori**, `map[string]*runningJob` dijaga `sync.Mutex`, tidak pernah dipersist.
4. **Channel progress dimiliki pengirim.** Producer (downloader/transcoder) yang menutupnya, consumer tidak pernah menutup.
5. **Context adalah satu-satunya mekanisme pembatalan.** Tidak ada flag boolean atau channel `done` buatan sendiri.

### 4.3 Topologi aliran data

```text
handler ──create──> job service ──tx insert──> DB
                          │
                          └──notify(non-blocking)──> scheduler
                                                          │
                                                     claim (atomik)
                                                          │
                                                          v
                                                    job runner
                             ┌────────────────────────────┼───────────────┐
                             │                            │               │
                       Progress chan                 transisi state    temp files
                       (buffer 64,                   (tx: jobs +       (dimiliki
                        drop-oldest)                  job_events)       runner)
                             │                            │
                             └──────> aggregator ─────────┘
                                    (throttle 4/detik)
                                             │
                                             v
                                         SSE hub ──> subscriber
```

### 4.4 Back-pressure

- Channel progress berbuffer 64 dengan kebijakan **drop-oldest**: progress bersifat lossy dan boleh hilang, yang penting nilai terbaru sampai.
- Event `state`, `error`, dan `done` **tidak pernah di-drop**; pengiriman ke hub bersifat blocking dengan timeout, dan kegagalannya dicatat sebagai bug, bukan kondisi normal.
- Antrean job dibatasi `max_queue_depth`; melebihi itu ditolak `QUEUE_FULL` di lapisan handler, bukan di worker.

## 5. Walkthrough satu job

Jalur sukses, dari request sampai file final:

1. `POST /api/jobs`. Handler memvalidasi skema URL, menormalkan ke `source_key`, dan menolak lebih awal bila skema tidak didukung.
2. Service menulis baris `jobs` berstatus `queued` beserta event pertama dalam satu transaksi. Unique index parsial pada `(source_key, preset_id)` untuk status non-terminal menegakkan `DUPLICATE_ACTIVE_JOB` di level database, bukan hanya di kode.
3. Handler membalas `202` dan mengirim sinyal non-blocking ke scheduler.
4. Scheduler, bila ada slot kosong, memanggil `ClaimNextQueued` yang memindahkan job ke `resolving` secara atomik.
5. Job runner dibuat dengan context turunan; `cancelFunc`-nya didaftarkan ke registry.
6. **Resolving.** `MediaResolver.Resolve` dipanggil. Bila `is_live` bernilai benar, job gagal dengan `LIVE_NOT_SUPPORTED`. Metadata disimpan ke `media_items`, dan **timeout fase berikutnya dihitung dari durasi media**, bukan konstanta.
7. **Preflight disk.** Estimasi kebutuhan = ukuran unduhan + (durasi × bitrate preset), dikali faktor aman 1.5. Kurang ruang berarti gagal cepat dengan `DISK_FULL`.
8. **Downloading.** `ProcessRunner.Start` menjalankan yt-dlp. Dua goroutine membaca stdout dan stderr; parser mengubah baris progress jadi `Progress` dan mengirimnya ke channel.
9. **Converting.** Thumbnail diunduh secara best-effort dengan batas ukuran; kegagalannya tidak menggagalkan job. ffmpeg dijalankan dengan `-progress pipe:1`.
10. **Verifying.** `ffprobe` memastikan ada stream audio dan durasinya masuk toleransi; `sha256` dihitung.
11. **Commit.** `OutputStore.Reserve` memenangkan nama secara atomik, `Commit` melakukan rename dalam satu volume, lalu baris `files` dan transisi `completed` ditulis dalam satu transaksi.
12. Temp dibersihkan, handle dilepas, event `done` dipublikasikan, slot worker dikembalikan.

Jalur cancel:

1. Handler menemukan `cancelFunc` di registry, menulis transisi `cancelling`, memanggil cancel.
2. Job runner melihat `ctx.Done()`, memanggil `Handle.Terminate(5 * time.Second)`.
3. Runner menunggu `Wait()` benar-benar kembali **sebelum** menghapus temp — di Windows file yang masih dipegang proses tidak bisa dihapus.
4. Runner menulis transisi `cancelled` dan melepas registry.

## 6. Desain SSE hub

Antarmuka:

```go
func (h *Hub) Subscribe(jobID string, lastEventID int64) (<-chan domain.Event, func(), error)
```

Urutan operasi saat subscribe penting, karena naif akan kehilangan event yang terjadi di antara pembacaan histori dan pemasangan listener:

1. Daftarkan channel live lebih dulu (buffer 64).
2. Baca event terpersist dengan `seq > lastEventID` dari `job_events`.
3. Kirim histori, lalu alirkan buffer live sambil membuang duplikat dengan `seq` yang sudah terkirim.

Aturan lain:

- Hanya `state`, `error`, dan `done` yang terpersist dan bisa di-replay. `progress` hidup di memori; saat reconnect, client menerima satu snapshot progress terkini, bukan ribuan frame lama.
- Job yang sudah terminal saat subscribe menerima snapshot lalu stream ditutup. Tidak pernah menggantung.
- Heartbeat `: ping` tiap 15 detik per koneksi.
- Handler wajib memantau `r.Context().Done()` dan memanggil fungsi unsubscribe lewat `defer`.
- Maksimum 4 koneksi per job.

## 7. Lifecycle aplikasi dan single instance

### 7.1 Urutan startup

1. Ambil lock instance (named mutex di Windows, `flock` di Unix) di `<data_dir>/app.lock`.
2. Bila lock gagal: baca `<data_dir>/runtime.json`, buka browser ke URL instance yang sudah jalan, lalu keluar dengan kode 0.
3. Muat konfigurasi, pastikan direktori data dan output ada.
4. Buka SQLite, jalankan migrasi, verifikasi `schema_version` (lihat [data-model.md §2.7](data-model.md)).
5. Jalankan crash recovery sweep (§8).
6. Discovery tool secara non-blocking — tool yang hilang tidak menghalangi startup.
7. Listen di `127.0.0.1:0`, dapatkan port aktual.
8. Tulis `runtime.json`, jalankan housekeeper dan scheduler.
9. Buka browser default ke `http://127.0.0.1:<port>/?token=<token>`.

### 7.2 `runtime.json`

```json
{
  "pid": 12345,
  "port": 51234,
  "token": "…",
  "started_at": "2026-09-12T09:00:00Z",
  "version": "0.1.0"
}
```

Ditulis atomik (temp + rename), dihapus saat shutdown bersih. File basi dari proses yang sudah mati dikenali dengan mengecek keberadaan PID, lalu diambil alih.

### 7.3 Cara keluar

Pustaka tray icon umumnya membutuhkan cgo, yang bertabrakan langsung dengan ADR-011 dan target cross-compile. Karena itu MVP tidak memakai tray, dan penghentian ditangani tiga jalur:

| Jalur | Perilaku |
| --- | --- |
| Tombol Quit di SPA | `POST /api/shutdown` → graceful shutdown |
| Idle shutdown | Tidak ada job aktif, tidak ada koneksi SSE, dan tidak ada request selama 30 menit → keluar |
| Sinyal OS | `SIGINT`/`SIGTERM`, atau penutupan console |

### 7.4 Urutan shutdown

1. Berhenti menerima request baru dan berhenti meng-claim job.
2. Batalkan seluruh context job aktif; job berjalan berakhir sebagai `cancelled`, bukan digantung.
3. Tunggu `sync.WaitGroup` dengan batas 10 detik, lalu paksa terminasi sisa process tree.
4. Tutup DB, hapus `runtime.json`, lepas lock.

## 8. Crash recovery

Dijalankan sebelum listener dibuka:

1. Job berstatus non-terminal ditandai `failed` dengan `error_code = INTERRUPTED`, karena proses OS pemiliknya sudah tidak ada. Job `queued` dipertahankan.
2. Hapus isi `<data_dir>/tmp/` dan `<output_dir>/.tmp/` yang tidak dirujuk job aktif.
3. Baris `files` yang path-nya hilang di disk ditandai `missing = 1`.

## 9. Housekeeping

Satu goroutine dengan tick per jam:

| Tugas | Kebijakan |
| --- | --- |
| Pangkas `job_events` | Buang event job terminal yang lebih tua dari 30 hari |
| GC temp yatim | Hapus temp berumur > 24 jam yang bukan milik job aktif; direktori kerja bernama id job dan berkas commit `<id>-*` dikenali sebagai milik job |
| Kedaluwarsa cache metadata | Buang baris `media_items` yang `fetched_at`-nya lewat TTL dan tidak dirujuk job |
| Rekonsiliasi berkas | Samakan `files.missing` dengan disk, dua arah |
| Rotasi log | Dikerjakan writer log sendiri saat hari berganti (`logs/app.log` → `app-YYYY-MM-DD.log`), retensi 7 hari |

Putaran pertama berjalan saat startup sebelum scheduler dan listener, sehingga langkah 2 dan 3 crash recovery di atas dikerjakan oleh housekeeper yang sama.
| Cek update tool | Mingguan, opsional, tidak pernah otomatis memasang |

## 10. Invarian yang tidak boleh dilanggar

Daftar ini adalah checklist review dan dasar test:

1. Kolom `status` hanya ditulis lewat `JobRepository.Transition`.
2. Setiap transisi status dan event terpersist berada dalam satu transaksi yang sama.
3. Tidak ada path filesystem yang pernah menyeberang batas API.
4. Tidak ada perintah eksternal yang dijalankan lewat shell.
5. File output final hanya muncul lewat rename dari temp satu volume.
6. Nama file final hanya dimenangkan lewat reservasi `O_EXCL`.
7. Temp hanya dihapus setelah `Wait()` proses pemiliknya kembali.
8. Pesan mentah tool tidak pernah sampai ke UI.
9. Setiap goroutine punya pemilik yang jelas dan berakhir saat context-nya dibatalkan.
10. Semua timestamp disimpan UTC ISO-8601 dan hanya dilokalkan saat render.
