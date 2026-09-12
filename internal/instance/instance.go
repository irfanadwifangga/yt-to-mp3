// Package instance menjaga agar hanya ada satu proses aplikasi yang aktif.
//
// Deteksi tidak memakai lock file, melainkan menyelidiki instance lama lewat
// endpoint publik /api/ping. Pendekatan ini hanya memakai stdlib, bekerja sama
// di seluruh OS, dan sekaligus memberi tahu URL yang harus dibuka. File yang
// ditinggalkan proses mati otomatis dianggap basi karena port-nya tidak
// menjawab. Lihat docs architecture "Lifecycle aplikasi dan single instance".
package instance

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/version"
)

// Info adalah isi runtime.json.
type Info struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	Token     string    `json:"token"`
	StartedAt time.Time `json:"started_at"`
	Version   string    `json:"version"`
}

// URL mengembalikan alamat pembuka lengkap dengan session token.
func (i Info) URL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/?token=%s", i.Port, i.Token)
}

// probeTimeout sengaja pendek: kita hanya menyentuh loopback.
const probeTimeout = 700 * time.Millisecond

// FindRunning melaporkan instance lain yang masih hidup, bila ada.
func FindRunning(path string) (Info, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Info{}, false
	}
	var info Info
	if err := json.Unmarshal(raw, &info); err != nil || info.Port == 0 {
		return Info{}, false
	}

	client := &http.Client{Timeout: probeTimeout}
	url := fmt.Sprintf("http://127.0.0.1:%d/api/ping", info.Port)
	resp, err := client.Get(url)
	if err != nil {
		return Info{}, false // port tidak menjawab, file basi
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return Info{}, false
	}
	var pong struct {
		App string `json:"app"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pong); err != nil {
		return Info{}, false
	}
	if pong.App != version.AppName {
		return Info{}, false // port dipakai aplikasi lain
	}
	return info, true
}

// Write menyimpan runtime.json secara atomik.
func Write(path string, info Info) error {
	raw, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Remove menghapus runtime.json saat shutdown bersih.
func Remove(path string) error {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
