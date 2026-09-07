// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fuseitall/core"
)

// AppSettings is the Mac's app settings: clipboard sync direction plus the
// notification master switch. UpdatedUnix/UpdatedBy implement
// last-writer-wins against the phone's blob (ties go to mac). Persisted in
// settings.json so a restart keeps the last choice.
type AppSettings struct {
	ClipboardMode        string `json:"clipboard_mode"`
	NotificationsEnabled bool   `json:"notifications_enabled"`
	UpdatedUnix          int64  `json:"updated_unix"`
	UpdatedBy            string `json:"updated_by"`
}

// ClipboardModeLabel returns the short consumer label plus arrow glyph for
// a mode: Off (∅), Mac→Phone (→), Phone→Mac (←), Two-way (⇄). Pure.
func ClipboardModeLabel(mode string) string {
	switch mode {
	case core.ClipboardMacToPhone:
		return "Mac → Phone"
	case core.ClipboardPhoneToMac:
		return "Phone → Mac"
	case core.ClipboardTwoWay:
		return "Two-way ⇄"
	default:
		return "Off ∅"
	}
}

// DefaultAppSettings returns first-launch defaults: two-way clipboard,
// notifications on, stamped now by mac.
func DefaultAppSettings() AppSettings {
	return AppSettings{
		ClipboardMode:        core.ClipboardTwoWay,
		NotificationsEnabled: true,
		UpdatedUnix:          time.Now().Unix(),
		UpdatedBy:            core.OriginMac,
	}
}

// SettingsFilePath returns ~/Library/Application Support/FuseItAll/settings.json.
func SettingsFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, "Library", "Application Support", "FuseItAll", "settings.json"), nil
}

// LoadAppSettings returns the persisted settings, or has=false when absent.
// A corrupt file returns an error; callers fall back to defaults (pairing
// still works, there is just no remembered choice).
func LoadAppSettings() (AppSettings, bool, error) {
	path, err := SettingsFilePath()
	if err != nil {
		return AppSettings{}, false, err
	}
	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return AppSettings{}, false, nil
		}
		return AppSettings{}, false, fmt.Errorf("read app settings: %w", rerr)
	}
	st, err := decodeAppSettings(raw)
	if err != nil {
		return AppSettings{}, false, err
	}
	return st, true, nil
}

// decodeAppSettings validates the on-disk shape. Pure. Unknown modes fail
// closed (error); missing notifications_enabled defaults true so pre-toggle
// files keep mirroring.
func decodeAppSettings(raw []byte) (AppSettings, error) {
	var st AppSettings
	if err := json.Unmarshal(raw, &st); err != nil {
		return AppSettings{}, fmt.Errorf("decode app settings: %w", err)
	}
	if !core.ValidClipboardMode(st.ClipboardMode) {
		return AppSettings{}, fmt.Errorf("unknown clipboard mode %q", st.ClipboardMode)
	}
	if st.UpdatedUnix < 0 {
		return AppSettings{}, fmt.Errorf("settings timestamp must not be negative")
	}
	st.ClipboardMode = strings.TrimSpace(st.ClipboardMode)
	st.UpdatedBy = core.NormalizeUpdatedBy(st.UpdatedBy)
	return st, nil
}

// StoreAppSettings writes settings with 0600 file mode (dir 0700).
func StoreAppSettings(st AppSettings) error {
	path, err := SettingsFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create settings dir: %w", err)
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("encode app settings: %w", err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write app settings: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod app settings: %w", err)
	}
	return nil
}

// SettingsStore owns the Mac settings plus one pending outbound sync slot
// (latest-wins). All methods are safe for concurrent use.
type SettingsStore struct {
	mu      sync.Mutex
	cur     AppSettings
	pending bool
}

// NewSettingsStore loads persisted settings best-effort, else defaults.
func NewSettingsStore() *SettingsStore {
	if st, ok, err := LoadAppSettings(); err == nil && ok {
		return &SettingsStore{cur: st}
	}
	return &SettingsStore{cur: DefaultAppSettings()}
}

// Get returns a copy of the current settings.
func (s *SettingsStore) Get() AppSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

// SetMode stores a new clipboard direction, stamps now/mac, marks pending.
// Unknown modes fail closed without touching state.
func (s *SettingsStore) SetMode(mode string) (AppSettings, error) {
	if !core.ValidClipboardMode(mode) {
		return AppSettings{}, fmt.Errorf("unknown clipboard mode %q", mode)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cur.ClipboardMode = mode
	s.cur.UpdatedUnix = time.Now().Unix()
	s.cur.UpdatedBy = core.OriginMac
	s.pending = true
	return s.cur, nil
}

// SetNotificationsEnabled stores the master switch, stamps now/mac.
func (s *SettingsStore) SetNotificationsEnabled(enabled bool) (AppSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cur.NotificationsEnabled = enabled
	s.cur.UpdatedUnix = time.Now().Unix()
	s.cur.UpdatedBy = core.OriginMac
	s.pending = true
	return s.cur, nil
}

// ApplyRemote adopts an incoming settings blob when it wins
// (core.RemoteSettingsWins). Returns true when adopted. Older blobs are
// dropped so offline edits cannot rewind newer choices.
func (s *SettingsStore) ApplyRemote(remote core.SettingsSyncPayload) bool {
	sanitized, ok := core.SanitizeSettings(remote)
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	local := core.SettingsSyncPayload{
		ClipboardMode: s.cur.ClipboardMode,
		UpdatedUnix:   s.cur.UpdatedUnix,
		UpdatedBy:     s.cur.UpdatedBy,
	}
	if !core.RemoteSettingsWins(local, sanitized) {
		return false
	}
	s.cur.ClipboardMode = sanitized.ClipboardMode
	if sanitized.NotificationsEnabled != nil {
		s.cur.NotificationsEnabled = *sanitized.NotificationsEnabled
	}
	s.cur.UpdatedUnix = sanitized.UpdatedUnix
	s.cur.UpdatedBy = core.NormalizeUpdatedBy(sanitized.UpdatedBy)
	// Adopted remote state is clean: no need to echo it back.
	s.pending = false
	return true
}

// TakePending returns the current blob and clears the pending flag. False
// when nothing needs sending.
func (s *SettingsStore) TakePending() (AppSettings, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.pending {
		return AppSettings{}, false
	}
	s.pending = false
	return s.cur, true
}

// HasPending reports whether a sync is queued.
func (s *SettingsStore) HasPending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending
}

// persistSnapshot writes the current settings best-effort; failures are for
// the caller to log (never fail the setting change itself).
func (s *SettingsStore) persistSnapshot() error {
	return StoreAppSettings(s.Get())
}
