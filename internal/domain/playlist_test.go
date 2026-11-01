package domain

import "testing"

func TestNormalizePlaylistURL(t *testing.T) {
	for _, raw := range []string{"https://youtube.com/playlist?list=PL_test-123", "https://www.youtube.com/watch?v=aaaaaaaaaaa&list=PL_test-123&start_radio=1", "https://youtu.be/aaaaaaaaaaa?list=PL_test-123"} {
		got, err := NormalizePlaylistURL(raw)
		if err != nil || got != "https://www.youtube.com/playlist?list=PL_test-123" {
			t.Fatalf("%q => %q, %v", raw, got, err)
		}
	}
	for _, raw := range []string{"file://youtube.com/playlist?list=PL_test-123", "https://evil.test/playlist?list=PL_test-123", "https://youtube.com/playlist?list=../../secret", "https://youtube.com/watch?v=aaaaaaaaaaa", "https://youtube.com/playlist?list=x"} {
		if _, err := NormalizePlaylistURL(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}
