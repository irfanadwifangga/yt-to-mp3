package domain

import "time"

// MediaInfo adalah metadata sumber yang sudah dinormalkan dari keluaran
// yt-dlp. Field yang tidak diketahui dibiarkan kosong, bukan diisi sentinel.
type MediaInfo struct {
	SourceKey    string        `json:"source_key"`
	SourceURL    string        `json:"source_url"`
	Title        string        `json:"title"`
	Uploader     string        `json:"uploader"`
	Duration     time.Duration `json:"-"`
	DurationMS   int64         `json:"duration_ms"`
	ThumbnailURL string        `json:"thumbnail_url"`
	SourceCodec  string        `json:"source_codec"`
	SampleRate   int           `json:"sample_rate"`
	IsLive       bool          `json:"is_live"`

	// Data rilis dari katalog YouTube Music. Hanya terisi untuk video yang
	// terhubung ke katalog musik; kosong untuk unggahan biasa. Tahun unggah
	// sengaja tidak dipakai sebagai pengganti ReleaseYear: video lagu tahun
	// 1975 yang diunggah pada 2008 bukan rilisan 2008.
	Track       string `json:"track,omitempty"`
	Artist      string `json:"artist,omitempty"`
	Album       string `json:"album,omitempty"`
	ReleaseYear int    `json:"release_year,omitempty"`
}
