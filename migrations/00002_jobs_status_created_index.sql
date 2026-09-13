-- +goose Up
-- Riwayat difilter status diurutkan created_at DESC, id DESC. Dengan indeks
-- status saja, SQLite mengambil seluruh baris berstatus itu lalu
-- mengurutkannya di memori; pada 10.000 job terukur p95 38 ms, terlalu dekat
-- dengan target 50 ms. Indeks komposit memberi urutan langsung sehingga
-- query berhenti setelah LIMIT terpenuhi.
--
-- idx_jobs_status dibuang karena kolom depannya sudah dicakup indeks baru;
-- mempertahankannya hanya menambah biaya tulis setiap transisi status.
CREATE INDEX idx_jobs_status_created ON jobs(status, created_at DESC, id DESC);
DROP INDEX idx_jobs_status;

UPDATE schema_meta SET schema_version = 2 WHERE id = 1;

-- +goose Down
CREATE INDEX idx_jobs_status ON jobs(status);
DROP INDEX idx_jobs_status_created;
UPDATE schema_meta SET schema_version = 1 WHERE id = 1;
