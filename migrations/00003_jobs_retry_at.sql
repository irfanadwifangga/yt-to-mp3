-- +goose Up
-- Auto-retry mengembalikan job yang gagal sementara ke antrean dengan jeda.
-- retry_at menahan job itu sampai jedanya lewat; NULL berarti boleh diambil
-- kapan saja. Disimpan di database, bukan timer di memori, supaya jeda
-- tetap dihormati walau aplikasi dibuka ulang. Lihat planning §19.
ALTER TABLE jobs ADD COLUMN retry_at TEXT;

-- Riwayat diurutkan created_at DESC, id DESC. Indeks created_at saja
-- memaksa SQLite mengurutkan ulang baris bertimestamp sama berdasarkan id;
-- indeks yang memuat keduanya memberi urutan penuh sehingga daftar riwayat
-- berhenti tepat setelah LIMIT.
CREATE INDEX idx_jobs_created_id ON jobs(created_at DESC, id DESC);
DROP INDEX idx_jobs_created;

UPDATE schema_meta SET schema_version = 3 WHERE id = 1;

-- +goose Down
CREATE INDEX idx_jobs_created ON jobs(created_at DESC);
DROP INDEX idx_jobs_created_id;
ALTER TABLE jobs DROP COLUMN retry_at;
UPDATE schema_meta SET schema_version = 2 WHERE id = 1;
