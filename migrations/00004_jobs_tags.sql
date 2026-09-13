-- +goose Up
-- Judul dan artis yang disunting pengguna sebelum konversi. NULL berarti
-- memakai metadata sumber apa adanya. Disimpan per job, bukan di
-- media_items, karena cache metadata dibagi oleh semua job untuk video yang
-- sama dan dapat kedaluwarsa lalu diambil ulang. Lihat planning §7.
ALTER TABLE jobs ADD COLUMN tag_title TEXT;
ALTER TABLE jobs ADD COLUMN tag_artist TEXT;

UPDATE schema_meta SET schema_version = 4 WHERE id = 1;

-- +goose Down
ALTER TABLE jobs DROP COLUMN tag_artist;
ALTER TABLE jobs DROP COLUMN tag_title;
UPDATE schema_meta SET schema_version = 3 WHERE id = 1;
