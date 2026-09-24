-- +goose Up
-- Keluaran video MP4 di samping audio MP3. Preset tetap satu tabel karena
-- job, dedup, dan riwayat seluruhnya berporos pada preset_id; memisahkan
-- tabel video berarti menggandakan semua itu.
--
-- kind membedakan jalur pipeline. max_height adalah batas resolusi yang
-- diminta ke yt-dlp; NULL berarti resolusi tertinggi yang tersedia. Untuk
-- preset video, codec, bitrate_kbps, dan channels berlaku bagi trek audio
-- AAC saat audio sumber harus di-encode ulang; video selalu H.264 supaya
-- berkasnya dapat diputar di mana saja. Lihat planning §11.
ALTER TABLE presets ADD COLUMN kind TEXT NOT NULL DEFAULT 'audio'
  CHECK (kind IN ('audio','video'));
ALTER TABLE presets ADD COLUMN max_height INTEGER
  CHECK (max_height IS NULL OR max_height > 0);

-- Resolusi video tertinggi yang ditawarkan sumber, supaya UI dapat
-- menandai pilihan kualitas yang melebihi sumbernya. NULL pada baris cache
-- lama berarti tidak diketahui; TTL 24 jam mengisinya sendiri.
ALTER TABLE media_items ADD COLUMN video_height INTEGER;

-- sample_rate NULL: audio AAC ikut sample rate sumber, tidak ada alasan
-- me-resample trek yang menyertai video.
INSERT INTO presets
  (id, label, format, codec, mode, bitrate_kbps, vbr_quality, sample_rate, channels, sort_order, kind, max_height)
VALUES
  ('mp4_360',  '360p',  'mp4', 'aac', 'cbr', 128, NULL, NULL, 2, 110, 'video', 360),
  ('mp4_480',  '480p',  'mp4', 'aac', 'cbr', 128, NULL, NULL, 2, 120, 'video', 480),
  ('mp4_720',  '720p',  'mp4', 'aac', 'cbr', 192, NULL, NULL, 2, 130, 'video', 720),
  ('mp4_1080', '1080p', 'mp4', 'aac', 'cbr', 192, NULL, NULL, 2, 140, 'video', 1080),
  ('mp4_best', 'Best',  'mp4', 'aac', 'cbr', 192, NULL, NULL, 2, 150, 'video', NULL);

UPDATE schema_meta SET schema_version = 6 WHERE id = 1;

-- +goose Down
DELETE FROM presets WHERE kind = 'video';
ALTER TABLE media_items DROP COLUMN video_height;
ALTER TABLE presets DROP COLUMN max_height;
ALTER TABLE presets DROP COLUMN kind;
UPDATE schema_meta SET schema_version = 5 WHERE id = 1;
