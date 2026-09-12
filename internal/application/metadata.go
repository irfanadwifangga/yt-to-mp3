package application

import (
	"context"
	"log/slog"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// MediaCache menyimpan hasil metadata agar analisis berulang tidak
// memanggil yt-dlp lagi.
type MediaCache interface {
	Get(ctx context.Context, sourceKey string) (*domain.MediaInfo, bool, error)
	Upsert(ctx context.Context, info *domain.MediaInfo, rawJSON string) error
}

// PresetLister membaca definisi preset.
type PresetLister interface {
	List(ctx context.Context, includeDeprecated bool) ([]domain.Preset, error)
	Get(ctx context.Context, id string) (*domain.Preset, error)
}

// MetadataService menganalisis URL tanpa mengunduh media.
type MetadataService struct {
	resolver MediaResolver
	cache    MediaCache
	log      *slog.Logger
}

// NewMetadataService membuat use case metadata.
func NewMetadataService(r MediaResolver, c MediaCache, log *slog.Logger) *MetadataService {
	return &MetadataService{resolver: r, cache: c, log: log}
}

// Analyze menormalkan URL lalu mengembalikan metadatanya.
//
// Validasi selalu terjadi sebelum apa pun menyentuh yt-dlp, sehingga skema
// seperti file:// tidak pernah sampai ke subprocess.
func (s *MetadataService) Analyze(ctx context.Context, rawURL string) (*domain.MediaInfo, error) {
	sourceKey, derr := domain.NormalizeURL(rawURL)
	if derr != nil {
		return nil, derr
	}

	// Cache hanya percepatan. Kegagalan membacanya tidak boleh menggagalkan
	// analisis, cukup dicatat lalu jatuh ke pemanggilan tool.
	if info, ok, err := s.cache.Get(ctx, sourceKey); err != nil {
		s.log.Warn("baca cache metadata gagal", "source_key", sourceKey, "error", err)
	} else if ok {
		return info, nil
	}

	info, err := s.resolver.Resolve(ctx, sourceKey)
	if err != nil {
		return nil, err
	}

	if err := s.cache.Upsert(ctx, info, ""); err != nil {
		s.log.Warn("simpan cache metadata gagal", "source_key", sourceKey, "error", err)
	}
	return info, nil
}
