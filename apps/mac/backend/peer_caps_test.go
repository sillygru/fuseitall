// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"errors"
	"strings"
	"testing"
	"time"

	"fuseitall/core"
)

func TestParsePeerDeviceExtractsSenderAndCapabilities(t *testing.T) {
	raw := []byte(`{
		"protocol_v": 1,
		"type": "ping",
		"sender": {
			"platform": "android",
			"app_build": 6,
			"min_peer_build": 1,
			"app_version": "0.6.0"
		},
		"capabilities": ["ping", "notifications", "clipboard", "settings-sync", "files"],
		"payload": {
			"nonce": "n1",
			"reply_port": 18790,
			"device_name": "Pixel 8"
		}
	}`)

	facts := ParsePeerDevice(raw)
	if !facts.HasPlatform || facts.Platform != "android" {
		t.Fatalf("Platform = %q (has=%v), want android", facts.Platform, facts.HasPlatform)
	}
	if !facts.HasBuild || facts.AppBuild != 6 {
		t.Fatalf("AppBuild = %d (has=%v), want 6", facts.AppBuild, facts.HasBuild)
	}
	if !facts.HasVersion || facts.AppVersion != "0.6.0" {
		t.Fatalf("AppVersion = %q (has=%v), want 0.6.0", facts.AppVersion, facts.HasVersion)
	}
	if !facts.HasCaps || len(facts.Capabilities) != 5 {
		t.Fatalf("Capabilities = %v (has=%v), want 5 caps", facts.Capabilities, facts.HasCaps)
	}
	if core.IsCapabilitySupported(facts.Capabilities, core.CapabilityPhotos) {
		t.Fatal("0.6.0 peer unexpectedly reports photos capability")
	}
}

func TestPhotosGatedOnPeerBuildAndCapability(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))

	// Simulate 0.6.0 phone ping (build 6, no photos capability).
	pingBody := []byte(`{
		"protocol_v": 1,
		"type": "ping",
		"sender": {
			"platform": "android",
			"app_build": 6,
			"min_peer_build": 1,
			"app_version": "0.6.0"
		},
		"capabilities": ["ping", "notifications", "clipboard", "settings-sync", "files"],
		"payload": {
			"nonce": "n1",
			"reply_port": 18790,
			"device_name": "Pixel 8"
		}
	}`)
	port, fp, _ := ParsePeerPingFull(pingBody)
	facts := ParsePeerDevice(pingBody)
	svc.setPeerWithFacts("192.168.1.10", port, fp, facts)

	if !svc.IsPaired() {
		t.Fatal("phone should be paired after ping")
	}

	start := time.Now()
	res, err := svc.ListPhonePhotos("", 100)
	duration := time.Since(start)

	if duration > 2*time.Second {
		t.Fatalf("ListPhonePhotos took %v, want fast failure (< 2s, no 8s timeout)", duration)
	}
	if err == nil {
		t.Fatal("ListPhonePhotos on 0.6.0 peer want error, got nil")
	}
	if !errors.Is(err, core.ErrPeerOutdated) {
		t.Fatalf("err = %v, want ErrPeerOutdated", err)
	}
	if res.ErrorCode != core.CodeUpdateRequired {
		t.Fatalf("res.ErrorCode = %q, want %q", res.ErrorCode, core.CodeUpdateRequired)
	}
	if !strings.Contains(res.Error, "0.7.0") || !strings.Contains(res.Error, "build >= 7") {
		t.Fatalf("res.Error = %q, want message mentioning 0.7.0 and build >= 7", res.Error)
	}

	notice := svc.GetUpdateNotice()
	if !notice.Active {
		t.Fatal("UpdateNotice should be active")
	}
	if notice.Self {
		t.Fatal("UpdateNotice.Self should be false for peer outdated")
	}
	if notice.RequiredBuild != 7 || notice.RequiredVersion != "0.7.0" {
		t.Fatalf("UpdateNotice = %+v, want RequiredBuild=7, RequiredVersion=0.7.0", notice)
	}

	// RequestPhotoThumb should also be gated fast.
	_, errThumb := svc.RequestPhotoThumb("123", 256)
	if errThumb == nil || !errors.Is(errThumb, core.ErrPeerOutdated) {
		t.Fatalf("RequestPhotoThumb err = %v, want ErrPeerOutdated", errThumb)
	}

	// DeletePhonePhotos should also be gated fast.
	delRes, errDel := svc.DeletePhonePhotos([]string{"123"})
	if errDel == nil || !errors.Is(errDel, core.ErrPeerOutdated) {
		t.Fatalf("DeletePhonePhotos err = %v, want ErrPeerOutdated", errDel)
	}
	if !strings.Contains(delRes.Error, "0.7.0") {
		t.Fatalf("delRes.Error = %q, want 0.7.0 update message", delRes.Error)
	}

	// RequestPhonePhoto should also be gated fast.
	_, errPull := svc.RequestPhonePhoto("123", "")
	if errPull == nil || !errors.Is(errPull, core.ErrPeerOutdated) {
		t.Fatalf("RequestPhonePhoto err = %v, want ErrPeerOutdated", errPull)
	}
}

func TestPeerUpdateClearsNotice(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))

	// 1. Peer sends 0.6.0 ping.
	pingOld := []byte(`{
		"protocol_v": 1,
		"type": "ping",
		"sender": {"platform": "android", "app_build": 6, "min_peer_build": 1, "app_version": "0.6.0"},
		"capabilities": ["ping", "notifications", "clipboard", "settings-sync", "files"],
		"payload": {"nonce": "n1", "reply_port": 18790}
	}`)
	svc.setPeerWithFacts("192.168.1.10", 18790, "fp1", ParsePeerDevice(pingOld))

	// Trigger photo check to set notice.
	_, _ = svc.ListPhonePhotos("", 100)
	if !svc.GetUpdateNotice().Active {
		t.Fatal("expected active update notice on 0.6.0 peer")
	}

	// 2. Phone updates to 0.7.0 (build 7, even if ping only advertised ping capability).
	pingNew := []byte(`{
		"protocol_v": 1,
		"type": "ping",
		"sender": {"platform": "android", "app_build": 7, "min_peer_build": 1, "app_version": "0.7.0"},
		"capabilities": ["ping"],
		"payload": {"nonce": "n2", "reply_port": 18790}
	}`)
	svc.setPeerWithFacts("192.168.1.10", 18790, "fp1", ParsePeerDevice(pingNew))

	// Notice should now be automatically cleared.
	notice := svc.GetUpdateNotice()
	if notice.Active {
		t.Fatalf("update notice should be cleared after phone updated to 0.7.0, got %+v", notice)
	}

	// Peer capability check for photos must pass now.
	if err := svc.checkPeerCapability(core.CapabilityPhotos, 7); err != nil {
		t.Fatalf("checkPeerCapability for photos want nil on build 7, got %v", err)
	}
}

func TestLastDevicePersistsBuildAndCapabilities(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))

	ping := []byte(`{
		"protocol_v": 1,
		"type": "ping",
		"sender": {"platform": "android", "app_build": 6, "min_peer_build": 1, "app_version": "0.6.0"},
		"capabilities": ["ping", "notifications", "clipboard", "settings-sync", "files"],
		"payload": {"nonce": "n1", "reply_port": 18790, "device_name": "Galaxy S24"}
	}`)
	svc.setPeerWithFacts("192.168.1.20", 18790, "fp2", ParsePeerDevice(ping))

	last := svc.GetLastDevice()
	if last.DeviceName != "Galaxy S24" {
		t.Fatalf("DeviceName = %q, want Galaxy S24", last.DeviceName)
	}

	// Restart service from disk.
	svc2 := NewService("{}", "fp", "tok", NewLogBuffer(20))
	if svc2.peerBuild != 6 {
		t.Fatalf("svc2.peerBuild = %d, want 6", svc2.peerBuild)
	}
	if svc2.peerVersion != "0.6.0" {
		t.Fatalf("svc2.peerVersion = %q, want 0.6.0", svc2.peerVersion)
	}
	if svc2.peerPlatform != "android" {
		t.Fatalf("svc2.peerPlatform = %q, want android", svc2.peerPlatform)
	}
	if len(svc2.peerCapabilities) != 5 {
		t.Fatalf("svc2.peerCapabilities = %v, want 5 caps", svc2.peerCapabilities)
	}
}

func TestStaleFilesGateNamesPeerBuild(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))

	// Phone on build 4 (pre-files): the gate must fire, but "current" names
	// the peer's cached 0.4.0 — never this Mac's 0.7.0.
	pingOld := []byte(`{
		"protocol_v": 1,
		"type": "ping",
		"sender": {"platform": "android", "app_build": 4, "min_peer_build": 1, "app_version": "0.4.0"},
		"capabilities": ["ping", "notifications", "clipboard", "settings-sync"],
		"payload": {"nonce": "n1", "reply_port": 18790}
	}`)
	svc.setPeerWithFacts("192.168.1.10", 18790, "fp1", ParsePeerDevice(pingOld))

	err := svc.checkPeerCapability(core.CapabilityFiles, 5)
	if err == nil {
		t.Fatal("checkPeerCapability(files) on build 4 want error, got nil")
	}
	if !errors.Is(err, core.ErrPeerOutdated) {
		t.Fatalf("err = %v, want ErrPeerOutdated", err)
	}
	want := "Update FuseItAll on android to 0.5.0 (build >= 5); current 0.4.0 (build 4)"
	if err.Error() != want {
		t.Fatalf("err = %q, want %q", err.Error(), want)
	}
}

func TestLearnPeerHealsStaleFilesGate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))

	pingOld := []byte(`{
		"protocol_v": 1,
		"type": "ping",
		"sender": {"platform": "android", "app_build": 4, "min_peer_build": 1, "app_version": "0.4.0"},
		"capabilities": ["ping", "notifications", "clipboard", "settings-sync"],
		"payload": {"nonce": "n1", "reply_port": 18790}
	}`)
	svc.setPeerWithFacts("192.168.1.10", 18790, "fp1", ParsePeerDevice(pingOld))

	// Gate fires on the stale cache and arms the notice.
	if err := svc.checkPeerCapability(core.CapabilityFiles, 5); err == nil {
		t.Fatal("checkPeerCapability(files) on build 4 want error, got nil")
	}
	if !svc.GetUpdateNotice().Active {
		t.Fatal("want active update notice after stale gate")
	}

	// Phone updates to 0.7.0: the next authenticated contact (pong sender,
	// feature ack, pushed response) heals the cache immediately.
	svc.learnPeer("android", 7, "0.7.0",
		[]string{"ping", "notifications", "clipboard", "settings-sync", "files", "photos"})

	if err := svc.checkPeerCapability(core.CapabilityFiles, 5); err != nil {
		t.Fatalf("checkPeerCapability(files) after heal = %v, want nil", err)
	}
	if notice := svc.GetUpdateNotice(); notice.Active {
		t.Fatalf("notice = %+v, want cleared after heal", notice)
	}
	// The healed build persists across restarts.
	svc2 := NewService("{}", "fp", "tok", NewLogBuffer(20))
	if svc2.peerBuild != 7 {
		t.Fatalf("svc2.peerBuild = %d, want 7", svc2.peerBuild)
	}
}

func TestIngestFileRespLearnsPeer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))

	pingOld := []byte(`{
		"protocol_v": 1,
		"type": "ping",
		"sender": {"platform": "android", "app_build": 4, "min_peer_build": 1, "app_version": "0.4.0"},
		"capabilities": ["ping", "notifications", "clipboard", "settings-sync"],
		"payload": {"nonce": "n1", "reply_port": 18790}
	}`)
	svc.setPeerWithFacts("192.168.1.10", 18790, "fp1", ParsePeerDevice(pingOld))

	// A pushed file-list-resp from the updated phone carries its sender: the
	// HTTP ingest path must learn it even though no inbound ping arrived.
	resp := []byte(`{
		"protocol_v": 1,
		"type": "file-list-resp",
		"sender": {"platform": "android", "app_build": 7, "min_peer_build": 1, "app_version": "0.7.0"},
		"capabilities": ["ping", "files"],
		"payload": {"nonce": "n9", "req_id": "r1", "entries": []}
	}`)
	svc.ingestFileBody(resp)
	if svc.peerBuild != 7 {
		t.Fatalf("peerBuild = %d, want 7 after file-list-resp", svc.peerBuild)
	}
	if err := svc.checkPeerCapability(core.CapabilityFiles, 5); err != nil {
		t.Fatalf("checkPeerCapability(files) after resp = %v, want nil", err)
	}
}

func TestFreshBuildOneGateNamesBuildOne(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))

	// The exact user report shape: a recently verified build-1 phone fails
	// the files gate with "current 0.1.0 (build 1)". Fresh verification
	// keeps the fast fail so genuinely old peers never wait out a timeout.
	pingOld := []byte(`{
		"protocol_v": 1,
		"type": "ping",
		"sender": {"platform": "android", "app_build": 1, "min_peer_build": 1, "app_version": "0.1.0"},
		"capabilities": ["ping"],
		"payload": {"nonce": "n1", "reply_port": 18790}
	}`)
	svc.setPeerWithFacts("192.168.1.10", 18790, "fp1", ParsePeerDevice(pingOld))

	err := svc.checkPeerCapability(core.CapabilityFiles, 5)
	if err == nil {
		t.Fatal("checkPeerCapability(files) on fresh build 1 want error, got nil")
	}
	if !errors.Is(err, core.ErrPeerOutdated) {
		t.Fatalf("err = %v, want ErrPeerOutdated", err)
	}
	want := "Update FuseItAll on android to 0.5.0 (build >= 5); current 0.1.0 (build 1)"
	if err.Error() != want {
		t.Fatalf("err = %q, want %q", err.Error(), want)
	}
}

func TestStaleDiskBuildSkipsLocalGate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))

	// Simulate a disk restore with no contact this session: build present,
	// peerLearnedAt zero. The gate must not accuse the phone of its old
	// build; the live reply decides and heals via learnPeerInfo.
	svc.mu.Lock()
	svc.peerPlatform = "android"
	svc.peerBuild = 1
	svc.peerVersion = "0.1.0"
	svc.peerCapabilities = []string{"ping"}
	svc.mu.Unlock()

	if err := svc.checkPeerCapability(core.CapabilityFiles, 5); err != nil {
		t.Fatalf("checkPeerCapability(files) on unverified build 1 = %v, want nil (skip)", err)
	}
	if notice := svc.GetUpdateNotice(); notice.Active {
		t.Fatalf("stale skip must not arm an update notice, got %+v", notice)
	}

	// The next authenticated contact re-verifies: same old build now fails
	// fast (trusted), a new build passes.
	svc.learnPeer("android", 1, "0.1.0", []string{"ping"})
	if err := svc.checkPeerCapability(core.CapabilityFiles, 5); err == nil {
		t.Fatal("checkPeerCapability(files) on verified build 1 want error, got nil")
	}
	svc.learnPeer("android", 10, "0.10.0",
		[]string{"ping", "notifications", "clipboard", "settings-sync", "files", "photos", "playback"})
	if err := svc.checkPeerCapability(core.CapabilityFiles, 5); err != nil {
		t.Fatalf("checkPeerCapability(files) after 0.10.0 heal = %v, want nil", err)
	}
}

func TestExpiredVersionSkipsLocalGate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))

	pingOld := []byte(`{
		"protocol_v": 1,
		"type": "ping",
		"sender": {"platform": "android", "app_build": 4, "min_peer_build": 1, "app_version": "0.4.0"},
		"capabilities": ["ping", "notifications", "clipboard", "settings-sync"],
		"payload": {"nonce": "n1", "reply_port": 18790}
	}`)
	svc.setPeerWithFacts("192.168.1.10", 18790, "fp1", ParsePeerDevice(pingOld))

	// Age the verification past peerTTL without any new contact: the cached
	// build is no longer trustworthy, so the gate skips instead of accusing.
	svc.mu.Lock()
	svc.peerLearnedAt = time.Now().Add(-(peerTTL + time.Second))
	svc.mu.Unlock()

	if err := svc.checkPeerCapability(core.CapabilityFiles, 5); err != nil {
		t.Fatalf("checkPeerCapability(files) on expired build 4 = %v, want nil (skip)", err)
	}
	if notice := svc.GetUpdateNotice(); notice.Active {
		t.Fatalf("expired skip must not arm an update notice, got %+v", notice)
	}
}

func TestForgetClearsCachedPeerVersion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc, _, _ := testPairingService(t, NewLogBuffer(20))

	ping := []byte(`{
		"protocol_v": 1,
		"type": "ping",
		"sender": {"platform": "android", "app_build": 7, "min_peer_build": 1, "app_version": "0.7.0"},
		"capabilities": ["ping", "files"],
		"payload": {"nonce": "n1", "reply_port": 18790}
	}`)
	svc.setPeerWithFacts("192.168.1.10", 18790, "fp1", ParsePeerDevice(ping))

	// Forget succeeds (rotation wired via testPairingService) and must drop
	// the cached version with the identity so the next pairing starts clean.
	if _, err := svc.ForgetLastDevice(); err != nil {
		t.Fatalf("forget = %v, want nil", err)
	}
	svc.mu.Lock()
	build, version, caps, learnedAt := svc.peerBuild, svc.peerVersion, svc.peerCapabilities, svc.peerLearnedAt
	platform := svc.peerPlatform
	svc.mu.Unlock()
	if build != 0 || version != "" || platform != "" || len(caps) != 0 || !learnedAt.IsZero() {
		t.Fatalf("forget must clear peer version, got build=%d version=%q platform=%q caps=%v learnedAt=%v",
			build, version, platform, caps, learnedAt)
	}
	if notice := svc.GetUpdateNotice(); notice.Active {
		t.Fatalf("forget must clear the update notice, got %+v", notice)
	}
}

func TestUnpairClearsCachedPeerVersion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc, _, _ := testPairingService(t, NewLogBuffer(20))

	ping := []byte(`{
		"protocol_v": 1,
		"type": "ping",
		"sender": {"platform": "android", "app_build": 7, "min_peer_build": 1, "app_version": "0.7.0"},
		"capabilities": ["ping", "files"],
		"payload": {"nonce": "n1", "reply_port": 18790}
	}`)
	svc.setPeerWithFacts("192.168.1.10", 18790, "fp1", ParsePeerDevice(ping))
	svc.ingestUnpairBody()

	svc.mu.Lock()
	build, version, caps, learnedAt := svc.peerBuild, svc.peerVersion, svc.peerCapabilities, svc.peerLearnedAt
	platform := svc.peerPlatform
	svc.mu.Unlock()
	if build != 0 || version != "" || platform != "" || len(caps) != 0 || !learnedAt.IsZero() {
		t.Fatalf("goodbye must clear peer version, got build=%d version=%q platform=%q caps=%v learnedAt=%v",
			build, version, platform, caps, learnedAt)
	}
}
