// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"encoding/json"
	"sync"

	"fuseitall/core"
)

// ClipNotice is the typed clipboard state for the frontend. Text is the
// full current value (UI truncates for preview); ChangedUnix orders pushes;
// Origin is "mac" or "android". HasText is false when no copy exists yet.
// Clipboard bodies never reach the log (lengths only).
type ClipNotice struct {
	HasText     bool
	Text        string
	ChangedUnix int64
	Origin      string
	Preview     string
	Pending     bool
}

// ClipStore owns the latest clipboard text plus one pending outbound push
// (latest-wins). In-memory only: clipboard contents never touch disk.
// Safe for concurrent use.
type ClipStore struct {
	mu        sync.Mutex
	text      string
	changedAt int64
	origin    string
	has       bool
	pending   bool
}

// NewClipStore returns an empty clipboard.
func NewClipStore() *ClipStore {
	return &ClipStore{}
}

// previewOf truncates text to 200 chars for list rows. Pure.
func previewOf(s string) string {
	const max = 200
	runes := []rune(s)
	if len(runes) > max {
		return string(runes[:max])
	}
	return s
}

// Get returns a copy of the current clipboard state.
func (s *ClipStore) Get() ClipNotice {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ClipNotice{
		HasText:     s.has,
		Text:        s.text,
		ChangedUnix: s.changedAt,
		Origin:      s.origin,
		Preview:     previewOf(s.text),
		Pending:     s.pending,
	}
}

// SetLocal records a Mac-side copy: stamps origin mac, marks pending.
// Empty text clears. Over-long input fails closed without touching state.
func (s *ClipStore) SetLocal(text string, changedAt int64) (ClipNotice, bool) {
	if _, ok := core.SanitizeClipText(text); !ok {
		return ClipNotice{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Identical text is a no-op: no pending churn, no re-push loops.
	if s.has && s.text == text {
		return ClipNotice{
			HasText: true, Text: s.text, ChangedUnix: s.changedAt,
			Origin: s.origin, Preview: previewOf(s.text), Pending: s.pending,
		}, true
	}
	s.text, s.changedAt, s.origin, s.has, s.pending = text, changedAt, core.OriginMac, true, true
	return ClipNotice{
		HasText: true, Text: text, ChangedUnix: changedAt,
		Origin: core.OriginMac, Preview: previewOf(text), Pending: true,
	}, true
}

// ApplyRemote adopts an incoming clip-push when it is newer
// (core.RemoteClipWins) and echoes are suppressed via origin check.
// Returns true when adopted.
func (s *ClipStore) ApplyRemote(p core.ClipPushPayload) bool {
	if _, ok := core.SanitizeClipText(p.Text); !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !core.RemoteClipWins(s.changedAt, p.ChangedAt) {
		return false
	}
	origin := core.NormalizeOrigin(p.Origin)
	if origin == "" {
		origin = core.OriginAndroid
	}
	// Echo of our own push must not be adopted.
	if origin == core.OriginMac {
		return false
	}
	s.text, s.changedAt, s.origin, s.has = p.Text, p.ChangedAt, origin, true
	// Adopted remote text must not bounce back.
	s.pending = false
	return true
}

// TakePending returns the current text for upload and clears the flag.
// False when nothing needs sending.
func (s *ClipStore) TakePending() (text string, changedAt int64, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.pending || !s.has {
		return "", 0, false
	}
	s.pending = false
	return s.text, s.changedAt, true
}

// HasPending reports whether a push is queued.
func (s *ClipStore) HasPending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending && s.has
}

// ParseClipPush extracts an accepted clip-push payload. Pure.
func ParseClipPush(body []byte) (core.ClipPushPayload, bool) {
	var env struct {
		Type    string               `json:"type"`
		Payload core.ClipPushPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return core.ClipPushPayload{}, false
	}
	if env.Type != core.TypeClipPush {
		return core.ClipPushPayload{}, false
	}
	if _, ok := core.SanitizeClipText(env.Payload.Text); !ok {
		return core.ClipPushPayload{}, false
	}
	return env.Payload, true
}

// ParseSettingsSync extracts an accepted settings-sync payload. Pure.
func ParseSettingsSync(body []byte) (core.SettingsSyncPayload, bool) {
	var env struct {
		Type    string                   `json:"type"`
		Payload core.SettingsSyncPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return core.SettingsSyncPayload{}, false
	}
	if env.Type != core.TypeSettingsSync {
		return core.SettingsSyncPayload{}, false
	}
	if _, ok := core.SanitizeSettings(env.Payload); !ok {
		return core.SettingsSyncPayload{}, false
	}
	return env.Payload, true
}
