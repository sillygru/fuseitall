// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSanitizeNotifID(t *testing.T) {
	if _, ok := SanitizeNotifID(""); ok {
		t.Fatal("empty id must fail")
	}
	if _, ok := SanitizeNotifID(strings.Repeat("x", MaxNotifIDLen+1)); ok {
		t.Fatal("over-long id must fail")
	}
	got, ok := SanitizeNotifID("  abc  ")
	if !ok || got != "abc" {
		t.Fatalf("trim failed: %q %v", got, ok)
	}
}

func TestTruncateNotifField(t *testing.T) {
	got := TruncateNotifField("  hello world  ", 5)
	if got != "hello" {
		t.Fatalf("truncate failed: %q", got)
	}
}

func TestSanitizeClipText(t *testing.T) {
	if _, ok := SanitizeClipText(strings.Repeat("x", MaxClipLen+1)); ok {
		t.Fatal("over-long clip must fail")
	}
	if _, ok := SanitizeClipText(""); !ok {
		t.Fatal("empty clip (cleared) must pass")
	}
}

func TestSanitizeSettings(t *testing.T) {
	if _, ok := SanitizeSettings(SettingsSyncPayload{UpdatedUnix: -1}); ok {
		t.Fatal("negative ts must fail")
	}
	got, ok := SanitizeSettings(SettingsSyncPayload{UpdatedBy: "macos"})
	if !ok || got.UpdatedBy != OriginMac {
		t.Fatalf("macos normalize failed: %+v %v", got, ok)
	}
}

func TestNotifFilterModes(t *testing.T) {
	if NormalizeNotifMode("") != NotifAllExceptMuted {
		t.Fatal("empty mode must default to all_except_muted")
	}
	if NormalizeNotifMode("ONLY_ALLOWED") != NotifOnlyAllowed {
		t.Fatal("mode normalize must be case-insensitive")
	}
	if NormalizeNotifMode("bogus") != NotifAllExceptMuted {
		t.Fatal("unknown mode must fall back to all_except_muted")
	}
	if !IsValidNotifMode(NotifOnlyAllowed) || !IsValidNotifMode(NotifAllExceptMuted) {
		t.Fatal("canonical modes must validate")
	}
	if IsValidNotifMode("bogus") {
		t.Fatal("bogus mode must not validate")
	}
}

func TestSanitizeNotifFilterList(t *testing.T) {
	got := SanitizeNotifFilterList(nil)
	if len(got) != 0 {
		t.Fatalf("nil must stay empty: %v", got)
	}
	got = SanitizeNotifFilterList([]string{"  ", "", "com.b", "com.a", "com.a", "com.b"})
	if len(got) != 2 || got[0] != "com.a" || got[1] != "com.b" {
		t.Fatalf("dedupe+sort failed: %v", got)
	}
	big := make([]string, 0, MaxNotifFilterApps+10)
	for i := 0; i < MaxNotifFilterApps+10; i++ {
		big = append(big, "com.example.app"+strings.Repeat("x", i%3)+string(rune('a'+i%26))+strings.Repeat("y", i/26))
	}
	got = SanitizeNotifFilterList(big)
	if len(got) > MaxNotifFilterApps {
		t.Fatalf("list must cap at %d, got %d", MaxNotifFilterApps, len(got))
	}
}

func TestShouldMirrorNotif(t *testing.T) {
	// Progress always drops, regardless of mode.
	if ShouldMirrorNotif(NotifAllExceptMuted, nil, nil, "com.whatsapp", true) {
		t.Fatal("progress must never mirror")
	}
	if ShouldMirrorNotif(NotifOnlyAllowed, nil, []string{"com.whatsapp"}, "com.whatsapp", true) {
		t.Fatal("progress must never mirror even when allowed")
	}
	// Denylist: everything except muted.
	if !ShouldMirrorNotif(NotifAllExceptMuted, nil, nil, "com.whatsapp", false) {
		t.Fatal("denylist empty must allow")
	}
	if ShouldMirrorNotif(NotifAllExceptMuted, []string{"com.whatsapp"}, nil, "com.whatsapp", false) {
		t.Fatal("muted package must drop")
	}
	if !ShouldMirrorNotif(NotifAllExceptMuted, []string{"com.other"}, nil, "com.whatsapp", false) {
		t.Fatal("non-muted package must allow")
	}
	// Allowlist: only listed packages.
	if ShouldMirrorNotif(NotifOnlyAllowed, nil, nil, "com.whatsapp", false) {
		t.Fatal("empty allowlist must drop")
	}
	if !ShouldMirrorNotif(NotifOnlyAllowed, nil, []string{"com.whatsapp"}, "com.whatsapp", false) {
		t.Fatal("allowed package must mirror")
	}
	if ShouldMirrorNotif(NotifOnlyAllowed, nil, []string{"com.other"}, "com.whatsapp", false) {
		t.Fatal("non-allowed package must drop")
	}
	// Unknown mode interoperates as allow-all.
	if !ShouldMirrorNotif("bogus", nil, nil, "com.whatsapp", false) {
		t.Fatal("unknown mode must fall back to allow")
	}
}

func TestIsStaleNotifPost(t *testing.T) {
	if IsStaleNotifPost(0, 1000, 300) {
		t.Fatal("unknown timestamp must be kept for backward compat")
	}
	if IsStaleNotifPost(900, 1000, 300) {
		t.Fatal("fresh post must be kept")
	}
	if !IsStaleNotifPost(100, 1000, 300) {
		t.Fatal("old post must be stale")
	}
}

func TestSanitizeSettingsNotifFilter(t *testing.T) {
	got, ok := SanitizeSettings(SettingsSyncPayload{
		NotifMode:       "ONLY_ALLOWED",
		MutedPackages:   []string{"com.b", "com.a", "com.a", "  "},
		AllowedPackages: []string{"com.x", "com.x"},
	})
	if !ok {
		t.Fatal("valid filter blob must pass")
	}
	if got.NotifMode != NotifOnlyAllowed {
		t.Fatalf("mode = %q, want only_allowed", got.NotifMode)
	}
	if len(got.MutedPackages) != 2 || got.MutedPackages[0] != "com.a" {
		t.Fatalf("muted sanitize failed: %v", got.MutedPackages)
	}
	if len(got.AllowedPackages) != 1 || got.AllowedPackages[0] != "com.x" {
		t.Fatalf("allowed sanitize failed: %v", got.AllowedPackages)
	}
}

func TestRemoteSettingsWins(t *testing.T) {
	local := SettingsSyncPayload{UpdatedUnix: 10, UpdatedBy: OriginAndroid}
	remote := SettingsSyncPayload{UpdatedUnix: 11, UpdatedBy: OriginAndroid}
	if !RemoteSettingsWins(local, remote) {
		t.Fatal("newer remote must win")
	}
	if RemoteSettingsWins(remote, local) {
		t.Fatal("older remote must lose")
	}
	// Tie goes to mac.
	a := SettingsSyncPayload{UpdatedUnix: 5, UpdatedBy: OriginAndroid}
	m := SettingsSyncPayload{UpdatedUnix: 5, UpdatedBy: OriginMac}
	if !RemoteSettingsWins(a, m) {
		t.Fatal("mac tie must win over android local")
	}
	if RemoteSettingsWins(m, a) {
		t.Fatal("android tie must lose to mac local")
	}
}

func TestRemoteClipWins(t *testing.T) {
	if !RemoteClipWins(10, 11) {
		t.Fatal("newer clip must win")
	}
	if RemoteClipWins(11, 11) {
		t.Fatal("tie must keep local")
	}
	if RemoteClipWins(11, 0) {
		t.Fatal("zero stamp must never win")
	}
}

func TestFeatureRoundTripTLS(t *testing.T) {
	token, err := RotatePairToken()
	if err != nil {
		t.Fatalf("RotatePairToken: %v", err)
	}
	srv, err := NewServer(token, "mac",
		[]string{CapabilityPing, CapabilityNotifications, CapabilityClipboard, CapabilitySettingsSync},
		nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	client, err := NewTOFUClient(srv.CertFingerprint())
	if err != nil {
		t.Fatalf("NewTOFUClient: %v", err)
	}
	baseURL := testServer(t, srv)
	sender := SenderInfo{Platform: "android", AppBuild: CurrentBuild, MinPeerBuild: CurrentMinPeerBuild}
	caps := []string{CapabilityNotifications, CapabilityClipboard, CapabilitySettingsSync}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, _, err := SendFeature(ctx, client, baseURL, token, sender, caps,
		TypeNotifPost, &NotifPostPayload{ID: "n1", Title: "Hi"}); err != nil {
		t.Fatalf("notif-post: %v", err)
	}
	if _, _, err := SendFeature(ctx, client, baseURL, token, sender, caps,
		TypeNotifDismiss, &NotifDismissPayload{ID: "n1"}); err != nil {
		t.Fatalf("notif-dismiss: %v", err)
	}
	if _, _, err := SendFeature(ctx, client, baseURL, token, sender, caps,
		TypeClipPush, &ClipPushPayload{Text: "hello", ChangedAt: 42, Origin: OriginAndroid}); err != nil {
		t.Fatalf("clip-push: %v", err)
	}
	if _, _, err := SendFeature(ctx, client, baseURL, token, sender, caps,
		TypeSettingsSync, &SettingsSyncPayload{UpdatedUnix: 7}); err != nil {
		t.Fatalf("settings-sync: %v", err)
	}
	// Oversize clipboard fails closed.
	big := strings.Repeat("x", MaxClipLen+1)
	if _, _, err := SendFeature(ctx, client, baseURL, token, sender, caps,
		TypeClipPush, &ClipPushPayload{Text: big}); err == nil {
		t.Fatal("oversize clip-push = nil, want BAD_REQUEST")
	}
}

func TestUnpairRoundTrip(t *testing.T) {
	token, err := RotatePairToken()
	if err != nil {
		t.Fatalf("RotatePairToken: %v", err)
	}
	srv, err := NewServer(token, "mac",
		[]string{CapabilityPing, CapabilityNotifications, CapabilityClipboard, CapabilitySettingsSync},
		nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	client, err := NewTOFUClient(srv.CertFingerprint())
	if err != nil {
		t.Fatalf("NewTOFUClient: %v", err)
	}
	baseURL := testServer(t, srv)
	sender := SenderInfo{Platform: "android", AppBuild: CurrentBuild, MinPeerBuild: CurrentMinPeerBuild}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, _, err := SendFeature(ctx, client, baseURL, token, sender,
		[]string{CapabilityPing}, TypeUnpair, &UnpairPayload{}); err != nil {
		t.Fatalf("unpair: %v", err)
	}
	if FeaturePath(TypeUnpair) != "/unpair" {
		t.Fatalf("FeaturePath(unpair) = %q, want /unpair", FeaturePath(TypeUnpair))
	}
}

func TestSetTokenRotatesAuth(t *testing.T) {
	oldToken, err := RotatePairToken()
	if err != nil {
		t.Fatalf("RotatePairToken: %v", err)
	}
	srv, err := NewServer(oldToken, "mac", []string{CapabilityPing}, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.SetToken(""); err == nil {
		t.Fatal("SetToken(empty) = nil, want error")
	}
	newToken, err := RotatePairToken()
	if err != nil {
		t.Fatalf("RotatePairToken: %v", err)
	}
	if err := srv.SetToken(newToken); err != nil {
		t.Fatalf("SetToken: %v", err)
	}
	client, err := NewTOFUClient(srv.CertFingerprint())
	if err != nil {
		t.Fatalf("NewTOFUClient: %v", err)
	}
	baseURL := testServer(t, srv)
	sender := SenderInfo{Platform: "android", AppBuild: CurrentBuild, MinPeerBuild: CurrentMinPeerBuild}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Old token must now 403; server_test asserts the unauthorized shape.
	if _, _, err := SendPing(ctx, client, baseURL, oldToken, sender,
		[]string{CapabilityPing}, time.Now()); err == nil {
		t.Fatal("old token ping = nil, want unauthorized")
	}
	if _, _, err := SendPing(ctx, client, baseURL, newToken, sender,
		[]string{CapabilityPing}, time.Now()); err != nil {
		t.Fatalf("new token ping: %v", err)
	}
}

func TestFeatureMissingCapabilityGatesUpdate(t *testing.T) {
	token, err := RotatePairToken()
	if err != nil {
		t.Fatalf("RotatePairToken: %v", err)
	}
	srv, err := NewServer(token, "mac", []string{CapabilityPing}, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	client, err := NewTOFUClient(srv.CertFingerprint())
	if err != nil {
		t.Fatalf("NewTOFUClient: %v", err)
	}
	baseURL := testServer(t, srv)
	sender := SenderInfo{Platform: "android", AppBuild: CurrentBuild, MinPeerBuild: CurrentMinPeerBuild}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Sender omits the notifications capability: server must answer
	// error/UPDATE_REQUIRED, surfaced as *UpdateRequiredError.
	_, _, err = SendFeature(ctx, client, baseURL, token, sender, []string{CapabilityPing},
		TypeNotifPost, &NotifPostPayload{ID: "n1"})
	if err == nil {
		t.Fatal("missing capability = nil, want update-required")
	}
	var upd *UpdateRequiredError
	if !errors.As(err, &upd) {
		t.Fatalf("err = %v, want *UpdateRequiredError", err)
	}
}

func TestSanitizeNotifAppsReq(t *testing.T) {
	if SanitizeNotifAppsReq(NotifAppsReqPayload{}) {
		t.Fatal("empty req must fail")
	}
	if !SanitizeNotifAppsReq(NotifAppsReqPayload{Nonce: "n", ReqID: "r1"}) {
		t.Fatal("minimal req must pass (defaults: first page, limit 50, icons)")
	}
	if !NotifAppsWantIcons(NotifAppsReqPayload{Nonce: "n", ReqID: "r1"}) {
		t.Fatal("absent with_icons must mean true")
	}
	off := false
	if NotifAppsWantIcons(NotifAppsReqPayload{WithIcons: &off}) {
		t.Fatal("explicit false must opt out of icons")
	}
	if SanitizeNotifAppsReq(NotifAppsReqPayload{Nonce: "n", ReqID: "r1", Limit: MaxNotifAppsPerResp + 1}) {
		t.Fatal("over-limit req must fail")
	}
	if SanitizeNotifAppsReq(NotifAppsReqPayload{Nonce: "n", ReqID: "  "}) {
		t.Fatal("blank req_id must fail")
	}
	if EffectiveNotifAppsLimit(0) != DefaultNotifAppsLimit {
		t.Fatal("limit 0 must mean default")
	}
}

func TestSanitizeNotifAppsResp(t *testing.T) {
	if SanitizeNotifAppsResp(NotifAppsRespPayload{}) {
		t.Fatal("empty resp must fail")
	}
	if !SanitizeNotifAppsResp(NotifAppsRespPayload{Nonce: "n", ReqID: "r1"}) {
		t.Fatal("empty entries must pass (last page)")
	}
	big := make([]NotifAppEntry, 0, MaxNotifAppsPerResp+1)
	for i := 0; i < MaxNotifAppsPerResp+1; i++ {
		big = append(big, NotifAppEntry{PackageName: "com.example.app"})
	}
	if SanitizeNotifAppsResp(NotifAppsRespPayload{Nonce: "n", ReqID: "r1", Entries: big}) {
		t.Fatalf("over-page resp must fail (got %d)", len(big))
	}
	if SanitizeNotifAppsResp(NotifAppsRespPayload{
		Nonce: "n", ReqID: "r1",
		Entries: []NotifAppEntry{{PackageName: "  "}},
	}) {
		t.Fatal("blank package must fail")
	}
	if SanitizeNotifAppsResp(NotifAppsRespPayload{
		Nonce: "n", ReqID: "r1",
		Entries: []NotifAppEntry{{PackageName: "com.ok", IconB64: "!!!not-base64!!!"}},
	}) {
		t.Fatal("bad icon must fail the page (sender must drop it, not send it)")
	}
	if !SanitizeNotifAppsResp(NotifAppsRespPayload{
		Nonce: "n", ReqID: "r1",
		Entries: []NotifAppEntry{{PackageName: "com.ok", App: "OK"}},
	}) {
		t.Fatal("valid row must pass")
	}
}

func TestNotifAppsRoundTripTLS(t *testing.T) {
	token, err := RotatePairToken()
	if err != nil {
		t.Fatalf("RotatePairToken: %v", err)
	}
	srv, err := NewServer(token, "mac",
		[]string{CapabilityPing, CapabilityNotifications},
		nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	client, err := NewTOFUClient(srv.CertFingerprint())
	if err != nil {
		t.Fatalf("NewTOFUClient: %v", err)
	}
	baseURL := testServer(t, srv)
	sender := SenderInfo{Platform: "android", AppBuild: CurrentBuild, MinPeerBuild: CurrentMinPeerBuild}
	caps := []string{CapabilityNotifications}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if FeaturePath(TypeNotifAppsReq) != "/notif" || FeaturePath(TypeNotifAppsResp) != "/notif" {
		t.Fatal("app inventory must ride the /notif lane")
	}
	if _, _, err := SendFeature(ctx, client, baseURL, token, sender, caps,
		TypeNotifAppsReq, &NotifAppsReqPayload{ReqID: "r1"}); err != nil {
		t.Fatalf("notif-apps-req: %v", err)
	}
	if _, _, err := SendFeature(ctx, client, baseURL, token, sender, caps,
		TypeNotifAppsResp, &NotifAppsRespPayload{ReqID: "r1", Entries: []NotifAppEntry{{PackageName: "com.ok"}}}); err != nil {
		t.Fatalf("notif-apps-resp: %v", err)
	}
	// Oversize page fails closed.
	huge := make([]NotifAppEntry, 0, MaxNotifAppsPerResp+1)
	for i := 0; i < MaxNotifAppsPerResp+1; i++ {
		huge = append(huge, NotifAppEntry{PackageName: "com.example.app"})
	}
	if _, _, err := SendFeature(ctx, client, baseURL, token, sender, caps,
		TypeNotifAppsResp, &NotifAppsRespPayload{ReqID: "r1", Entries: huge}); err == nil {
		t.Fatal("oversize notif-apps-resp = nil, want BAD_REQUEST")
	}
}

func TestFeatureEnvelopeRoundTrip(t *testing.T) {
	env, err := NewEnvelope(TypeNotifPost, CurrentSender("android"),
		[]string{CapabilityNotifications}, NotifPostPayload{ID: "n1", Title: "Hi"})
	if err != nil {
		t.Fatalf("new envelope: %v", err)
	}
	var p NotifPostPayload
	if err := DecodePayload(env, &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.ID != "n1" {
		t.Fatalf("id mismatch: %+v", p)
	}
}
