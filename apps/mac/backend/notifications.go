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

// maxNotifs caps the in-memory mirror. Oldest dismissed-or-oldest entries
// fall off first; the badge counts only live (non-dismissed) rows.
const maxNotifs = 100

// NotifItem is one mirrored phone notification for the frontend. Text is
// truncated at ingest (never the full body on disk); bodies never reach the
// log.
type NotifItem struct {
	ID         string `json:"id"`
	App        string `json:"app"`
	Title      string `json:"title"`
	Text       string `json:"text"`
	PostedUnix int64  `json:"posted_unix"`
}

// NotifStore owns the mirrored list plus queued outbound dismissals.
// In-memory only (a restart refetches via heartbeat); safe for concurrent use.
type NotifStore struct {
	mu             sync.Mutex
	items          []NotifItem
	pendingDismiss []string
	unseen         int
}

// NewNotifStore returns an empty mirror.
func NewNotifStore() *NotifStore {
	return &NotifStore{}
}

// List returns live items, newest first, plus the unseen badge count.
func (s *NotifStore) List() ([]NotifItem, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]NotifItem{}, s.items...)
	return out, s.unseen
}

// MarkSeen resets the badge count (opening the Notifications pane).
func (s *NotifStore) MarkSeen() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unseen = 0
}

// Post ingests one accepted notif-post: same-ID reposts update in place and
// jump to front, new IDs prepend (cap maxNotifs, oldest dropped). Display
// fields are truncated fail-soft; bad IDs are dropped.
func (s *NotifStore) Post(p core.NotifPostPayload) bool {
	id, ok := core.SanitizeNotifID(p.ID)
	if !ok {
		return false
	}
	item := NotifItem{
		ID:         id,
		App:        core.TruncateNotifField(p.App, core.MaxNotifAppLen),
		Title:      core.TruncateNotifField(p.Title, core.MaxNotifTitleLen),
		Text:       core.TruncateNotifField(p.Text, core.MaxNotifTextLen),
		PostedUnix: p.PostedAt,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, it := range s.items {
		if it.ID == id {
			s.items = append(append([]NotifItem{item}, s.items[:i]...), s.items[i+1:]...)
			s.unseen++
			return true
		}
	}
	s.items = append([]NotifItem{item}, s.items...)
	if len(s.items) > maxNotifs {
		s.items = s.items[:maxNotifs]
	}
	s.unseen++
	return true
}

// Dismiss removes an ID locally and queues it for the phone. False when the
// ID was unknown (still queued: the phone may hold what we dropped).
func (s *NotifStore) Dismiss(id string) bool {
	clean, ok := core.SanitizeNotifID(id)
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	known := false
	kept := s.items[:0]
	for _, it := range s.items {
		if it.ID == clean {
			known = true
			continue
		}
		kept = append(kept, it)
	}
	s.items = kept
	s.pendingDismiss = append(s.pendingDismiss, clean)
	return known
}

// ApplyRemoteDismiss drops an ID the phone retracted. Pure store op.
func (s *NotifStore) ApplyRemoteDismiss(id string) {
	clean, ok := core.SanitizeNotifID(id)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.items[:0]
	for _, it := range s.items {
		if it.ID == clean {
			continue
		}
		kept = append(kept, it)
	}
	s.items = kept
}

// Clear empties the mirror and the badge. Dismissals are not echoed: clear
// is a local view reset, the phone reposts live notifications on heartbeat.
func (s *NotifStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = nil
	s.unseen = 0
}

// TakePendingDismissals returns queued dismissal IDs and clears the queue.
func (s *NotifStore) TakePendingDismissals() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pendingDismiss) == 0 {
		return nil
	}
	out := append([]string{}, s.pendingDismiss...)
	s.pendingDismiss = nil
	return out
}

// ParseNotifPost extracts an accepted notif-post payload from a raw feature
// envelope body. ok is false for wrong types or bad IDs. Pure.
func ParseNotifPost(body []byte) (core.NotifPostPayload, bool) {
	var env struct {
		Type    string                `json:"type"`
		Payload core.NotifPostPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return core.NotifPostPayload{}, false
	}
	if env.Type != core.TypeNotifPost {
		return core.NotifPostPayload{}, false
	}
	if _, ok := core.SanitizeNotifID(env.Payload.ID); !ok {
		return core.NotifPostPayload{}, false
	}
	return env.Payload, true
}

// ParseNotifDismiss extracts an accepted notif-dismiss payload. Pure.
func ParseNotifDismiss(body []byte) (core.NotifDismissPayload, bool) {
	var env struct {
		Type    string                   `json:"type"`
		Payload core.NotifDismissPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return core.NotifDismissPayload{}, false
	}
	if env.Type != core.TypeNotifDismiss {
		return core.NotifDismissPayload{}, false
	}
	if _, ok := core.SanitizeNotifID(env.Payload.ID); !ok {
		return core.NotifDismissPayload{}, false
	}
	return env.Payload, true
}
