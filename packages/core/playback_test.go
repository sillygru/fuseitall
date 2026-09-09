// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package core

import (
	"strings"
	"testing"
)

func TestNormalizePlaybackMode(t *testing.T) {
	if NormalizePlaybackMode("") != PlaybackAndroidToMac {
		t.Fatal("empty must default to android_to_mac")
	}
	if NormalizePlaybackMode("BOTH") != PlaybackBoth {
		t.Fatal("case-insensitive both failed")
	}
	if NormalizePlaybackMode("bogus") != PlaybackAndroidToMac {
		t.Fatal("unknown must default to android_to_mac")
	}
	if !IsValidPlaybackMode(PlaybackDisabled) || IsValidPlaybackMode("bogus") {
		t.Fatal("validity check failed")
	}
}

func TestPlaybackDirectionGates(t *testing.T) {
	if !PlaybackModeAllowsSend(PlaybackBoth, OriginMac) || !PlaybackModeAllowsSend(PlaybackBoth, OriginAndroid) {
		t.Fatal("both must allow either side")
	}
	if PlaybackModeAllowsSend(PlaybackDisabled, OriginMac) || PlaybackModeAllowsReceive(PlaybackDisabled, OriginAndroid) {
		t.Fatal("disabled must block all")
	}
	if !PlaybackModeAllowsSend(PlaybackAndroidToMac, OriginAndroid) || PlaybackModeAllowsSend(PlaybackAndroidToMac, OriginMac) {
		t.Fatal("android_to_mac must allow android only")
	}
	if !PlaybackModeAllowsSend(PlaybackMacToAndroid, OriginMac) || PlaybackModeAllowsSend(PlaybackMacToAndroid, OriginAndroid) {
		t.Fatal("mac_to_android must allow mac only")
	}
	if !PlaybackModeAllowsState(PlaybackAndroidToMac) || PlaybackModeAllowsCommand(PlaybackAndroidToMac) {
		t.Fatal("android_to_mac is view-only: state yes, command no")
	}
	if PlaybackModeAllowsState(PlaybackMacToAndroid) || !PlaybackModeAllowsCommand(PlaybackMacToAndroid) {
		t.Fatal("mac_to_android is blind remote: state no, command yes")
	}
}

func TestNormalizePlaybackOutput(t *testing.T) {
	if NormalizePlaybackOutput("") != PlaybackOutputInApp {
		t.Fatal("empty must default to inapp")
	}
	if NormalizePlaybackOutput("SYSTEM") != PlaybackOutputSystem {
		t.Fatal("system parse failed")
	}
	if NormalizePlaybackOutput("bogus") != PlaybackOutputInApp {
		t.Fatal("unknown must default to inapp")
	}
}

func TestSanitizePlaybackState(t *testing.T) {
	p := PlaybackStatePayload{Nonce: "n", Title: "  Song  ", State: "PLAYING", PositionMs: 1000, DurationMs: 200000, UpdatedMs: 5, Origin: "macos"}
	got, ok := SanitizePlaybackState(p)
	if !ok {
		t.Fatal("valid state rejected")
	}
	if got.Title != "Song" || got.State != PlaybackPlaying || got.Origin != OriginMac {
		t.Fatalf("normalize failed: %+v", got)
	}
	if _, ok := SanitizePlaybackState(PlaybackStatePayload{}); ok {
		t.Fatal("missing nonce accepted")
	}
	if _, ok := SanitizePlaybackState(PlaybackStatePayload{Nonce: "n", PositionMs: -1}); ok {
		t.Fatal("negative position accepted")
	}
	// Oversize artwork drops fail-soft, metadata kept.
	big := PlaybackStatePayload{Nonce: "n", Title: "T", UpdatedMs: 1, ArtworkB64: strings.Repeat("A", MaxPlaybackArtworkB64Len+1), ArtworkMime: "image/jpeg"}
	got, ok = SanitizePlaybackState(big)
	if !ok || got.ArtworkB64 != "" || got.Title != "T" {
		t.Fatal("oversize artwork must drop fail-soft")
	}
}

func TestSanitizePlaybackCmd(t *testing.T) {
	got, ok := SanitizePlaybackCmd(PlaybackCmdPayload{Nonce: "n", Cmd: "NEXT", Origin: "macos"})
	if !ok || got.Cmd != PlaybackCmdNext || got.Origin != OriginMac {
		t.Fatal("cmd normalize failed")
	}
	if _, ok := SanitizePlaybackCmd(PlaybackCmdPayload{Nonce: "n", Cmd: "dance"}); ok {
		t.Fatal("unknown cmd accepted")
	}
}

func TestRemotePlaybackWins(t *testing.T) {
	if !RemotePlaybackWins(10, 11) {
		t.Fatal("newer must win")
	}
	if RemotePlaybackWins(11, 11) || RemotePlaybackWins(11, 10) {
		t.Fatal("ties/stale must keep local")
	}
	if RemotePlaybackWins(5, 0) {
		t.Fatal("zero must never win")
	}
}

func TestShouldSendPlaybackState(t *testing.T) {
	if !ShouldSendPlaybackState(nil, PlaybackStatePayload{Title: "A"}, 1000) {
		t.Fatal("nil last must send")
	}
	last := &PlaybackStatePayload{Title: "A", State: PlaybackPlaying, UpdatedMs: 1000}
	if !ShouldSendPlaybackState(last, PlaybackStatePayload{Title: "B", State: PlaybackPlaying, UpdatedMs: 1001}, 1001) {
		t.Fatal("track change must send immediately")
	}
	if !ShouldSendPlaybackState(last, PlaybackStatePayload{Title: "A", State: PlaybackPaused, UpdatedMs: 1001}, 1001) {
		t.Fatal("state change must send immediately")
	}
	if ShouldSendPlaybackState(last, PlaybackStatePayload{Title: "A", State: PlaybackPlaying, UpdatedMs: 1001}, 2000) {
		t.Fatal("bare progress must throttle")
	}
	if !ShouldSendPlaybackState(last, PlaybackStatePayload{Title: "A", State: PlaybackPlaying, UpdatedMs: 7000}, 8000) {
		t.Fatal("progress after 5s must send")
	}
}

func TestSanitizeSettingsPlaybackAdditive(t *testing.T) {
	got, ok := SanitizeSettings(SettingsSyncPayload{PlaybackMode: "BOTH", PlaybackOutput: "SYSTEM"})
	if !ok || got.PlaybackMode != PlaybackBoth || got.PlaybackOutput != PlaybackOutputSystem {
		t.Fatalf("playback settings failed: %+v", got)
	}
	got, ok = SanitizeSettings(SettingsSyncPayload{})
	if !ok || got.PlaybackMode != PlaybackAndroidToMac || got.PlaybackOutput != PlaybackOutputInApp {
		t.Fatalf("absent playback must default view-only/inapp: %+v", got)
	}
}
