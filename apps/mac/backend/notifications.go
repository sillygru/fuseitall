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
	"time"

	"fuseitall/core"
)

// maxNotifs caps the in-memory mirror. Oldest dismissed-or-oldest entries
// fall off first; the badge counts only live (non-dismissed) rows.
const maxNotifs = 100

// bannerCooldownSec suppresses repeat banners for the same notification ID:
// Play Store percent ticks and other rapid reposts update the mirror row
// silently instead of spamming the desktop. Distinct notifications always
// banner immediately.
const bannerCooldownSec = 30

// stalePostMaxAgeSec is the live-only horizon: posts older than this are
// dropped instead of mirrored, so a phone reconnect never floods the Mac
// with hours-old history as if it were new. Zero/negative PostedUnix (old
// senders) is kept for backward compat.
const stalePostMaxAgeSec = 300

// NotifItem is one mirrored phone notification for the frontend. Text is
// truncated at ingest (never the full body on disk); bodies never reach the
// log. PackageName and IconB64 are additive 0.3.0+: cached per package, fail-
// soft when invalid/oversize, never logged.
type NotifItem struct {
	ID          string `json:"id"`
	App         string `json:"app"`
	PackageName string `json:"package_name"`
	IconB64     string `json:"app_icon_b64"`
	GroupKey    string `json:"group_key"`
	Title       string `json:"title"`
	Text        string `json:"text"`
	PostedUnix  int64  `json:"posted_unix"`
}

// NotifStore owns the mirrored list plus queued outbound dismissals.
// In-memory only (live-only: a restart shows only new posts, never a stale
// replay); safe for concurrent use.
// iconCache remembers the last icon per package so later posts that omit the
// icon (bandwidth saving) still render with the cached icon.
// lastBannerUnix throttles repeat banners per ID (progress-tick spam fix).
type NotifStore struct {
	mu             sync.Mutex
	items          []NotifItem
	pendingDismiss []string
	unseen         int
	iconCache      map[string]string
	lastBannerUnix map[string]int64
	nowUnix        func() int64
}

// NewNotifStore returns an empty mirror.
func NewNotifStore() *NotifStore {
	return &NotifStore{
		iconCache:      make(map[string]string),
		lastBannerUnix: make(map[string]int64),
		nowUnix:        func() int64 { return time.Now().Unix() },
	}
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

// IconForPackage returns the cached icon base64 for a given package name, if known.
func (s *NotifStore) IconForPackage(pkg string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.iconCache == nil {
		return ""
	}
	return s.iconCache[pkg]
}

// Post ingests one accepted notif-post. It returns (accepted, shouldBanner):
// bad IDs are dropped (false,false); progress posts are dropped (false,false)
// so download percent ticks never reach the mirror; identical same-ID
// reposts refresh the row silently (true,false) without badge or banner;
// new IDs or changed content banner subject to a per-ID 30s cooldown
// (true, banner). Display fields are truncated fail-soft; package/icon are
// cached and fail-soft (invalid icon → "" but notification kept).
func (s *NotifStore) Post(p core.NotifPostPayload) (bool, bool) {
	id, ok := core.SanitizeNotifID(p.ID)
	if !ok {
		return false, false
	}
	if p.HasProgress {
		return false, false
	}
	icon := core.SanitizeNotifIconB64(p.IconB64)
	pkg := core.SanitizePackageName(p.PackageName)
	group := core.SanitizeGroupKey(p.GroupKey)
	now := s.nowUnix()
	if s.nowUnix == nil {
		now = time.Now().Unix()
	}
	// Live-only horizon: drop stale history replays.
	if p.PostedAt > 0 && now-p.PostedAt > stalePostMaxAgeSec {
		return false, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if pkg != "" {
		if icon != "" {
			if s.iconCache == nil {
				s.iconCache = make(map[string]string)
			}
			s.iconCache[pkg] = icon
		} else if cached, ok := s.iconCache[pkg]; ok {
			icon = cached
		}
	}
	item := NotifItem{
		ID:          id,
		App:         core.TruncateNotifField(p.App, core.MaxNotifAppLen),
		PackageName: pkg,
		IconB64:     icon,
		GroupKey:    group,
		Title:       core.TruncateNotifField(p.Title, core.MaxNotifTitleLen),
		Text:        core.TruncateNotifField(p.Text, core.MaxNotifTextLen),
		PostedUnix:  p.PostedAt,
	}
	for i, it := range s.items {
		if it.ID == id {
			// Preserve previous icon if new one is empty but old had one.
			if item.IconB64 == "" && it.IconB64 != "" {
				item.IconB64 = it.IconB64
			}
			s.items = append(append([]NotifItem{item}, s.items[:i]...), s.items[i+1:]...)
			// Identical content: silent row refresh, no badge, no banner.
			if it.Title == item.Title && it.Text == item.Text {
				return true, false
			}
			// Changed content: badge always, banner throttled per ID.
			s.unseen++
			if s.lastBannerUnix == nil {
				s.lastBannerUnix = make(map[string]int64)
			}
			if last, ok := s.lastBannerUnix[id]; ok && now-last < bannerCooldownSec {
				return true, false
			}
			s.lastBannerUnix[id] = now
			return true, true
		}
	}
	s.items = append([]NotifItem{item}, s.items...)
	if len(s.items) > maxNotifs {
		s.items = s.items[:maxNotifs]
	}
	s.unseen++
	if s.lastBannerUnix == nil {
		s.lastBannerUnix = make(map[string]int64)
	}
	s.lastBannerUnix[id] = now
	return true, true
}

// maxPendingDismiss caps the queued outbound dismissals; oldest dropped.
const maxPendingDismiss = 50

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
	for len(s.pendingDismiss) > maxPendingDismiss {
		s.pendingDismiss = s.pendingDismiss[1:]
	}
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
// Icon cache is kept so reposts that omit icons still render.
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
