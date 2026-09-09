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

// PlaybackView is the typed now-playing state for the frontend. ArtworkB64
// holds downscaled cover art (may be ""); empty title+stopped means idle.
// Artwork is never logged verbatim; handlers log package + lengths only.
type PlaybackView struct {
	HasState    bool   `json:"has_state"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Album       string `json:"album"`
	PackageName string `json:"package_name"`
	App         string `json:"app"`
	State       string `json:"state"`
	PositionMs  int64  `json:"position_ms"`
	DurationMs  int64  `json:"duration_ms"`
	UpdatedMs   int64  `json:"updated_ms"`
	ArtworkB64  string `json:"artwork_b64,omitempty"`
	ArtworkMime string `json:"artwork_mime,omitempty"`
}

// PlaybackStore owns the latest phone playback snapshot (latest-wins on
// UpdatedMs) plus a nonce set for command idempotency. Safe for concurrent
// use. In-memory only: playback snapshots never touch disk.
type PlaybackStore struct {
	mu       sync.Mutex
	cur      PlaybackView
	has      bool
	seenCmds map[string]struct{}
}

// NewPlaybackStore returns an empty (idle) playback store.
func NewPlaybackStore() *PlaybackStore {
	return &PlaybackStore{seenCmds: make(map[string]struct{})}
}

// Get returns a copy of the current playback state.
func (s *PlaybackStore) Get() PlaybackView {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.has {
		return PlaybackView{State: core.PlaybackStopped}
	}
	return s.cur
}

// ApplyRemote adopts an incoming playback-state when it is newer
// (core.RemotePlaybackWins). Returns true when adopted.
func (s *PlaybackStore) ApplyRemote(p core.PlaybackStatePayload) bool {
	sanitized, ok := core.SanitizePlaybackState(p)
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !core.RemotePlaybackWins(s.cur.UpdatedMs, sanitized.UpdatedMs) {
		return false
	}
	s.cur = PlaybackView{
		HasState:    true,
		Title:       sanitized.Title,
		Artist:      sanitized.Artist,
		Album:       sanitized.Album,
		PackageName: sanitized.PackageName,
		App:         sanitized.App,
		State:       sanitized.State,
		PositionMs:  sanitized.PositionMs,
		DurationMs:  sanitized.DurationMs,
		UpdatedMs:   sanitized.UpdatedMs,
		ArtworkB64:  sanitized.ArtworkB64,
		ArtworkMime: sanitized.ArtworkMime,
	}
	s.has = true
	return true
}

// Clear drops the snapshot (unpair/off). Idempotent.
func (s *PlaybackStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cur = PlaybackView{State: core.PlaybackStopped}
	s.has = false
}

// SeenCmd reports whether a command nonce was already handled (idempotency
// for redelivered playback-cmd). Marks new nonces; evicts when over 256.
func (s *PlaybackStore) SeenCmd(nonce string) bool {
	if nonce == "" {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seenCmds[nonce]; ok {
		return true
	}
	s.seenCmds[nonce] = struct{}{}
	if len(s.seenCmds) > 256 {
		for k := range s.seenCmds {
			delete(s.seenCmds, k)
			break
		}
	}
	return false
}

// ParsePlaybackState extracts an accepted playback-state payload. Pure.
func ParsePlaybackState(body []byte) (core.PlaybackStatePayload, bool) {
	var env struct {
		Type    string                     `json:"type"`
		Payload core.PlaybackStatePayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return core.PlaybackStatePayload{}, false
	}
	if env.Type != core.TypePlaybackState {
		return core.PlaybackStatePayload{}, false
	}
	if _, ok := core.SanitizePlaybackState(env.Payload); !ok {
		return core.PlaybackStatePayload{}, false
	}
	return env.Payload, true
}

// ParsePlaybackCmd extracts an accepted playback-cmd payload. Pure.
func ParsePlaybackCmd(body []byte) (core.PlaybackCmdPayload, bool) {
	var env struct {
		Type    string                   `json:"type"`
		Payload core.PlaybackCmdPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return core.PlaybackCmdPayload{}, false
	}
	if env.Type != core.TypePlaybackCmd {
		return core.PlaybackCmdPayload{}, false
	}
	if _, ok := core.SanitizePlaybackCmd(env.Payload); !ok {
		return core.PlaybackCmdPayload{}, false
	}
	return env.Payload, true
}
