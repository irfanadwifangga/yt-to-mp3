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
}
