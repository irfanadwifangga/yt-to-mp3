package ytdlp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/process"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/tools"
)

func playlistArgs(url string) []string {
	return []string{"--ignore-config", "--no-exec", "--no-cache-dir", "--yes-playlist", "--flat-playlist", "--dump-single-json", "--skip-download", "--ignore-errors", "--no-warnings", "--playlist-end", fmt.Sprint(domain.MaxPlaylistEntries + 1), "--socket-timeout", "20", "--retries", "1", "--", url}
}

func (r *Resolver) ResolvePlaylist(ctx context.Context, rawURL string) (*domain.Playlist, error) {
	url, validation := domain.NormalizePlaylistURL(rawURL)
	if validation != nil {
		return nil, validation
	}
	bin, _, err := r.tools.Resolve(ctx, tools.YTDLP)
	if err != nil {
		return nil, err
	}
	result, err := process.Output(ctx, process.Spec{Bin: bin, Args: playlistArgs(url), Timeout: 90 * time.Second})
	if err != nil {
		return nil, domain.WrapError(domain.CodeTimeout, domain.ClassTransient, "pengambilan playlist gagal", err)
	}
	if result.ExitCode != 0 {
		return nil, mapStderr(result.Stderr, result.ExitCode)
	}
	return parsePlaylist(result.Stdout)
}

func parsePlaylist(data []byte) (*domain.Playlist, error) {
	var raw struct {
		Title   string `json:"title"`
		Entries []*struct {
			ID           string  `json:"id"`
			Title        string  `json:"title"`
			Duration     float64 `json:"duration"`
			Availability string  `json:"availability"`
			IsLive       bool    `json:"is_live"`
			LiveStatus   string  `json:"live_status"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, domain.WrapError(domain.CodeInternal, domain.ClassTransient, "keluaran playlist bukan JSON yang dikenal", err)
	}
	list := &domain.Playlist{Title: raw.Title, Entries: []domain.PlaylistEntry{}, Truncated: len(raw.Entries) > domain.MaxPlaylistEntries}
	for index, entry := range raw.Entries {
		if index >= domain.MaxPlaylistEntries {
			break
		}
		item := domain.PlaylistEntry{Unavailable: true}
		if entry != nil {
			item.Title = entry.Title
			if key, invalid := domain.NormalizeURL("https://www.youtube.com/watch?v=" + entry.ID); invalid == nil {
				item.URL = CanonicalURL(key)
				title := strings.ToLower(entry.Title)
				item.Unavailable = entry.IsLive || entry.LiveStatus == "is_live" || title == "[private video]" || title == "[deleted video]" || (entry.Availability != "" && entry.Availability != "public" && entry.Availability != "unlisted")
			}
			if entry.Duration > 0 && entry.Duration < 9e12 {
				item.DurationMS = int64(entry.Duration * 1000)
			}
		}
		list.Entries = append(list.Entries, item)
	}
	return list, nil
}
