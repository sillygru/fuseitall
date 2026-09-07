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

func TestValidClipboardMode(t *testing.T) {
	for _, m := range []string{ClipboardOff, ClipboardMacToPhone, ClipboardPhoneToMac, ClipboardTwoWay} {
		if !ValidClipboardMode(m) {
			t.Fatalf("mode %q should be valid", m)
		}
	}
	if ValidClipboardMode("both") {
		t.Fatal("unknown mode must be invalid")
	}
}

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
	if _, ok := SanitizeSettings(SettingsSyncPayload{ClipboardMode: "both"}); ok {
		t.Fatal("bad mode must fail")
	}
	got, ok := SanitizeSettings(SettingsSyncPayload{ClipboardMode: ClipboardTwoWay, UpdatedBy: "macos"})
	if !ok || got.UpdatedBy != OriginMac {
		t.Fatalf("macos normalize failed: %+v %v", got, ok)
	}
}

func TestRemoteSettingsWins(t *testing.T) {
	local := SettingsSyncPayload{ClipboardMode: ClipboardOff, UpdatedUnix: 10, UpdatedBy: OriginAndroid}
	remote := SettingsSyncPayload{ClipboardMode: ClipboardTwoWay, UpdatedUnix: 11, UpdatedBy: OriginAndroid}
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

func TestClipDirectionAllows(t *testing.T) {
	if !ClipDirectionAllows(ClipboardTwoWay, ClipboardMacToPhone) {
		t.Fatal("two-way must allow mac_to_phone")
	}
	if ClipDirectionAllows(ClipboardOff, ClipboardMacToPhone) {
		t.Fatal("off must block")
	}
	if !ClipDirectionAllows(ClipboardMacToPhone, ClipboardMacToPhone) {
		t.Fatal("one-way exact must allow")
	}
	if ClipDirectionAllows(ClipboardMacToPhone, ClipboardPhoneToMac) {
		t.Fatal("one-way reverse must block")
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
	if _, err := SendFeature(ctx, client, baseURL, token, sender, caps,
		TypeNotifPost, &NotifPostPayload{ID: "n1", Title: "Hi"}); err != nil {
		t.Fatalf("notif-post: %v", err)
	}
	if _, err := SendFeature(ctx, client, baseURL, token, sender, caps,
		TypeNotifDismiss, &NotifDismissPayload{ID: "n1"}); err != nil {
		t.Fatalf("notif-dismiss: %v", err)
	}
	if _, err := SendFeature(ctx, client, baseURL, token, sender, caps,
		TypeClipPush, &ClipPushPayload{Text: "hello", ChangedAt: 42, Origin: OriginAndroid}); err != nil {
		t.Fatalf("clip-push: %v", err)
	}
	if _, err := SendFeature(ctx, client, baseURL, token, sender, caps,
		TypeSettingsSync, &SettingsSyncPayload{ClipboardMode: ClipboardTwoWay, UpdatedUnix: 7}); err != nil {
		t.Fatalf("settings-sync: %v", err)
	}
	// Oversize clipboard fails closed.
	big := strings.Repeat("x", MaxClipLen+1)
	if _, err := SendFeature(ctx, client, baseURL, token, sender, caps,
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
	if _, err := SendFeature(ctx, client, baseURL, token, sender,
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
	if _, err := SendPing(ctx, client, baseURL, oldToken, sender,
		[]string{CapabilityPing}, time.Now()); err == nil {
		t.Fatal("old token ping = nil, want unauthorized")
	}
	if _, err := SendPing(ctx, client, baseURL, newToken, sender,
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
	_, err = SendFeature(ctx, client, baseURL, token, sender, []string{CapabilityPing},
		TypeNotifPost, &NotifPostPayload{ID: "n1"})
	if err == nil {
		t.Fatal("missing capability = nil, want update-required")
	}
	var upd *UpdateRequiredError
	if !errors.As(err, &upd) {
		t.Fatalf("err = %v, want *UpdateRequiredError", err)
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
