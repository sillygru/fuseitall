// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"testing"

	"fuseitall/core"
)

func TestPlaybackStoreLatestWins(t *testing.T) {
	s := NewPlaybackStore()
	if s.Get().State != core.PlaybackStopped {
		t.Fatal("empty must be stopped")
	}
	if !s.ApplyRemote(core.PlaybackStatePayload{Nonce: "a", Title: "Song", State: "playing", UpdatedMs: 10, Origin: "android"}) {
		t.Fatal("first snapshot must adopt")
	}
	if s.ApplyRemote(core.PlaybackStatePayload{Nonce: "b", Title: "Old", UpdatedMs: 5}) {
		t.Fatal("stale must not win")
	}
	if got := s.Get(); got.Title != "Song" {
		t.Fatalf("title = %q", got.Title)
	}
}

func TestPlaybackDirectionSetters(t *testing.T) {
	s := NewSettingsStore()
	if _, err := s.SetPlaybackMode("both"); err != nil {
		t.Fatalf("set both: %v", err)
	}
	// Unknown normalizes fail-soft to android_to_mac (no error, like
	// clipboard): the stored value must be the default, not garbage.
	if _, err := s.SetPlaybackMode("bogus-mode"); err != nil {
		t.Fatalf("unknown must normalize, got error: %v", err)
	}
	if got := s.Get().PlaybackMode; got != core.PlaybackAndroidToMac {
		t.Fatalf("unknown must normalize to android_to_mac, got %q", got)
	}
	if _, err := s.SetPlaybackOutput("system"); err != nil {
		t.Fatalf("set system: %v", err)
	}
	if got := s.Get().PlaybackOutput; got != core.PlaybackOutputSystem {
		t.Fatalf("output = %q", got)
	}
}

func TestPlaybackCmdValidation(t *testing.T) {
	svc := NewService("", "", "tok", NewLogBuffer(10))
	if _, err := svc.SendPlaybackCmd("dance"); err == nil {
		t.Fatal("unknown cmd accepted")
	}
	// Offline must fail loud, not silent.
	if _, err := svc.SendPlaybackCmd("pause"); err == nil {
		t.Fatal("offline cmd must fail")
	}
}

func TestPlaybackSystemMirrorNoPanic(t *testing.T) {
	SetSystemPlayback(nil)
	mirrorPlaybackToSystem("inapp", PlaybackView{}, nil)
	mirrorPlaybackToSystem("system", PlaybackView{HasState: true, State: "playing", Title: "T"}, nil)
	mirrorPlaybackToSystem("system", PlaybackView{}, nil)
}
