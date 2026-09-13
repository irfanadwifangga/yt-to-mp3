package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
)

// UpdateStateFile menyimpan hasil cek pembaruan terakhir sebagai JSON.
//
// Berkas terpisah dari database karena isinya cache yang boleh hilang:
// menghapusnya hanya membuat cek berikutnya berjalan lebih cepat.
type UpdateStateFile struct {
	path string
}

// NewUpdateStateFile membuat penyimpanan di path tertentu.
func NewUpdateStateFile(path string) *UpdateStateFile {
	return &UpdateStateFile{path: path}
}

// Load membaca keadaan terakhir; berkas yang belum ada bukan kegagalan.
func (f *UpdateStateFile) Load() (application.ToolUpdateState, error) {
	var s application.ToolUpdateState
	raw, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("baca %s: %w", f.path, err)
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		// Berkas rusak diperlakukan seperti belum pernah dicek.
		return application.ToolUpdateState{}, nil
	}
	return s, nil
}

// Save menulis keadaan secara atomik.
func (f *UpdateStateFile) Save(s application.ToolUpdateState) error {
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return fmt.Errorf("tulis %s: %w", tmp, err)
	}
	return os.Rename(tmp, f.path)
}
