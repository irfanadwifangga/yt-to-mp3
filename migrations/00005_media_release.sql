-- +goose Up
-- Data rilis musik dari katalog YouTube Music: judul lagu, artis, album,
-- dan tahun rilis. Hanya terisi untuk video yang terhubung ke katalog; NULL
-- untuk unggahan biasa. Baris cache lama tidak diisi ulang di sini: TTL
-- cache 24 jam, jadi data rilis muncul sendiri pada analisis berikutnya.
-- Lihat planning §12.2.
ALTER TABLE media_items ADD COLUMN track TEXT;
ALTER TABLE media_items ADD COLUMN artist TEXT;
ALTER TABLE media_items ADD COLUMN album TEXT;
ALTER TABLE media_items ADD COLUMN release_year INTEGER;

UPDATE schema_meta SET schema_version = 5 WHERE id = 1;

-- +goose Down
ALTER TABLE media_items DROP COLUMN release_year;
ALTER TABLE media_items DROP COLUMN album;
ALTER TABLE media_items DROP COLUMN artist;
ALTER TABLE media_items DROP COLUMN track;
UPDATE schema_meta SET schema_version = 4 WHERE id = 1;
