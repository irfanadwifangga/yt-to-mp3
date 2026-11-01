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

func TestMixPreservesSeedVideo(t *testing.T) {
	want := "https://www.youtube.com/watch?v=u9z--aIMHhE&list=RDu9z--aIMHhE"
	for _, raw := range []string{want + "&index=1", "https://www.youtube.com/playlist?list=RDu9z--aIMHhE", "https://youtu.be/u9z--aIMHhE?list=RDu9z--aIMHhE"} {
		got, err := NormalizePlaylistURL(raw)
		if err != nil || got != want {
			t.Fatalf("%q => %q, %v", raw, got, err)
		}
	}
	if _, err := NormalizePlaylistURL("https://www.youtube.com/playlist?list=RDCLAK5uy_test"); err == nil || err.Code != CodePlaylistSeedRequired {
		t.Fatalf("missing seed should be actionable: %v", err)
	}
}
