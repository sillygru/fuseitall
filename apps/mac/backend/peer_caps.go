// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"time"

	"fuseitall/core"
)

// checkPeerCapability verifies that the connected peer supports the required
// capability and build number. If the peer lacks the capability or is below
// minBuild, it records an update notice on s and returns *core.UpdateRequiredError.
// The notice names the peer's cached build as "current", never this Mac's
// build, so the message stays truthful when the cache is stale.
// A cached build below minBuild only fails fast when the version was verified
// from an authenticated contact recently (within peerTTL this session). A
// stale disk restore or a just-connected socket with no envelope yet skips
// the local gate and lets the live reply decide: the ack/pong sender heals
// the cache via learnPeerInfo, so a phone that already updated is never
// accused of running its old build.
func (s *Service) checkPeerCapability(requiredCap string, minBuild int) error {
	s.mu.Lock()
	build := s.peerBuild
	caps := s.peerCapabilities
	platform := s.peerPlatform
	learnedAt := s.peerLearnedAt
	s.mu.Unlock()

	if platform == "" {
		platform = "android"
	}

	peerLacks := false
	if build > 0 {
		if build >= minBuild {
			return nil
		}
		if learnedAt.IsZero() || time.Since(learnedAt) >= peerTTL {
			s.appendLine("peer version stale, skipping local gate cap=" + requiredCap)
			return nil
		}
		peerLacks = true
	} else if len(caps) > 0 {
		peerLacks = !core.IsCapabilitySupported(caps, requiredCap)
	}

	if peerLacks {
		upd := core.NewPeerUpdateRequiredPayload(platform, minBuild, build)
		s.setUpdateDetail(upd.Message, false, upd.RequiredVersion, upd.CurrentVersion, upd.RequiredBuild)
		return &core.UpdateRequiredError{
			Message:         upd.Message,
			RequiredBuild:   upd.RequiredBuild,
			RequiredVersion: upd.RequiredVersion,
			CurrentVersion:  upd.CurrentVersion,
			Device:          platform,
		}
	}
	return nil
}

// learnPeer folds an authenticated peer identity into the cached peer state.
// Every accepted contact refreshes it: inbound pings (via setPeerWithFacts),
// WebSocket envelopes, outbound pong/ack senders, and pushed file/photo
// responses. Fields are fail-soft: empty platform/version and non-positive
// builds keep the previous reading, and an empty capability list never wipes
// a known set. A satisfied update requirement clears the notice, and the
// remembered device persists best-effort. Pure adapter: no gating here.
func (s *Service) learnPeer(platform string, build int, version string, caps []string) {
	s.mu.Lock()
	changed := false
	now := time.Now()
	if platform != "" && platform != s.peerPlatform {
		s.peerPlatform = platform
		changed = true
	}
	if build > 0 {
		if build != s.peerBuild {
			s.peerBuild = build
			changed = true
		}
		// Any authenticated contact carrying a build re-verifies the cached
		// version, even when the value itself is unchanged.
		s.peerLearnedAt = now
	}
	if version != "" && version != s.peerVersion {
		s.peerVersion = version
		changed = true
	}
	if len(caps) > 0 && !equalStrings(caps, s.peerCapabilities) {
		s.peerCapabilities = append([]string{}, caps...)
		changed = true
	}
	// The peer updated past the requirement: the notice is stale, clear it.
	if s.lastUpdateSet && !s.lastUpdateSelf && s.lastUpdateReqBuild > 0 && s.peerBuild >= s.lastUpdateReqBuild {
		s.lastUpdateSet = false
		s.lastUpdateMsg = ""
		s.lastUpdateReqBuild = 0
		s.lastUpdateReqVer = ""
		s.lastUpdateCurVer = ""
		changed = true
	}
	var dev LastDevice
	if changed {
		dev = s.snapshotLastDeviceLocked()
		// Fresh contact ends any rotation window: the next failure is a new
		// incident and must be loud again.
		s.lastRotationKind = ""
		s.lastRotationLog = time.Time{}
	}
	s.mu.Unlock()
	if changed {
		s.persistSnapshot(dev)
	}
}

// learnPeerInfo folds a core.PeerInfo from an accepted pong/ack reply into
// the cached peer state. Thin: everything substantive lives in learnPeer.
func (s *Service) learnPeerInfo(peer core.PeerInfo) {
	s.learnPeer(peer.Platform, peer.AppBuild, peer.AppVersion, peer.Capabilities)
}

// equalStrings reports whether two capability lists hold the same entries in
// order. Pure.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
