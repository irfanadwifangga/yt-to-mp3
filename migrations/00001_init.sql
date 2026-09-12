-- +goose Up
-- Skema awal. Lihat docs/data-model.md untuk invarian dan alasan tiap
-- constraint. Migrasi bersifat maju saja saat runtime; bagian Down hanya
-- dipakai test.

CREATE TABLE presets (
  id           TEXT PRIMARY KEY,
  label        TEXT NOT NULL,
  format       TEXT NOT NULL,
  codec        TEXT NOT NULL,
  mode         TEXT NOT NULL CHECK (mode IN ('cbr','vbr')),
  bitrate_kbps INTEGER,
  vbr_quality  INTEGER,
  -- NULL berarti ikut sample rate sumber; hanya dipakai preset lossless.
  sample_rate  INTEGER,
  channels     INTEGER NOT NULL,
  extra_args   TEXT NOT NULL DEFAULT '[]',
  sort_order   INTEGER NOT NULL,
  deprecated   INTEGER NOT NULL DEFAULT 0,
  CHECK ((mode = 'cbr' AND bitrate_kbps IS NOT NULL)
      OR (mode = 'vbr' AND vbr_quality  IS NOT NULL))
);

CREATE TABLE jobs (
  id             TEXT PRIMARY KEY,
  source_url     TEXT NOT NULL,
  source_key     TEXT NOT NULL,
  title          TEXT,
  status         TEXT NOT NULL CHECK (status IN (
                   'queued','resolving','downloading','converting',
                   'verifying','completed','failed','cancelling','cancelled')),
  preset_id      TEXT NOT NULL REFERENCES presets(id),
  filename_mode  TEXT NOT NULL DEFAULT 'title'
                   CHECK (filename_mode IN ('title','title-uploader','uploader-title','id')),
  progress       REAL CHECK (progress IS NULL OR (progress >= 0 AND progress <= 100)),
  phase          TEXT,
  attempt_count  INTEGER NOT NULL DEFAULT 0,
  error_code     TEXT,
  error_message  TEXT,
  created_at     TEXT NOT NULL,
  started_at     TEXT,
  finished_at    TEXT
);

CREATE INDEX idx_jobs_status  ON jobs(status);
CREATE INDEX idx_jobs_created ON jobs(created_at DESC);

-- Menegakkan DUPLICATE_ACTIVE_JOB di level database, bukan hanya di kode.
CREATE UNIQUE INDEX idx_jobs_active_dedup
  ON jobs(source_key, preset_id)
  WHERE status NOT IN ('completed','failed','cancelled');

CREATE TABLE media_items (
  source_key    TEXT PRIMARY KEY,
  title         TEXT NOT NULL,
  uploader      TEXT,
  duration_ms   INTEGER,
  thumbnail_url TEXT,
  source_codec  TEXT,
  sample_rate   INTEGER,
  is_live       INTEGER NOT NULL DEFAULT 0,
  raw_json      TEXT,
  fetched_at    TEXT NOT NULL
);

CREATE INDEX idx_media_fetched ON media_items(fetched_at);

CREATE TABLE files (
  id         TEXT PRIMARY KEY,
  job_id     TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  path       TEXT NOT NULL,
  filename   TEXT NOT NULL,
  mime       TEXT NOT NULL,
  size_bytes INTEGER NOT NULL CHECK (size_bytes > 0),
  sha256     TEXT NOT NULL,
  missing    INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);

CREATE UNIQUE INDEX idx_files_job ON files(job_id);

-- CHECK sengaja tidak memuat 'progress': pada 4 event per detik,
-- mempersistkannya menghasilkan ribuan baris per job. Lihat ADR-023.
CREATE TABLE job_events (
  job_id     TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  seq        INTEGER NOT NULL,
  type       TEXT NOT NULL CHECK (type IN ('state','error','done')),
  payload    TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (job_id, seq)
);

CREATE TABLE settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  type       TEXT NOT NULL CHECK (type IN ('string','int','bool')),
  updated_at TEXT NOT NULL
);

CREATE TABLE schema_meta (
  id             INTEGER PRIMARY KEY CHECK (id = 1),
  app_version    TEXT NOT NULL,
  schema_version INTEGER NOT NULL
);

INSERT INTO schema_meta (id, app_version, schema_version) VALUES (1, '', 1);

-- Seed preset. Tabel ini append-only karena preset_id tersimpan permanen di
-- history (ADR-028). Sample rate 48 kHz menyesuaikan sumber YouTube yang
-- didominasi Opus (ADR-030).
INSERT INTO presets
  (id, label, format, codec, mode, bitrate_kbps, vbr_quality, sample_rate, channels, sort_order)
VALUES
  ('mp3_economy',  'Economy',  'mp3', 'libmp3lame', 'cbr', 128,  NULL, 48000, 2, 10),
  ('mp3_standard', 'Standard', 'mp3', 'libmp3lame', 'cbr', 192,  NULL, 48000, 2, 20),
  ('mp3_high',     'High',     'mp3', 'libmp3lame', 'cbr', 256,  NULL, 48000, 2, 30),
  ('mp3_max',      'Max',      'mp3', 'libmp3lame', 'cbr', 320,  NULL, 48000, 2, 40),
  ('mp3_vbr_v0',   'VBR High', 'mp3', 'libmp3lame', 'vbr', NULL, 0,    48000, 2, 50);

-- +goose Down
DROP TABLE IF EXISTS schema_meta;
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS job_events;
DROP TABLE IF EXISTS files;
DROP TABLE IF EXISTS media_items;
DROP TABLE IF EXISTS jobs;
DROP TABLE IF EXISTS presets;
