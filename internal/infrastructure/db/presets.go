package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// PresetRepository membaca definisi preset.
//
// Hanya operasi baca yang disediakan: tabel presets diisi lewat migrasi dan
// bersifat append-only, karena preset_id tersimpan permanen di history
// (ADR-028).
type PresetRepository struct {
	db *DB
}

// NewPresetRepository membuat repository preset.
func NewPresetRepository(d *DB) *PresetRepository {
	return &PresetRepository{db: d}
}

const presetColumns = `id, label, kind, format, codec, mode, bitrate_kbps, vbr_quality,
	sample_rate, channels, max_height, passthrough, extra_args, sort_order, deprecated`

// List mengembalikan preset terurut. Preset usang disembunyikan dari UI
// tetapi tetap dapat di-resolve lewat Get, supaya history lama tidak yatim.
func (r *PresetRepository) List(ctx context.Context, includeDeprecated bool) ([]domain.Preset, error) {
	query := `SELECT ` + presetColumns + ` FROM presets`
	if !includeDeprecated {
		query += ` WHERE deprecated = 0`
	}
	query += ` ORDER BY sort_order ASC`

	rows, err := r.db.Read().QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query preset: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Preset
	for rows.Next() {
		p, err := scanPreset(rows)
		if err != nil {
			return nil, fmt.Errorf("scan preset: %w", err)
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// Get mengambil satu preset, termasuk yang sudah usang.
func (r *PresetRepository) Get(ctx context.Context, id string) (*domain.Preset, error) {
	row := r.db.Read().QueryRowContext(ctx,
		`SELECT `+presetColumns+` FROM presets WHERE id = ?`, id)

	p, err := scanPreset(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.NewError(domain.CodeInternal, domain.ClassLocal,
			fmt.Sprintf("preset %s tidak ada", id))
	}
	if err != nil {
		return nil, fmt.Errorf("baca preset: %w", err)
	}
	return p, nil
}

func scanPreset(s scanner) (*domain.Preset, error) {
	var (
		p           domain.Preset
		bitrate     sql.NullInt64
		vbrQuality  sql.NullInt64
		sampleRate  sql.NullInt64
		maxHeight   sql.NullInt64
		passthrough int
		deprecated  int
	)

	err := s.Scan(&p.ID, &p.Label, &p.Kind, &p.Format, &p.Codec, &p.Mode,
		&bitrate, &vbrQuality, &sampleRate, &p.Channels, &maxHeight, &passthrough,
		&p.ExtraArgs, &p.SortOrder, &deprecated)
	if err != nil {
		return nil, err
	}

	if bitrate.Valid {
		v := int(bitrate.Int64)
		p.BitrateKbps = &v
	}
	if vbrQuality.Valid {
		v := int(vbrQuality.Int64)
		p.VBRQuality = &v
	}
	// sample_rate NULL berarti ikut sumber; hanya dipakai preset lossless.
	if sampleRate.Valid {
		v := int(sampleRate.Int64)
		p.SampleRate = &v
	}
	// max_height NULL berarti resolusi tertinggi yang tersedia.
	if maxHeight.Valid {
		v := int(maxHeight.Int64)
		p.MaxHeight = &v
	}
	p.Passthrough = passthrough != 0
	p.Deprecated = deprecated != 0

	return &p, nil
}
