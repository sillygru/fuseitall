// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"strings"
	"testing"

	"fuseitall/core"
)

func TestSettingsStoreRoundTrip(t *testing.T) {
	s := NewSettingsStore()
	got, err := s.SetNotificationsEnabled(false)
	if err != nil {
		t.Fatalf("SetNotificationsEnabled: %v", err)
	}
	if got.NotificationsEnabled != false {
		t.Fatalf("enabled = %v", got.NotificationsEnabled)
	}
	if !s.HasPending() {
		t.Fatal("set must arm pending")
	}
	if _, ok := s.TakePending(); !ok {
		t.Fatal("TakePending must return the blob")
	}
	if s.HasPending() {
		t.Fatal("take must clear pending")
	}
}

func TestSettingsStoreApplyRemote(t *testing.T) {
	s := NewSettingsStore()
	local := s.Get()
	older := core.SettingsSyncPayload{
		UpdatedUnix: local.UpdatedUnix - 1,
	}
	if s.ApplyRemote(older) {
		t.Fatal("older remote must lose")
	}
	newer := core.SettingsSyncPayload{
		UpdatedUnix: local.UpdatedUnix + 10,
		UpdatedBy: core.OriginAndroid,
	}
	if !s.ApplyRemote(newer) {
		t.Fatal("newer remote must win")
	}
	if s.HasPending() {
		t.Fatal("adopted remote must not echo back")
	}
}

func TestNotifStoreCapAndDismiss(t *testing.T) {
	s := NewNotifStore()
	for i := 0; i < maxNotifs+10; i++ {
		s.Post(core.NotifPostPayload{ID: strings.Repeat("n", 3) + string(rune('a'+i%26)) + strings.Repeat("x", i%5)})
	}
	items, unseen := s.List()
	if len(items) > maxNotifs {
		t.Fatalf("len = %d, want <= %d", len(items), maxNotifs)
	}
	if unseen == 0 {
		t.Fatal("posts must raise the badge")
	}
	s.MarkSeen()
	if _, unseen := s.List(); unseen != 0 {
		t.Fatal("mark seen must reset badge")
	}
	id := items[0].ID
	s.Dismiss(id)
	rest, _ := s.List()
	for _, it := range rest {
		if it.ID == id {
			t.Fatal("dismissed id still listed")
		}
	}
	if pend := s.TakePendingDismissals(); len(pend) != 1 || pend[0] != id {
		t.Fatalf("pending = %v", pend)
	}
}

func TestClipStoreEchoSuppression(t *testing.T) {
	s := NewClipStore()
	if _, ok := s.SetLocal("hello", 10); !ok {
		t.Fatal("SetLocal must accept")
	}
	if !s.HasPending() {
		t.Fatal("local copy must arm pending")
	}
	if _, ok := s.SetLocal("hello", 11); !ok {
		t.Fatal("same text must still ok")
	}
	if s.ApplyRemote(core.ClipPushPayload{Text: "old", ChangedAt: 5, Origin: core.OriginAndroid}) {
		t.Fatal("older remote must lose")
	}
	if !s.ApplyRemote(core.ClipPushPayload{Text: "new", ChangedAt: 20, Origin: core.OriginAndroid}) {
		t.Fatal("newer remote must win")
	}
	if s.HasPending() {
		t.Fatal("adopted remote must not bounce back")
	}
	if got := s.Get(); got.Text != "new" || got.Origin != core.OriginAndroid {
		t.Fatalf("clip = %+v", got)
	}
	// Echo of own origin must not apply.
	if s.ApplyRemote(core.ClipPushPayload{Text: "echo", ChangedAt: 30, Origin: core.OriginMac}) {
		t.Fatal("own echo must not apply")
	}
}

func TestPhotoThumbLRUCache(t *testing.T) {
	lru := newPhotoThumbLRU(3)
	lru.Put("p1", PhotoThumbResult{PhotoID: "1", DataB64: "d1"})
	lru.Put("p2", PhotoThumbResult{PhotoID: "2", DataB64: "d2"})
	lru.Put("p3", PhotoThumbResult{PhotoID: "3", DataB64: "d3"})

	// Access p1 so it becomes most recently used
	val, ok := lru.Get("p1")
	if !ok || val.DataB64 != "d1" {
		t.Fatalf("expected p1 to be in cache, got %v", val)
	}

	// Insert p4, should evict p2 (least recently used)
	lru.Put("p4", PhotoThumbResult{PhotoID: "4", DataB64: "d4"})

	if _, ok := lru.Get("p2"); ok {
		t.Fatal("expected p2 to be evicted")
	}
	if _, ok := lru.Get("p1"); !ok {
		t.Fatal("expected p1 to still be cached")
	}
	if _, ok := lru.Get("p3"); !ok {
		t.Fatal("expected p3 to still be cached")
	}
	if _, ok := lru.Get("p4"); !ok {
		t.Fatal("expected p4 to still be cached")
	}
}

