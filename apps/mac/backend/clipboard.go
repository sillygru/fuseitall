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

// ClipNotice is the typed clipboard state for the frontend. For text, Text
// holds the full value (UI truncates for preview); for images, ImageB64+Mmime
// hold the base64 payload. Kind is "text" or "image". ImageB64 is never
// logged verbatim; handlers log lengths and IDs only. Filename is sanitized
// basename for UTI/extension preservation.
type ClipNotice struct {
	HasText     bool
	Kind        string
	Text        string
	Mime        string
	ImageB64    string
	Filename    string
	ChangedUnix int64
	Origin      string
	Preview     string
	Pending     bool
}

// ClipStore owns the latest clipboard payload plus one pending outbound push
// (latest-wins). In-memory only: clipboard contents never touch disk.
// Safe for concurrent use.
type ClipStore struct {
	mu        sync.Mutex
	kind      string
	text      string
	mime      string
	imageB64  string
	filename  string
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
	kind := s.kind
	if kind == "" {
		kind = core.ClipKindText
	}
	preview := previewOf(s.text)
	if kind == core.ClipKindImage {
		preview = s.mime + " image"
		if s.imageB64 != "" {
			preview += " (" + s.mime + ")"
		}
		if s.filename != "" {
			preview += " " + s.filename
		}
	}
	return ClipNotice{
		HasText:     s.has,
		Kind:        kind,
		Text:        s.text,
		Mime:        s.mime,
		ImageB64:    s.imageB64,
		Filename:    s.filename,
		ChangedUnix: s.changedAt,
		Origin:      s.origin,
		Preview:     preview,
		Pending:     s.pending,
	}
}

// SetLocal records a Mac-side text copy: stamps origin mac, marks pending.
// Over-long input fails closed without touching state.
func (s *ClipStore) SetLocal(text string, changedAt int64) (ClipNotice, bool) {
	if _, ok := core.SanitizeClipText(text); !ok {
		return ClipNotice{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.has && s.kind == core.ClipKindText && s.text == text {
		return ClipNotice{
			HasText: true, Kind: core.ClipKindText, Text: s.text, ChangedUnix: s.changedAt,
			Origin: s.origin, Preview: previewOf(s.text), Pending: s.pending,
		}, true
	}
	s.kind, s.text, s.mime, s.imageB64 = core.ClipKindText, text, "", ""
	s.changedAt, s.origin, s.has, s.pending = changedAt, core.OriginMac, true, true
	return ClipNotice{
		HasText: true, Kind: core.ClipKindText, Text: text, ChangedUnix: changedAt,
		Origin: core.OriginMac, Preview: previewOf(text), Pending: true,
	}, true
}

// SetLocalImage records a Mac-side image copy. B64 must be valid base64 and
// mime must be whitelisted. Fails closed without touching state.
func (s *ClipStore) SetLocalImage(b64, mime string, changedAt int64) (ClipNotice, bool) {
	return s.SetLocalImageWithFilename(b64, mime, "", changedAt)
}

// SetLocalImageWithFilename records a Mac-side image copy with optional filename.
func (s *ClipStore) SetLocalImageWithFilename(b64, mime, filename string, changedAt int64) (ClipNotice, bool) {
	if _, ok := core.SanitizeClipImage(b64, mime); !ok {
		return ClipNotice{}, false
	}
	m, _ := core.SanitizeClipMime(mime)
	fn := core.SanitizeClipFilename(filename)
	if filename != "" && fn == "" {
		// Loud fail-closed on bad filename: drop filename but keep image if caller passed garbage.
		fn = ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.has && s.kind == core.ClipKindImage && s.imageB64 == b64 && s.mime == m && s.filename == fn {
		return ClipNotice{
			HasText: true, Kind: core.ClipKindImage, Mime: s.mime, ImageB64: s.imageB64, Filename: s.filename,
			ChangedUnix: s.changedAt, Origin: s.origin, Preview: m + " image", Pending: s.pending,
		}, true
	}
	s.kind, s.mime, s.imageB64, s.text = core.ClipKindImage, m, b64, ""
	s.filename = fn
	s.changedAt, s.origin, s.has, s.pending = changedAt, core.OriginMac, true, true
	preview := m + " image"
	if fn != "" {
		preview += " " + fn
	}
	return ClipNotice{
		HasText: true, Kind: core.ClipKindImage, Mime: m, ImageB64: b64, Filename: fn,
		ChangedUnix: changedAt, Origin: core.OriginMac, Preview: preview, Pending: true,
	}, true
}

// ApplyRemote adopts an incoming clip-push when it is newer
// (core.RemoteClipWins) and echoes are suppressed via origin check.
// Returns true when adopted.
func (s *ClipStore) ApplyRemote(p core.ClipPushPayload) bool {
	if !core.SanitizeClipPush(p) {
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
	if origin == core.OriginMac {
		return false
	}
	kind := core.NormalizeClipKind(p.Kind)
	if kind == core.ClipKindImage {
		m, _ := core.SanitizeClipMime(p.Mime)
		s.kind, s.mime, s.imageB64, s.text = core.ClipKindImage, m, p.ImageB64, ""
		s.filename = core.SanitizeClipFilename(p.Filename)
	} else {
		s.kind, s.text, s.mime, s.imageB64 = core.ClipKindText, p.Text, "", ""
		s.filename = ""
	}
	s.changedAt, s.origin, s.has = p.ChangedAt, origin, true
	s.pending = false
	return true
}

// TakePending returns the current payload for upload and clears the flag.
// False when nothing needs sending.
func (s *ClipStore) TakePending() (core.ClipPushPayload, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.pending || !s.has {
		return core.ClipPushPayload{}, false
	}
	s.pending = false
	kind := s.kind
	if kind == "" {
		kind = core.ClipKindText
	}
	return core.ClipPushPayload{
		Kind:      kind,
		Text:      s.text,
		Mime:      s.mime,
		ImageB64:  s.imageB64,
		Filename:  s.filename,
		ChangedAt: s.changedAt,
		Origin:    s.origin,
	}, true
}

// TakePendingText is a helper for legacy callers that only need text.
func (s *ClipStore) TakePendingText() (text string, changedAt int64, ok bool) {
	p, ok := s.TakePending()
	if !ok || p.Kind == core.ClipKindImage {
		if ok {
			// Requeue image pending for caller that expected text.
			s.mu.Lock()
			s.pending = true
			s.mu.Unlock()
		}
		return "", 0, false
	}
	return p.Text, p.ChangedAt, true
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
	if !core.SanitizeClipPush(env.Payload) {
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
