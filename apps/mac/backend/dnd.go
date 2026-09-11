// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"sync"

	"fuseitall/core"
)

// DNDView is the typed Do Not Disturb state for the frontend.
type DNDView struct {
	Enabled       bool  `json:"enabled"`
	HasPermission bool  `json:"has_permission"`
	HasState      bool  `json:"has_state"`
	UpdatedMs     int64 `json:"updated_ms"`
}

// DNDStore owns the phone's Do Not Disturb status. Safe for concurrent use.
// In-memory only: DND state is volatile and refreshed on connect/push.
type DNDStore struct {
	mu  sync.RWMutex
	cur DNDView
}

// NewDNDStore returns an empty DND store.
func NewDNDStore() *DNDStore {
	return &DNDStore{}
}

// Get returns a copy of the current DND state.
func (s *DNDStore) Get() DNDView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur
}

// ApplyRemote adopts an incoming dnd-state snapshot when it is newer
// than or equal to the current snapshot. Returns true when applied.
func (s *DNDStore) ApplyRemote(p core.DNDStatePayload) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur.HasState && p.UpdatedMs > 0 && p.UpdatedMs < s.cur.UpdatedMs {
		return false
	}
	s.cur = DNDView{
		Enabled:       p.Enabled,
		HasPermission: p.HasPermission,
		HasState:      true,
		UpdatedMs:     p.UpdatedMs,
	}
	return true
}

// SetOptimistic applies an optimistic state toggle on user action.
func (s *DNDStore) SetOptimistic(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cur.Enabled = enabled
	s.cur.HasState = true
}

// Clear resets the DND state to default (e.g. on disconnect or unpair).
func (s *DNDStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cur = DNDView{}
}
