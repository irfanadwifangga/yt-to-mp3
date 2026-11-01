package domain

import (
	"net/url"
	"regexp"
	"strings"
)

const MaxPlaylistEntries = 500

var playlistIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{2,128}$`)

type PlaylistEntry struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	DurationMS  int64  `json:"duration_ms"`
	Unavailable bool   `json:"unavailable"`
}

type Playlist struct {
	Title     string          `json:"title"`
	Entries   []PlaylistEntry `json:"entries"`
	Truncated bool            `json:"truncated"`
}

// Only a canonical YouTube playlist URL may reach the resolver subprocess.
func NormalizePlaylistURL(raw string) (string, *Error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil {
		return "", NewError(CodeInvalidURL, ClassPermanent, "URL playlist tidak valid")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", NewError(CodeInvalidURL, ClassPermanent, "skema URL playlist ditolak")
	}
	if !supportedHosts[strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")] {
		return "", NewError(CodeUnsupportedURL, ClassPermanent, "host playlist tidak didukung")
	}
	id := u.Query().Get("list")
	if !playlistIDPattern.MatchString(id) {
		return "", NewError(CodeInvalidURL, ClassPermanent, "id playlist tidak valid")
	}
	if strings.HasPrefix(id, "RD") {
		seed := ""
		if key, invalid := NormalizeURL(raw); invalid == nil {
			seed = VideoID(key)
		}
		if seed == "" && len(id) == 13 && videoIDPattern.MatchString(id[2:]) {
			seed = id[2:]
		}
		if seed == "" {
			return "", NewError(CodePlaylistSeedRequired, ClassPermanent, "Mix YouTube memerlukan video asal")
		}
		return "https://www.youtube.com/watch?v=" + seed + "&list=" + id, nil
	}
	return "https://www.youtube.com/playlist?list=" + id, nil
}
