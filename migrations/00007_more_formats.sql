-- +goose NO TRANSACTION
-- +goose Up
-- Format tambahan: M4A (AAC dan ALAC), Opus, Ogg Vorbis, FLAC, dan WAV untuk
-- audio; MKV, MOV, WebM, AVI, dan FLV untuk video. Lihat planning §11 dan
-- ADR-036.
--
-- Tabel presets dibangun ulang karena CHECK lama mewajibkan bitrate atau
-- kualitas VBR, sedangkan FLAC, ALAC, dan WAV tidak punya keduanya. SQLite
-- tidak dapat mengubah CHECK di tempat, jadi tabel baru dibuat, diisi, lalu
-- menggantikan yang lama. jobs.preset_id merujuk presets, sehingga
-- penegakan foreign key dimatikan selama penggantian (PRAGMA itu tidak
-- berlaku di dalam transaksi, karena itu migrasi ini mengelola transaksinya
-- sendiri) dan rujukannya diperiksa sendiri sebelum commit. Koneksi penulis hanya satu,
-- jadi PRAGMA berlaku untuk seluruh pernyataan di bawah.
--
-- passthrough berarti stream sumber boleh disalin apa adanya bila codecnya
-- sudah sesuai format. Semua preset video memakainya; di antara preset
-- audio hanya Opus asli, sisanya selalu di-encode ke kualitas yang dipilih.
PRAGMA foreign_keys = OFF;

BEGIN;

CREATE TABLE presets_new (
  id           TEXT PRIMARY KEY,
  label        TEXT NOT NULL,
  format       TEXT NOT NULL,
  codec        TEXT NOT NULL,
  mode         TEXT NOT NULL CHECK (mode IN ('cbr','vbr','lossless')),
  bitrate_kbps INTEGER,
  vbr_quality  INTEGER,
  sample_rate  INTEGER,
  channels     INTEGER NOT NULL,
  extra_args   TEXT NOT NULL DEFAULT '[]',
  sort_order   INTEGER NOT NULL,
  deprecated   INTEGER NOT NULL DEFAULT 0,
  kind         TEXT NOT NULL DEFAULT 'audio' CHECK (kind IN ('audio','video')),
  max_height   INTEGER CHECK (max_height IS NULL OR max_height > 0),
  passthrough  INTEGER NOT NULL DEFAULT 0 CHECK (passthrough IN (0, 1)),
  CHECK ((mode = 'cbr' AND bitrate_kbps IS NOT NULL)
      OR (mode = 'vbr' AND vbr_quality  IS NOT NULL)
      OR (mode = 'lossless' AND bitrate_kbps IS NULL AND vbr_quality IS NULL))
);

INSERT INTO presets_new
  (id, label, format, codec, mode, bitrate_kbps, vbr_quality, sample_rate, channels,
   extra_args, sort_order, deprecated, kind, max_height, passthrough)
SELECT id, label, format, codec, mode, bitrate_kbps, vbr_quality, sample_rate, channels,
       extra_args, sort_order, deprecated, kind, max_height,
       CASE WHEN kind = 'video' THEN 1 ELSE 0 END
FROM presets;

DROP TABLE presets;
ALTER TABLE presets_new RENAME TO presets;

-- Audio. AAC dan Vorbis 48 kHz mengikuti preset MP3 (ADR-030); lossless
-- mengikuti sumber. Opus asli disalin dari sumber Opus YouTube; 160 kbps
-- hanya dipakai bila sumbernya ternyata bukan Opus.
INSERT INTO presets
  (id, label, format, codec, mode, bitrate_kbps, vbr_quality, sample_rate, channels, sort_order, kind, passthrough)
VALUES
  ('m4a_192',     'AAC',        'm4a',  'aac',        'cbr',      192,  NULL, 48000, 2, 55, 'audio', 0),
  ('m4a_256',     'AAC',        'm4a',  'aac',        'cbr',      256,  NULL, 48000, 2, 60, 'audio', 0),
  ('m4a_alac',    'ALAC',       'm4a',  'alac',       'lossless', NULL, NULL, NULL,  2, 65, 'audio', 0),
  ('opus_source', 'Original',   'opus', 'libopus',    'cbr',      160,  NULL, 48000, 2, 70, 'audio', 1),
  ('ogg_q6',      'Vorbis Q6',  'ogg',  'libvorbis',  'vbr',      NULL, 6,    48000, 2, 75, 'audio', 0),
  ('flac',        'Lossless',   'flac', 'flac',       'lossless', NULL, NULL, NULL,  2, 80, 'audio', 0),
  ('wav_pcm16',   'PCM 16-bit', 'wav',  'pcm_s16le',  'lossless', NULL, NULL, NULL,  2, 85, 'audio', 0);

-- Video. codec dan bitrate_kbps berlaku bagi audio yang harus di-encode
-- ulang: AAC untuk MOV dan FLV, Opus untuk MKV dan WebM, MP3 untuk AVI.
INSERT INTO presets
  (id, label, format, codec, mode, bitrate_kbps, vbr_quality, sample_rate, channels, sort_order, kind, max_height, passthrough)
VALUES
  ('mkv_360',   '360p',  'mkv',  'libopus',    'cbr', 128, NULL, NULL, 2, 160, 'video', 360,  1),
  ('mkv_480',   '480p',  'mkv',  'libopus',    'cbr', 128, NULL, NULL, 2, 170, 'video', 480,  1),
  ('mkv_720',   '720p',  'mkv',  'libopus',    'cbr', 160, NULL, NULL, 2, 180, 'video', 720,  1),
  ('mkv_1080',  '1080p', 'mkv',  'libopus',    'cbr', 160, NULL, NULL, 2, 190, 'video', 1080, 1),
  ('mkv_best',  'Best',  'mkv',  'libopus',    'cbr', 160, NULL, NULL, 2, 200, 'video', NULL, 1),
  ('mov_360',   '360p',  'mov',  'aac',        'cbr', 128, NULL, NULL, 2, 210, 'video', 360,  1),
  ('mov_480',   '480p',  'mov',  'aac',        'cbr', 128, NULL, NULL, 2, 220, 'video', 480,  1),
  ('mov_720',   '720p',  'mov',  'aac',        'cbr', 192, NULL, NULL, 2, 230, 'video', 720,  1),
  ('mov_1080',  '1080p', 'mov',  'aac',        'cbr', 192, NULL, NULL, 2, 240, 'video', 1080, 1),
  ('mov_best',  'Best',  'mov',  'aac',        'cbr', 192, NULL, NULL, 2, 250, 'video', NULL, 1),
  ('webm_360',  '360p',  'webm', 'libopus',    'cbr', 128, NULL, NULL, 2, 260, 'video', 360,  1),
  ('webm_480',  '480p',  'webm', 'libopus',    'cbr', 128, NULL, NULL, 2, 270, 'video', 480,  1),
  ('webm_720',  '720p',  'webm', 'libopus',    'cbr', 160, NULL, NULL, 2, 280, 'video', 720,  1),
  ('webm_1080', '1080p', 'webm', 'libopus',    'cbr', 160, NULL, NULL, 2, 290, 'video', 1080, 1),
  ('webm_best', 'Best',  'webm', 'libopus',    'cbr', 160, NULL, NULL, 2, 300, 'video', NULL, 1),
  ('avi_360',   '360p',  'avi',  'libmp3lame', 'cbr', 128, NULL, NULL, 2, 310, 'video', 360,  1),
  ('avi_480',   '480p',  'avi',  'libmp3lame', 'cbr', 128, NULL, NULL, 2, 320, 'video', 480,  1),
  ('avi_720',   '720p',  'avi',  'libmp3lame', 'cbr', 192, NULL, NULL, 2, 330, 'video', 720,  1),
  ('avi_1080',  '1080p', 'avi',  'libmp3lame', 'cbr', 192, NULL, NULL, 2, 340, 'video', 1080, 1),
  ('avi_best',  'Best',  'avi',  'libmp3lame', 'cbr', 192, NULL, NULL, 2, 350, 'video', NULL, 1),
  ('flv_360',   '360p',  'flv',  'aac',        'cbr', 128, NULL, NULL, 2, 360, 'video', 360,  1),
  ('flv_480',   '480p',  'flv',  'aac',        'cbr', 128, NULL, NULL, 2, 370, 'video', 480,  1),
  ('flv_720',   '720p',  'flv',  'aac',        'cbr', 192, NULL, NULL, 2, 380, 'video', 720,  1),
  ('flv_1080',  '1080p', 'flv',  'aac',        'cbr', 192, NULL, NULL, 2, 390, 'video', 1080, 1),
  ('flv_best',  'Best',  'flv',  'aac',        'cbr', 192, NULL, NULL, 2, 400, 'video', NULL, 1);

-- Penegakan foreign key sedang mati, jadi rujukan riwayat diperiksa
-- sendiri: satu saja job yang menunjuk preset yang hilang membuat INSERT ini
-- melanggar CHECK dan menggagalkan migrasi sebelum commit.
CREATE TEMP TABLE preset_ref_guard (ok INTEGER CHECK (ok = 1));
INSERT INTO preset_ref_guard SELECT 0 FROM jobs WHERE preset_id NOT IN (SELECT id FROM presets);
DROP TABLE preset_ref_guard;

UPDATE schema_meta SET schema_version = 7 WHERE id = 1;

COMMIT;

PRAGMA foreign_keys = ON;

-- +goose Down
PRAGMA foreign_keys = OFF;

BEGIN;

CREATE TABLE presets_old (
  id           TEXT PRIMARY KEY,
  label        TEXT NOT NULL,
  format       TEXT NOT NULL,
  codec        TEXT NOT NULL,
  mode         TEXT NOT NULL CHECK (mode IN ('cbr','vbr')),
  bitrate_kbps INTEGER,
  vbr_quality  INTEGER,
  sample_rate  INTEGER,
  channels     INTEGER NOT NULL,
  extra_args   TEXT NOT NULL DEFAULT '[]',
  sort_order   INTEGER NOT NULL,
  deprecated   INTEGER NOT NULL DEFAULT 0,
  kind         TEXT NOT NULL DEFAULT 'audio' CHECK (kind IN ('audio','video')),
  max_height   INTEGER CHECK (max_height IS NULL OR max_height > 0),
  CHECK ((mode = 'cbr' AND bitrate_kbps IS NOT NULL)
      OR (mode = 'vbr' AND vbr_quality  IS NOT NULL))
);

INSERT INTO presets_old
  (id, label, format, codec, mode, bitrate_kbps, vbr_quality, sample_rate, channels,
   extra_args, sort_order, deprecated, kind, max_height)
SELECT id, label, format, codec, mode, bitrate_kbps, vbr_quality, sample_rate, channels,
       extra_args, sort_order, deprecated, kind, max_height
FROM presets
WHERE format IN ('mp3', 'mp4');

DROP TABLE presets;
ALTER TABLE presets_old RENAME TO presets;

UPDATE schema_meta SET schema_version = 6 WHERE id = 1;

COMMIT;

PRAGMA foreign_keys = ON;
