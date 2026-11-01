package ytdlp

import (
	"encoding/json"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"slices"
	"testing"
)

func TestPlaylistParsingAndSafety(t *testing.T) {
	list, err := parsePlaylist([]byte(`{"title":"Mix","entries":[{"id":"aaaaaaaaaaa","title":"First","duration":1.25},null,{"id":"bbbbbbbbbbb","title":"[Private video]"},{"id":"ccccccccccc","is_live":true},{"id":"https://evil.test","title":"Bad"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Entries) != 5 || list.Entries[0].DurationMS != 1250 || list.Entries[0].URL != "https://www.youtube.com/watch?v=aaaaaaaaaaa" || list.Entries[0].Unavailable {
		t.Fatalf("unexpected entries: %+v", list)
	}
	for _, entry := range list.Entries[1:] {
		if !entry.Unavailable {
			t.Fatal("unavailable entry was selectable")
		}
	}
	args := playlistArgs("https://www.youtube.com/playlist?list=PL_test")
	for _, flag := range []string{"--ignore-config", "--no-exec", "--flat-playlist", "--yes-playlist", "--skip-download"} {
		if !slices.Contains(args, flag) {
			t.Error("missing", flag)
		}
	}
	if args[len(args)-2] != "--" || args[slices.Index(args, "--playlist-end")+1] != "501" {
		t.Fatal("missing URL or entry boundary")
	}
}

func TestPlaylistTruncationIsExplicit(t *testing.T) {
	entries := make([]map[string]string, domain.MaxPlaylistEntries+1)
	for i := range entries {
		entries[i] = map[string]string{"id": "aaaaaaaaaaa", "title": "Entry"}
	}
	data, _ := json.Marshal(map[string]any{"entries": entries})
	list, err := parsePlaylist(data)
	if err != nil || !list.Truncated || len(list.Entries) != domain.MaxPlaylistEntries {
		t.Fatalf("bad truncation: %+v %v", list, err)
	}
}
