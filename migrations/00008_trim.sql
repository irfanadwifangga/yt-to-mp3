-- +goose Up
ALTER TABLE media_items ADD COLUMN source_formats TEXT NOT NULL DEFAULT '[]';
ALTER TABLE jobs ADD COLUMN trim_start_ms INTEGER;
ALTER TABLE jobs ADD COLUMN trim_end_ms INTEGER CHECK (
 (trim_start_ms IS NULL AND trim_end_ms IS NULL) OR
 (trim_start_ms IS NOT NULL AND trim_end_ms IS NOT NULL AND trim_start_ms >= 0 AND trim_end_ms > trim_start_ms));
DROP INDEX idx_jobs_active_dedup;
CREATE UNIQUE INDEX idx_jobs_active_dedup ON jobs(source_key, preset_id, COALESCE(trim_start_ms, -1), COALESCE(trim_end_ms, -1))
 WHERE status NOT IN ('completed','failed','cancelled');
-- +goose Down
ALTER TABLE media_items DROP COLUMN source_formats;
-- A downgrade can fail if distinct active trims share a source and preset.
DROP INDEX idx_jobs_active_dedup;
ALTER TABLE jobs DROP COLUMN trim_end_ms;
ALTER TABLE jobs DROP COLUMN trim_start_ms;
CREATE UNIQUE INDEX idx_jobs_active_dedup ON jobs(source_key, preset_id) WHERE status NOT IN ('completed','failed','cancelled');
