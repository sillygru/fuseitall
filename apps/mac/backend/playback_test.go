// SPDX-License-Identifier: AGPL-3.0-only

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
	// Unknown normalizes fail-soft to both (no error, like
	// clipboard): the stored value must be the default, not garbage.
	if _, err := s.SetPlaybackMode("bogus-mode"); err != nil {
		t.Fatalf("unknown must normalize, got error: %v", err)
	}
	if got := s.Get().PlaybackMode; got != core.PlaybackBoth {
		t.Fatalf("unknown must normalize to both, got %q", got)
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
	// Disabled mode fails loud.
	if _, err := svc.settings.SetPlaybackMode(core.PlaybackDisabled); err != nil {
		t.Fatalf("set disabled: %v", err)
	}
	if _, err := svc.SendPlaybackCmd("pause"); err == nil || err.Error() != "playback commands are off for this direction" {
		t.Fatalf("disabled mode must fail loud, got: %v", err)
	}
	// android_to_mac auto-promotes to both upon user-initiated command.
	if _, err := svc.settings.SetPlaybackMode(core.PlaybackAndroidToMac); err != nil {
		t.Fatalf("set android_to_mac: %v", err)
	}
	// Offline fails loud after auto-promotion.
	if _, err := svc.SendPlaybackCmd("pause"); err == nil || err.Error() != "phone is offline — reconnect first" {
		t.Fatalf("offline cmd must fail with offline error, got: %v", err)
	}
	if got := svc.settings.Get().PlaybackMode; got != core.PlaybackBoth {
		t.Fatalf("mode should auto-promote to both, got: %s", got)
	}
}

func TestPlaybackSystemMirrorNoPanic(t *testing.T) {
	SetSystemPlayback(nil)
	mirrorPlaybackToSystem("inapp", PlaybackView{}, nil)
	mirrorPlaybackToSystem("system", PlaybackView{HasState: true, State: "playing", Title: "T"}, nil)
	mirrorPlaybackToSystem("system", PlaybackView{}, nil)
}
