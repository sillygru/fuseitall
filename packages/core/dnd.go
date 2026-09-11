// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package core

import (
	"strings"
)

// Do Not Disturb sync (capability dnd). Phone is the state source:
// dnd-state flows phone->Mac (or bidirectional); dnd-set flows Mac->phone.
// Every type rides the Envelope contract (packages/proto/dnd.json);
// decoders ignore unknown fields, encoders emit only known ones.
const (
	// TypeDNDState carries the current Do Not Disturb status.
	// Capability: CapabilityDND.
	TypeDNDState = "dnd-state"
	// TypeDNDSet carries a toggle command (Mac -> phone).
	// Capability: CapabilityDND.
	TypeDNDSet = "dnd-set"

	// CapabilityDND advertises Do Not Disturb control.
	CapabilityDND = "dnd"
)

// DNDStatePayload is the body of a TypeDNDState envelope.
// UpdatedMs orders snapshots (unix millis, greater wins).
type DNDStatePayload struct {
	Nonce         string `json:"nonce"`
	Enabled       bool   `json:"enabled"`
	UpdatedMs     int64  `json:"updated_ms,omitempty"`
	HasPermission bool   `json:"has_permission"`
	Origin        string `json:"origin,omitempty"`
}

// DNDSetPayload is the body of a TypeDNDSet envelope.
type DNDSetPayload struct {
	Nonce   string `json:"nonce"`
	Enabled bool   `json:"enabled"`
	Origin  string `json:"origin,omitempty"`
}

// SanitizeDNDState validates a DND state payload. Pure.
func SanitizeDNDState(p DNDStatePayload) (DNDStatePayload, bool) {
	if strings.TrimSpace(p.Nonce) == "" {
		return DNDStatePayload{}, false
	}
	if p.UpdatedMs < 0 {
		return DNDStatePayload{}, false
	}
	p.Origin = NormalizeOrigin(p.Origin)
	return p, true
}

// SanitizeDNDSet validates a DND set command payload. Pure.
func SanitizeDNDSet(p DNDSetPayload) (DNDSetPayload, bool) {
	if strings.TrimSpace(p.Nonce) == "" {
		return DNDSetPayload{}, false
	}
	p.Origin = NormalizeOrigin(p.Origin)
	return p, true
}
