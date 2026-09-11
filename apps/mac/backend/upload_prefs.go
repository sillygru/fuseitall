// SPDX-License-Identifier: AGPL-3.0-only

package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fuseitall/core"
)

// UploadPrefs is Mac-local only: the default phone folder for uploads that
// target the phone home folder, plus whether the home-folder prompt was
// answered with "remember". Never synced to the phone (unlike AppSettings).
type UploadPrefs struct {
	DefaultUploadDir string `json:"default_upload_dir,omitempty"`
}

// UploadPrefsFilePath returns ~/Library/Application Support/FuseItAll/upload_prefs.json.
func UploadPrefsFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, "Library", "Application Support", "FuseItAll", "upload_prefs.json"), nil
}

// LoadUploadPrefs returns persisted prefs, or empty when absent/corrupt.
// Corrupt files read as empty (fail-soft): the home dialog just asks again.
func LoadUploadPrefs() UploadPrefs {
	path, err := UploadPrefsFilePath()
	if err != nil {
		return UploadPrefs{}
	}
	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		return UploadPrefs{}
	}
	var p UploadPrefs
	if err := json.Unmarshal(raw, &p); err != nil {
		return UploadPrefs{}
	}
	if p.DefaultUploadDir == "" {
		return UploadPrefs{}
	}
	if _, ok := core.SanitizeFilePath(p.DefaultUploadDir); !ok {
		return UploadPrefs{}
	}
	return p
}

func storeUploadPrefs(p UploadPrefs) error {
	path, err := UploadPrefsFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create upload prefs dir: %w", err)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("encode upload prefs: %w", err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write upload prefs: %w", err)
	}
	return os.Chmod(path, 0o600)
}

// GetDefaultUploadDir returns the Mac-local default phone upload folder
// ("" = ask every time).
func (s *Service) GetDefaultUploadDir() string {
	return LoadUploadPrefs().DefaultUploadDir
}

// SetDefaultUploadDir stores the Mac-local default. Empty clears it.
// Values are sandboxed rel paths ("Download"); home ("") never persists
// as a default — clearing is the way to ask again.
func (s *Service) SetDefaultUploadDir(dir string) (string, error) {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" {
		if err := storeUploadPrefs(UploadPrefs{}); err != nil {
			return "", err
		}
		s.appendLine("default upload folder cleared")
		return "Upload default cleared. Home uploads will ask again.", nil
	}
	if _, ok := core.SanitizeFilePath(trimmed); !ok {
		return "", errors.New("invalid folder")
	}
	if err := storeUploadPrefs(UploadPrefs{DefaultUploadDir: trimmed}); err != nil {
		return "", err
	}
	s.appendLine("default upload folder set dir=" + trimmed)
	return "Default upload folder set to " + trimmed + ".", nil
}
