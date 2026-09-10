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
// basename for UTI/extension preservation. ChangedC carries the HLC counter;
// ContentHash dedupes identical pushes; Sensitive marks OS-flagged secrets.
type ClipNotice struct {
	HasText     bool
	Kind        string
	Text        string
	Mime        string
	ImageB64    string
	Filename    string
	ChangedUnix int64
	ChangedC    int64
	Origin      string
	Preview     string
	Pending     bool
	Sensitive   bool
}

// ClipStore owns the latest clipboard payload plus one pending outbound push
// (latest-wins). In-memory only: clipboard contents never touch disk.
// Safe for concurrent use. It also owns the HLC clock, the last applied
// content hash, and a bounded nonce LRU so redeliveries never flap the UI.
type ClipStore struct {
	mu        sync.Mutex
	kind      string
	text      string
	mime      string
	imageB64  string
	filename  string
	changedAt int64
	changedC  int64
	contentHash string
	sensitive bool
	origin    string
	has       bool
	pending   bool
	hlc       core.ClipStamp
	seen      map[string]int64
	seenOrder []string
	lastHash  string
}

// NewClipStore returns an empty clipboard.
func NewClipStore() *ClipStore {
	return &ClipStore{seen: make(map[string]int64)}
}

// nextStamp advances the HLC clock for a local copy. Call with mu held.
// Wall is unix seconds; monotonic against stored clock and current value.
func (s *ClipStore) nextStampLocked(wall int64) core.ClipStamp {
	if wall < 0 {
		wall = 0
	}
	if cur := s.changedAt; wall <= cur {
		wall = cur
	}
	// Promote wall through stored HLC plus current value so restarts and
	// clock skew never hand out lower stamps.
	stored := s.hlc
	if s.has {
		if s.changedAt > stored.L || (s.changedAt == stored.L && s.changedC > stored.C) {
			stored = core.ClipStamp{L: s.changedAt, C: s.changedC}
		}
	}
	next := core.ClipStampNext(stored, wall)
	s.hlc = next
	return next
}

// noteSeen records a nonce; true when already seen (duplicate redelivery).
// Call with mu held. Bounded LRU (256 entries).
func (s *ClipStore) noteSeenLocked(nonce string, now int64) bool {
	if nonce == "" {
		return false
	}
	if _, ok := s.seen[nonce]; ok {
		return true
	}
	s.seen[nonce] = now
	s.seenOrder = append(s.seenOrder, nonce)
	if len(s.seenOrder) > 256 {
		old := s.seenOrder[0]
		s.seenOrder = append([]string{}, s.seenOrder[1:]...)
		delete(s.seen, old)
	}
	return false
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
		ChangedC:    s.changedC,
		Origin:      s.origin,
		Preview:     preview,
		Pending:     s.pending,
		Sensitive:   s.sensitive,
	}
}

// SetLocal records a Mac-side text copy: stamps origin mac, marks pending.
// Over-long input fails closed without touching state. changedAt is treated
// as a wall hint and promoted through the HLC clock so stamps stay monotonic.
func (s *ClipStore) SetLocal(text string, changedAt int64) (ClipNotice, bool) {
	return s.SetLocalWithMeta(text, changedAt, false)
}

// SetLocalWithMeta records text with a sensitivity flag (manual Send sets
// sensitive when the OS flagged the source; auto callers pass false after
// gating). Pure aside from the store lock.
func (s *ClipStore) SetLocalWithMeta(text string, changedAt int64, sensitive bool) (ClipNotice, bool) {
	if _, ok := core.SanitizeClipText(text); !ok {
		return ClipNotice{}, false
	}
	hash := core.ContentHashForText(text)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.has && s.kind == core.ClipKindText && s.text == text {
		return ClipNotice{
			HasText: true, Kind: core.ClipKindText, Text: s.text, ChangedUnix: s.changedAt,
			ChangedC: s.changedC, Origin: s.origin, Preview: previewOf(s.text), Pending: s.pending,
			Sensitive: s.sensitive,
		}, true
	}
	stamp := s.nextStampLocked(changedAt)
	s.kind, s.text, s.mime, s.imageB64 = core.ClipKindText, text, "", ""
	s.filename, s.contentHash, s.sensitive = "", hash, sensitive
	s.changedAt, s.changedC, s.origin, s.has, s.pending = stamp.L, stamp.C, core.OriginMac, true, true
	s.lastHash = hash
	return ClipNotice{
		HasText: true, Kind: core.ClipKindText, Text: text, ChangedUnix: stamp.L,
		ChangedC: stamp.C, Origin: core.OriginMac, Preview: previewOf(text), Pending: true,
		Sensitive: sensitive,
	}, true
}

// SetLocalImage records a Mac-side image copy. B64 must be valid base64 and
// mime must be whitelisted. Fails closed without touching state.
func (s *ClipStore) SetLocalImage(b64, mime string, changedAt int64) (ClipNotice, bool) {
	return s.SetLocalImageWithFilename(b64, mime, "", changedAt)
}

// SetLocalImageWithFilename records a Mac-side image copy with optional filename.
func (s *ClipStore) SetLocalImageWithFilename(b64, mime, filename string, changedAt int64) (ClipNotice, bool) {
	return s.SetLocalImageWithMeta(b64, mime, filename, changedAt, false)
}

// SetLocalImageWithMeta records an image with HLC stamping, content hashing,
// and sensitivity. Fails closed without touching state.
func (s *ClipStore) SetLocalImageWithMeta(b64, mime, filename string, changedAt int64, sensitive bool) (ClipNotice, bool) {
	raw, ok := core.SanitizeClipImage(b64, mime)
	if !ok {
		return ClipNotice{}, false
	}
	m, _ := core.SanitizeClipMime(mime)
	fn := core.SanitizeClipFilename(filename)
	if filename != "" && fn == "" {
		fn = ""
	}
	hash := core.ContentHashForBytes(raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.has && s.kind == core.ClipKindImage && s.imageB64 == b64 && s.mime == m && s.filename == fn {
		return ClipNotice{
			HasText: true, Kind: core.ClipKindImage, Mime: s.mime, ImageB64: s.imageB64, Filename: s.filename,
			ChangedUnix: s.changedAt, ChangedC: s.changedC, Origin: s.origin, Preview: m + " image", Pending: s.pending,
			Sensitive: s.sensitive,
		}, true
	}
	stamp := s.nextStampLocked(changedAt)
	s.kind, s.mime, s.imageB64, s.text = core.ClipKindImage, m, b64, ""
	s.filename = fn
	s.contentHash, s.sensitive = hash, sensitive
	s.changedAt, s.changedC, s.origin, s.has, s.pending = stamp.L, stamp.C, core.OriginMac, true, true
	s.lastHash = hash
	preview := m + " image"
	if fn != "" {
		preview += " " + fn
	}
	return ClipNotice{
		HasText: true, Kind: core.ClipKindImage, Mime: m, ImageB64: b64, Filename: fn,
		ChangedUnix: stamp.L, ChangedC: stamp.C, Origin: core.OriginMac, Preview: preview, Pending: true,
		Sensitive: sensitive,
	}, true
}

// ApplyRemote adopts an incoming clip-push when it is newer
// (HLC lexicographic wins, ties keep local, zero never wins) and echoes are
// suppressed via origin + nonce + content-hash dedupe. Returns true when
// adopted. Drops are feed-only (caller logs lengths, never bodies).
func (s *ClipStore) ApplyRemote(p core.ClipPushPayload) bool {
	if !core.SanitizeClipPush(p) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.Nonce != "" && s.noteSeenLocked(p.Nonce, p.ChangedAt) {
		return false
	}
	if !core.RemoteClipWinsEx(s.changedAt, s.changedC, p.ChangedAt, p.ChangedC) {
		// Same-content redelivery inside the tie window is still a drop, but
		// hash equality makes it idempotent rather than a conflict.
		return false
	}
	origin := core.NormalizeOrigin(p.Origin)
	if origin == "" {
		origin = core.OriginAndroid
	}
	if origin == core.OriginMac {
		return false
	}
	// Hash dedupe: identical bytes already applied never rewrite the
	// pasteboard (kills sanitization-mutation loops).
	if p.ContentHash != "" && p.ContentHash == s.lastHash && s.has {
		return false
	}
	kind := core.NormalizeClipKind(p.Kind)
	if kind == core.ClipKindImage {
		m, _ := core.SanitizeClipMime(p.Mime)
		s.kind, s.mime, s.imageB64, s.text = core.ClipKindImage, m, p.ImageB64, ""
		s.filename = core.SanitizeClipFilename(p.Filename)
		if p.ContentHash != "" {
			s.contentHash, s.lastHash = p.ContentHash, p.ContentHash
		} else if raw, ok := core.SanitizeClipImage(p.ImageB64, m); ok {
			h := core.ContentHashForBytes(raw)
			s.contentHash, s.lastHash = h, h
		}
	} else {
		s.kind, s.text, s.mime, s.imageB64 = core.ClipKindText, p.Text, "", ""
		s.filename = ""
		if p.ContentHash != "" {
			s.contentHash, s.lastHash = p.ContentHash, p.ContentHash
		} else {
			h := core.ContentHashForText(p.Text)
			s.contentHash, s.lastHash = h, h
		}
	}
	s.changedAt, s.changedC, s.origin, s.has = p.ChangedAt, p.ChangedC, origin, true
	s.sensitive = p.Sensitive
	s.pending = false
	// Absorb the remote stamp so our next local stamp exceeds both clocks.
	s.hlc = core.ClipStampOnReceive(s.hlc, core.ClipStamp{L: p.ChangedAt, C: p.ChangedC}, p.ChangedAt)
	return true
}

// TakePending returns the current payload for upload and clears the flag.
// False when nothing needs sending. Carries HLC + hash + sensitivity so the
// receiver dedupes without rewriting the pasteboard.
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
	hash := s.contentHash
	if hash == "" {
		if kind == core.ClipKindImage {
			if raw, ok := core.SanitizeClipImage(s.imageB64, s.mime); ok {
				hash = core.ContentHashForBytes(raw)
			}
		} else {
			hash = core.ContentHashForText(s.text)
		}
	}
	return core.ClipPushPayload{
		Kind:        kind,
		Text:        s.text,
		Mime:        s.mime,
		ImageB64:    s.imageB64,
		Filename:    s.filename,
		ChangedAt:   s.changedAt,
		ChangedC:    s.changedC,
		Origin:      s.origin,
		ContentHash: hash,
		Sensitive:   s.sensitive,
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

// ParseClipManifest extracts an accepted chunk manifest. Pure.
func ParseClipManifest(body []byte) (core.ClipManifestPayload, bool) {
	var env struct {
		Type    string                   `json:"type"`
		Payload core.ClipManifestPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return core.ClipManifestPayload{}, false
	}
	if env.Type != core.TypeClipManifest {
		return core.ClipManifestPayload{}, false
	}
	if !core.SanitizeClipManifest(env.Payload) {
		return core.ClipManifestPayload{}, false
	}
	return env.Payload, true
}

// ParseClipChunk extracts an accepted chunk. Pure.
func ParseClipChunk(body []byte) (core.ClipChunkPayload, bool) {
	var env struct {
		Type    string                `json:"type"`
		Payload core.ClipChunkPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return core.ClipChunkPayload{}, false
	}
	if env.Type != core.TypeClipChunk {
		return core.ClipChunkPayload{}, false
	}
	if !core.SanitizeClipChunk(env.Payload) {
		return core.ClipChunkPayload{}, false
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
