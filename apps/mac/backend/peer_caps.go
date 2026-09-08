// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"fuseitall/core"
)

// checkPeerCapability verifies that the connected peer supports the required
// capability and build number. If the peer lacks the capability or is below
// minBuild, it records an update notice on s and returns *core.UpdateRequiredError.
func (s *Service) checkPeerCapability(requiredCap string, minBuild int) error {
	s.mu.Lock()
	build := s.peerBuild
	caps := s.peerCapabilities
	platform := s.peerPlatform
	s.mu.Unlock()

	if platform == "" {
		platform = "android"
	}

	peerLacks := false
	if build > 0 {
		peerLacks = build < minBuild
	} else if len(caps) > 0 {
		peerLacks = !core.IsCapabilitySupported(caps, requiredCap)
	}

	if peerLacks {
		upd := core.NewUpdateRequiredPayload(platform, minBuild)
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
