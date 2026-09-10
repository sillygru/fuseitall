// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"encoding/json"
	"errors"
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

func TestNotifStoreProgressDropped(t *testing.T) {
	s := NewNotifStore()
	if ok, banner := s.Post(core.NotifPostPayload{ID: "dl", HasProgress: true}); ok || banner {
		t.Fatal("progress post must drop")
	}
	if items, _ := s.List(); len(items) != 0 {
		t.Fatal("progress must never reach the mirror")
	}
}

func TestNotifStoreIdenticalRepostSilent(t *testing.T) {
	s := NewNotifStore()
	if ok, banner := s.Post(core.NotifPostPayload{ID: "n1", Title: "Hi", Text: "there"}); !ok || !banner {
		t.Fatal("first post must accept+banner")
	}
	if _, unseen := s.List(); unseen != 1 {
		t.Fatalf("unseen = %d, want 1", unseen)
	}
	// Identical repost: silent row refresh, no badge, no banner.
	if ok, banner := s.Post(core.NotifPostPayload{ID: "n1", Title: "Hi", Text: "there"}); !ok || banner {
		t.Fatal("identical repost must accept without banner")
	}
	if _, unseen := s.List(); unseen != 1 {
		t.Fatalf("identical repost must not bump badge, unseen = %d", unseen)
	}
	// Changed content: badge bumps, banner throttled by per-ID cooldown
	// (first banner was just now, so this one suppresses).
	ok, banner := s.Post(core.NotifPostPayload{ID: "n1", Title: "Hi", Text: "changed"})
	if !ok {
		t.Fatal("changed repost must accept")
	}
	if banner {
		t.Fatal("changed repost within cooldown must suppress banner")
	}
	if _, unseen := s.List(); unseen < 2 {
		t.Fatalf("changed repost must bump badge, unseen = %d", unseen)
	}
}

func TestNotifStoreStaleDropped(t *testing.T) {
	s := NewNotifStore()
	s.nowUnix = func() int64 { return 2000 }
	if ok, _ := s.Post(core.NotifPostPayload{ID: "old", PostedAt: 1000}); ok {
		t.Fatal("stale post must drop")
	}
	if ok, banner := s.Post(core.NotifPostPayload{ID: "fresh", PostedAt: 1900}); !ok || !banner {
		t.Fatal("fresh post must accept+banner")
	}
}

func TestSettingsStoreNotifFilter(t *testing.T) {
	s := NewSettingsStore()
	got, err := s.SetNotifMode(core.NotifOnlyAllowed)
	if err != nil || got.NotifMode != core.NotifOnlyAllowed {
		t.Fatalf("SetNotifMode: %+v %v", got, err)
	}
	if _, err := s.SetAppMuted("com.muted", true); err != nil {
		t.Fatalf("SetAppMuted: %v", err)
	}
	if _, err := s.SetAppAllowed("com.keep", true); err != nil {
		t.Fatalf("SetAppAllowed: %v", err)
	}
	cur := s.Get()
	if !cur.NotificationsEnabled {
		t.Fatal("filter setters must not flip master switch")
	}
	// Remote newer blob adopts filter lists wholesale (LWW).
	newer := core.SettingsSyncPayload{
		NotifMode:       core.NotifAllExceptMuted,
		MutedPackages:   []string{"com.a"},
		AllowedPackages: []string{"com.b"},
		UpdatedUnix:     cur.UpdatedUnix + 10,
		UpdatedBy:       core.OriginAndroid,
	}
	if !s.ApplyRemote(newer) {
		t.Fatal("newer remote must win")
	}
	after := s.Get()
	if after.NotifMode != core.NotifAllExceptMuted || len(after.MutedPackages) != 1 {
		t.Fatalf("filter adopt failed: %+v", after)
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
	if s.ApplyRemote(core.ClipPushPayload{Nonce: "n-old", Text: "old", ChangedAt: 5, Origin: core.OriginAndroid}) {
		t.Fatal("older remote must lose")
	}
	if !s.ApplyRemote(core.ClipPushPayload{Nonce: "n-new", Text: "new", ChangedAt: 20, Origin: core.OriginAndroid}) {
		t.Fatal("newer remote must win")
	}
	if s.HasPending() {
		t.Fatal("adopted remote must not bounce back")
	}
	if got := s.Get(); got.Text != "new" || got.Origin != core.OriginAndroid {
		t.Fatalf("clip = %+v", got)
	}
	// Echo of own origin must not apply.
	if s.ApplyRemote(core.ClipPushPayload{Nonce: "n-echo", Text: "echo", ChangedAt: 30, Origin: core.OriginMac}) {
		t.Fatal("own echo must not apply")
	}
	// Duplicate nonce must not re-apply (idempotent redelivery).
	if s.ApplyRemote(core.ClipPushPayload{Nonce: "n-new", Text: "new", ChangedAt: 20, Origin: core.OriginAndroid}) {
		t.Fatal("duplicate nonce must not re-apply")
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

func TestParseNotifAppsResp(t *testing.T) {
	env, err := core.NewEnvelope(core.TypeNotifAppsResp, core.CurrentSender("android"),
		[]string{core.CapabilityNotifications},
		core.NotifAppsRespPayload{Nonce: "n", ReqID: "r1", Entries: []core.NotifAppEntry{{PackageName: "com.a", App: "A"}}})
	if err != nil {
		t.Fatalf("new envelope: %v", err)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	p, ok := ParseNotifAppsResp(raw)
	if !ok {
		t.Fatal("valid resp must parse")
	}
	if p.ReqID != "r1" || len(p.Entries) != 1 || p.Entries[0].PackageName != "com.a" {
		t.Fatalf("payload mismatch: %+v", p)
	}
	if _, ok := ParseNotifAppsResp([]byte(`{"type":"notif-post","payload":{}}`)); ok {
		t.Fatal("wrong type must not parse as inventory")
	}
	if _, ok := ParseNotifAppsResp([]byte(`{"type":"notif-apps-resp","payload":{"nonce":"n"}}`)); ok {
		t.Fatal("missing req_id must not parse")
	}
}

func TestNotifStoreLearnIconFailSoft(t *testing.T) {
	s := NewNotifStore()
	s.LearnIcon("com.a", "!!!not-base64!!!")
	if got := s.IconForPackage("com.a"); got != "" {
		t.Fatal("invalid icon must not cache")
	}
	s.LearnIcon("", "aGk=")
	if got := s.IconForPackage(""); got != "" {
		t.Fatal("empty package must not cache")
	}
	s.LearnIcon("com.a", "aGk=")
	if got := s.IconForPackage("com.a"); got != "aGk=" {
		t.Fatalf("valid icon must cache, got %q", got)
	}
}

func TestIngestNotifAppsRespResolvesPending(t *testing.T) {
	svc := NewService("{}", "fp", "tok", NewLogBuffer(10))
	ch := make(chan core.NotifAppsRespPayload, 1)
	svc.notifAppsMu.Lock()
	if svc.pendingNotifApps == nil {
		svc.pendingNotifApps = make(map[string]chan core.NotifAppsRespPayload)
	}
	svc.pendingNotifApps["r9"] = ch
	svc.notifAppsMu.Unlock()

	env, err := core.NewEnvelope(core.TypeNotifAppsResp, core.CurrentSender("android"),
		[]string{core.CapabilityNotifications},
		core.NotifAppsRespPayload{Nonce: "n", ReqID: "r9", NextCursor: ""})
	if err != nil {
		t.Fatalf("new envelope: %v", err)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	svc.ingestNotifBody(raw)
	select {
	case p := <-ch:
		if p.ReqID != "r9" {
			t.Fatalf("req mismatch: %+v", p)
		}
	default:
		t.Fatal("pending waiter must resolve")
	}
}

func TestRequestPhoneNotifAppsOffline(t *testing.T) {
	svc := NewService("{}", "fp", "tok", NewLogBuffer(10))
	if _, err := svc.RequestPhoneNotifApps(true); err == nil {
		t.Fatal("offline fetch must fail so the caller falls back to known apps")
	}
}

func TestIsNotifAppsUnsupported(t *testing.T) {
	if !isNotifAppsUnsupported(ErrNotifAppsNotSupported) {
		t.Fatal("sentinel must match")
	}
	if !isNotifAppsUnsupported(errors.New("peer error BAD_REQUEST: unexpected message type")) {
		t.Fatal("old-phone wrong_type text must match")
	}
	if isNotifAppsUnsupported(errors.New("phone is offline — reconnect first")) {
		t.Fatal("offline must not look like an old phone")
	}
}

func TestFlattenNotifAppsSort(t *testing.T) {
	byPkg := map[string]*KnownNotifApp{
		"com.b": {PackageName: "com.b", App: "Bravo", Count: 1},
		"com.a": {PackageName: "com.a", App: "Alpha", Count: 1},
		"com.c": {PackageName: "com.c", App: "Charlie", Count: 5},
	}
	got := flattenNotifApps(byPkg, []string{"com.b", "com.a", "com.c"})
	if len(got) != 3 || got[0].PackageName != "com.c" || got[1].PackageName != "com.a" || got[2].PackageName != "com.b" {
		t.Fatalf("count-desc label-asc sort failed: %+v", got)
	}
}

