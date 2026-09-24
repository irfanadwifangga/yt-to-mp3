package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// MediaTTL adalah umur cache metadata.
//
// Tanpa cache, setiap analisis ulang URL yang sama memanggil yt-dlp lagi dan
// menunggu beberapa detik untuk data yang praktis tidak berubah.
const MediaTTL = 24 * time.Hour

// MediaRepository menyimpan cache metadata sumber.
type MediaRepository struct {
	db  *DB
	now func() time.Time
}

// NewMediaRepository membuat repository metadata.
func NewMediaRepository(d *DB) *MediaRepository {
	return &MediaRepository{db: d, now: func() time.Time { return time.Now().UTC() }}
}

// Upsert menyimpan atau memperbarui metadata beserta waktu pengambilannya.
func (r *MediaRepository) Upsert(ctx context.Context, info *domain.MediaInfo, rawJSON string) error {
	_, err := r.db.Write().ExecContext(ctx, `
		INSERT INTO media_items
			(source_key, title, uploader, duration_ms, thumbnail_url,
			 source_codec, sample_rate, is_live, raw_json, fetched_at,
			 track, artist, album, release_year, video_height)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_key) DO UPDATE SET
			title         = excluded.title,
			uploader      = excluded.uploader,
			duration_ms   = excluded.duration_ms,
			thumbnail_url = excluded.thumbnail_url,
			source_codec  = excluded.source_codec,
			sample_rate   = excluded.sample_rate,
			is_live       = excluded.is_live,
			raw_json      = excluded.raw_json,
			fetched_at    = excluded.fetched_at,
			track         = excluded.track,
			artist        = excluded.artist,
			album         = excluded.album,
			release_year  = excluded.release_year,
			video_height  = excluded.video_height`,
		info.SourceKey, info.Title, nullString(info.Uploader), info.DurationMS,
		nullString(info.ThumbnailURL), nullString(info.SourceCodec),
		nullInt(info.SampleRate), boolToInt(info.IsLive), nullString(rawJSON),
		formatTime(r.now()),
		nullString(info.Track), nullString(info.Artist), nullString(info.Album),
		nullInt(info.ReleaseYear), nullInt(info.VideoHeight))
	if err != nil {
		return fmt.Errorf("simpan metadata: %w", err)
	}
	return nil
}

// Get mengembalikan metadata yang masih segar.
//
// Baris kedaluwarsa diperlakukan seolah tidak ada, bukan dihapus di sini:
// pembersihannya tugas housekeeper, dan memanggil penulisan dari jalur baca
// akan membuat query history ikut antre di belakang penulis.
func (r *MediaRepository) Get(ctx context.Context, sourceKey string) (*domain.MediaInfo, bool, error) {
	var (
		info       domain.MediaInfo
		uploader   sql.NullString
		durationMS sql.NullInt64
		thumbnail  sql.NullString
		codec      sql.NullString
		sampleRate sql.NullInt64
		isLive     int
		fetchedAt  string
		track      sql.NullString
		artist     sql.NullString
		album      sql.NullString
		year       sql.NullInt64
		height     sql.NullInt64
	)

	err := r.db.Read().QueryRowContext(ctx, `
		SELECT source_key, title, uploader, duration_ms, thumbnail_url,
		       source_codec, sample_rate, is_live, fetched_at,
		       track, artist, album, release_year, video_height
		FROM media_items WHERE source_key = ?`, sourceKey).
		Scan(&info.SourceKey, &info.Title, &uploader, &durationMS, &thumbnail,
			&codec, &sampleRate, &isLive, &fetchedAt,
			&track, &artist, &album, &year, &height)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("baca metadata: %w", err)
	}

	fetched, err := parseTime(fetchedAt)
	if err != nil {
		return nil, false, fmt.Errorf("parse fetched_at: %w", err)
	}
	if r.now().Sub(fetched) > MediaTTL {
		return nil, false, nil
	}

	info.Uploader = uploader.String
	info.DurationMS = durationMS.Int64
	info.Duration = time.Duration(durationMS.Int64) * time.Millisecond
	info.ThumbnailURL = thumbnail.String
	info.SourceCodec = codec.String
	info.SampleRate = int(sampleRate.Int64)
	info.IsLive = isLive != 0
	info.Track = track.String
	info.Artist = artist.String
	info.Album = album.String
	info.ReleaseYear = int(year.Int64)
	info.VideoHeight = int(height.Int64)

	return &info, true, nil
}

// PurgeExpired membuang cache kedaluwarsa yang tidak dirujuk job mana pun.
func (r *MediaRepository) PurgeExpired(ctx context.Context) (int, error) {
	cutoff := formatTime(r.now().Add(-MediaTTL))

	res, err := r.db.Write().ExecContext(ctx, `
		DELETE FROM media_items
		WHERE fetched_at < ?
		  AND source_key NOT IN (SELECT source_key FROM jobs)`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("buang cache kedaluwarsa: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("hitung baris terbuang: %w", err)
	}
	return int(n), nil
}

func nullInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
