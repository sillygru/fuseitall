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
	"sync"
	"time"

	"fuseitall/core"
)

// AppSettings is the Mac's app settings: notification master switch +
// per-app filter mode/lists + clipboard mode + playback direction/output.
// UpdatedUnix/UpdatedBy implement last-writer-wins against the phone's blob
// (ties go to mac). Persisted in settings.json so a restart keeps the last choice.
// ClipboardAllowSensitive opts in to auto-syncing OS-flagged secrets
// (default false: auto skips loud, manual Send always bypasses).
type AppSettings struct {
	NotificationsEnabled    bool     `json:"notifications_enabled"`
	NotifMode               string   `json:"notif_mode"`
	MutedPackages           []string `json:"muted_packages,omitempty"`
	AllowedPackages         []string `json:"allowed_packages,omitempty"`
	ClipboardMode           string   `json:"clipboard_mode"`
	ClipboardAllowSensitive bool     `json:"clipboard_allow_sensitive,omitempty"`
	PlaybackMode            string   `json:"playback_mode"`
	PlaybackOutput          string   `json:"playback_output"`
	UpdatedUnix             int64    `json:"updated_unix"`
	UpdatedBy               string   `json:"updated_by"`
}

// DefaultAppSettings returns first-launch defaults: notifications on,
// filter allow-all, clipboard both, sensitive auto off, playback both +
// in-app output, stamped now by mac.
func DefaultAppSettings() AppSettings {
	return AppSettings{
		NotificationsEnabled: true,
		NotifMode:            core.NotifAllExceptMuted,
		ClipboardMode:        core.ClipboardBoth,
		PlaybackMode:         core.PlaybackBoth,
		PlaybackOutput:       core.PlaybackOutputInApp,
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

// decodeAppSettings validates the on-disk shape. Pure. Unknown fields are
// ignored for forward compat; missing notifications_enabled defaults true,
// missing clipboard_mode defaults "both", missing clipboard_allow_sensitive
// defaults false, missing notif filter defaults allow-all, missing
// playback_mode defaults both and missing playback_output defaults inapp.
func decodeAppSettings(raw []byte) (AppSettings, error) {
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawMap); err != nil {
		return AppSettings{}, fmt.Errorf("decode app settings: %w", err)
	}
	var st AppSettings
	if err := json.Unmarshal(raw, &st); err != nil {
		return AppSettings{}, fmt.Errorf("decode app settings: %w", err)
	}
	if st.UpdatedUnix < 0 {
		return AppSettings{}, fmt.Errorf("settings timestamp must not be negative")
	}
	if _, ok := rawMap["notifications_enabled"]; !ok {
		st.NotificationsEnabled = true
	}
	if _, ok := rawMap["clipboard_mode"]; !ok || st.ClipboardMode == "" {
		st.ClipboardMode = core.ClipboardBoth
	} else {
		st.ClipboardMode = core.NormalizeClipboardMode(st.ClipboardMode)
	}
	if _, ok := rawMap["notif_mode"]; !ok || st.NotifMode == "" {
		st.NotifMode = core.NotifAllExceptMuted
	} else {
		st.NotifMode = core.NormalizeNotifMode(st.NotifMode)
	}
	if _, ok := rawMap["playback_mode"]; !ok || st.PlaybackMode == "" {
		st.PlaybackMode = core.PlaybackBoth
	} else {
		st.PlaybackMode = core.NormalizePlaybackMode(st.PlaybackMode)
	}
	if _, ok := rawMap["playback_output"]; !ok || st.PlaybackOutput == "" {
		st.PlaybackOutput = core.PlaybackOutputInApp
	} else {
		st.PlaybackOutput = core.NormalizePlaybackOutput(st.PlaybackOutput)
	}
	// clipboard_allow_sensitive absent means false; no normalization needed.
	st.MutedPackages = core.SanitizeNotifFilterList(st.MutedPackages)
	st.AllowedPackages = core.SanitizeNotifFilterList(st.AllowedPackages)
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

// SetClipboardMode stores the clipboard auto direction, stamps now/mac.
func (s *SettingsStore) SetClipboardMode(mode string) (AppSettings, error) {
	norm := core.NormalizeClipboardMode(mode)
	if !core.IsValidClipboardMode(norm) {
		return AppSettings{}, fmt.Errorf("unknown clipboard mode %q", mode)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cur.ClipboardMode = norm
	s.cur.UpdatedUnix = time.Now().Unix()
	s.cur.UpdatedBy = core.OriginMac
	s.pending = true
	return s.cur, nil
}

// SetClipboardAllowSensitive stores the sensitive auto-sync opt-in, stamps
// now/mac. Auto watchers skip concealed content unless true; manual Send
// always bypasses.
func (s *SettingsStore) SetClipboardAllowSensitive(allow bool) (AppSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cur.ClipboardAllowSensitive = allow
	s.cur.UpdatedUnix = time.Now().Unix()
	s.cur.UpdatedBy = core.OriginMac
	s.pending = true
	return s.cur, nil
}

// SetNotifMode stores the per-app filter mode, stamps now/mac.
func (s *SettingsStore) SetNotifMode(mode string) (AppSettings, error) {
	norm := core.NormalizeNotifMode(mode)
	if !core.IsValidNotifMode(norm) {
		return AppSettings{}, fmt.Errorf("unknown notification filter mode %q", mode)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cur.NotifMode = norm
	s.cur.UpdatedUnix = time.Now().Unix()
	s.cur.UpdatedBy = core.OriginMac
	s.pending = true
	return s.cur, nil
}

// SetPlaybackMode stores the playback sync direction, stamps now/mac.
// Modes: both, android_to_mac, mac_to_android, disabled.
func (s *SettingsStore) SetPlaybackMode(mode string) (AppSettings, error) {
	norm := core.NormalizePlaybackMode(mode)
	if !core.IsValidPlaybackMode(norm) {
		return AppSettings{}, fmt.Errorf("unknown playback mode %q", mode)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cur.PlaybackMode = norm
	s.cur.UpdatedUnix = time.Now().Unix()
	s.cur.UpdatedBy = core.OriginMac
	s.pending = true
	return s.cur, nil
}

// SetPlaybackOutput stores the Mac presentation output, stamps now/mac.
// Outputs: inapp, system.
func (s *SettingsStore) SetPlaybackOutput(output string) (AppSettings, error) {
	norm := core.NormalizePlaybackOutput(output)
	if !core.IsValidPlaybackOutput(norm) {
		return AppSettings{}, fmt.Errorf("unknown playback output %q", output)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cur.PlaybackOutput = norm
	s.cur.UpdatedUnix = time.Now().Unix()
	s.cur.UpdatedBy = core.OriginMac
	s.pending = true
	return s.cur, nil
}

// SetAppMuted adds (muted=true) or removes (muted=false) a package from the
// denylist, stamps now/mac. Unknown packages are sanitized fail-soft.
func (s *SettingsStore) SetAppMuted(pkg string, muted bool) (AppSettings, error) {
	clean := core.SanitizePackageName(pkg)
	if clean == "" {
		return AppSettings{}, fmt.Errorf("unknown app package %q", pkg)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := make([]string, 0, len(s.cur.MutedPackages))
	for _, p := range s.cur.MutedPackages {
		if p != clean {
			kept = append(kept, p)
		}
	}
	if muted {
		if len(kept) >= core.MaxNotifFilterApps {
			return AppSettings{}, fmt.Errorf("muted app list is full")
		}
		kept = core.SanitizeNotifFilterList(append(kept, clean))
	}
	s.cur.MutedPackages = kept
	s.cur.UpdatedUnix = time.Now().Unix()
	s.cur.UpdatedBy = core.OriginMac
	s.pending = true
	return s.cur, nil
}

// SetAppAllowed adds (allowed=true) or removes (allowed=false) a package
// from the allowlist (used in only_allowed mode), stamps now/mac.
func (s *SettingsStore) SetAppAllowed(pkg string, allowed bool) (AppSettings, error) {
	clean := core.SanitizePackageName(pkg)
	if clean == "" {
		return AppSettings{}, fmt.Errorf("unknown app package %q", pkg)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := make([]string, 0, len(s.cur.AllowedPackages))
	for _, p := range s.cur.AllowedPackages {
		if p != clean {
			kept = append(kept, p)
		}
	}
	if allowed {
		if len(kept) >= core.MaxNotifFilterApps {
			return AppSettings{}, fmt.Errorf("allowed app list is full")
		}
		kept = core.SanitizeNotifFilterList(append(kept, clean))
	}
	s.cur.AllowedPackages = kept
	s.cur.UpdatedUnix = time.Now().Unix()
	s.cur.UpdatedBy = core.OriginMac
	s.pending = true
	return s.cur, nil
}

// ApplyRemote adopts an incoming settings blob when it wins
// (core.RemoteSettingsWins). Returns true when adopted.
func (s *SettingsStore) ApplyRemote(remote core.SettingsSyncPayload) bool {
	sanitized, ok := core.SanitizeSettings(remote)
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	local := core.SettingsSyncPayload{
		UpdatedUnix: s.cur.UpdatedUnix,
		UpdatedBy:   s.cur.UpdatedBy,
	}
	if !core.RemoteSettingsWins(local, sanitized) {
		return false
	}
	if sanitized.NotificationsEnabled != nil {
		s.cur.NotificationsEnabled = *sanitized.NotificationsEnabled
	}
	s.cur.ClipboardMode = sanitized.ClipboardMode
	s.cur.NotifMode = sanitized.NotifMode
	s.cur.MutedPackages = sanitized.MutedPackages
	s.cur.AllowedPackages = sanitized.AllowedPackages
	s.cur.PlaybackMode = sanitized.PlaybackMode
	s.cur.PlaybackOutput = sanitized.PlaybackOutput
	if sanitized.ClipboardAllowSensitive != nil {
		s.cur.ClipboardAllowSensitive = *sanitized.ClipboardAllowSensitive
	}
	s.cur.UpdatedUnix = sanitized.UpdatedUnix
	s.cur.UpdatedBy = core.NormalizeUpdatedBy(sanitized.UpdatedBy)
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
