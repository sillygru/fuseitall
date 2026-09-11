// SPDX-License-Identifier: AGPL-3.0-only

package backend

import (
	"log/slog"
)

// SystemPlayback mirrors the in-app player into the OS surface (Control
// Center Now Playing). Implementations are fail-soft: errors log and fall
// back to in-app only, never fail the state ingest.
type SystemPlayback interface {
	SetState(v PlaybackView)
	Clear()
}

// systemPlayback is the process-wide mirror, swapped in tests.
var systemPlayback SystemPlayback = noopSystemPlayback{}

// SetSystemPlayback swaps the mirror (tests only).
func SetSystemPlayback(sp SystemPlayback) {
	if sp == nil {
		sp = noopSystemPlayback{}
	}
	systemPlayback = sp
}

type noopSystemPlayback struct{}

func (noopSystemPlayback) SetState(PlaybackView) {}
func (noopSystemPlayback) Clear()                {}

// mirrorPlaybackToSystem pushes the snapshot to the OS surface when the
// output mode is system. In-app rendering never depends on it.
func mirrorPlaybackToSystem(output string, v PlaybackView, logger *slog.Logger) {
	if output != "system" {
		return
	}
	func() {
		defer func() {
			_ = recover()
		}()
		if !v.HasState || v.State == "stopped" {
			systemPlayback.Clear()
			return
		}
		systemPlayback.SetState(v)
	}()
	if logger != nil {
		logger.Debug("playback mirrored to system", "has_state", v.HasState)
	}
}
