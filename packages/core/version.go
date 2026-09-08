// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

const (
	// CurrentProtocolV is the wire protocol version this build speaks.
	CurrentProtocolV = 1
	// CurrentBuild is this build's numeric build. It is the authoritative
	// gate: version strings are display-only, builds decide compatibility.
	CurrentBuild = 8
	// CurrentMinPeerBuild is the oldest peer build this build talks to.
	// Build 1 peers still ping; they simply lack the notifications,
	// clipboard, and settings-sync capabilities (gated per message).
	CurrentMinPeerBuild = 1
	// CurrentAppVersion is the human-readable marketing version for this
	// build (0.5.0 features: file manager). Single source of truth:
	// manifests (pubspec, package.json) mirror it; task version:check
	// enforces the match.
	CurrentAppVersion = "0.8.0"
)

// BuildToVersion maps a known build number to its human version. Unknown
// builds have no entry: callers fall back to build-only messaging.
var BuildToVersion = map[int]string{1: "0.1.0", 2: "0.2.0", 3: "0.3.0", 4: "0.4.0", 5: "0.5.0", 6: "0.6.0", 7: "0.7.0", 8: "0.8.0"}

// MinPeerBuildByProtocol maps a known protocol_v to the minimum peer build
// that speaks it. Unknown versions are rejected, never assumed.
var MinPeerBuildByProtocol = map[int]int{1: 1}

var (
	// ErrUnsupportedProtocol means the peer's protocol_v is unknown (higher
	// than ours, zero, or otherwise unmapped). Fail closed: reply
	// error/UPDATE_REQUIRED, never silent-drop.
	ErrUnsupportedProtocol = errors.New("unsupported protocol version")
	// ErrPeerOutdated means the peer's app_build is below our minimum.
	ErrPeerOutdated = errors.New("peer build outdated")
	// ErrLocalOutdated means our build is below the peer's minimum.
	ErrLocalOutdated = errors.New("local build outdated")
)

// SenderInfo identifies the sender of an envelope. AppVersion is the
// human-readable version (e.g. "0.1.0"), display-only and optional for
// backward compatibility: older peers omit it and gate purely on builds.
type SenderInfo struct {
	Platform     string `json:"platform"`
	AppBuild     int    `json:"app_build"`
	MinPeerBuild int    `json:"min_peer_build"`
	AppVersion   string `json:"app_version,omitempty"`
}

// CurrentSender stamps an outbound sender with this build's identity.
// Use it instead of hand-building SenderInfo so builds can't drift.
func CurrentSender(platform string) SenderInfo {
	return SenderInfo{
		Platform:     platform,
		AppBuild:     CurrentBuild,
		MinPeerBuild: CurrentMinPeerBuild,
		AppVersion:   CurrentAppVersion,
	}
}

// AppVersionForBuild returns the human version for a known build, or ""
// when the build is unknown (caller falls back to build-only messaging).
func AppVersionForBuild(build int) string {
	return BuildToVersion[build]
}

// Header is the gateable prefix of an envelope: everything needed to accept
// or reject a message before running any logic.
type Header struct {
	ProtocolV    int        `json:"protocol_v"`
	Type         string     `json:"type"`
	Sender       SenderInfo `json:"sender"`
	Capabilities []string   `json:"capabilities"`
}

// CheckProtocolVersion rejects any protocol_v we do not speak.
func CheckProtocolVersion(v int) error {
	if _, ok := MinPeerBuildByProtocol[v]; ok {
		return nil
	}
	return fmt.Errorf("protocol version %d: %w", v, ErrUnsupportedProtocol)
}

// CheckPeerVersion gates a peer's advertised build numbers. Local outdated
// is checked first: if we cannot satisfy the peer, saying so beats accusing
// the peer.
func CheckPeerVersion(sender SenderInfo) error {
	if CurrentBuild < sender.MinPeerBuild {
		return fmt.Errorf("local build %d below peer minimum %d: %w",
			CurrentBuild, sender.MinPeerBuild, ErrLocalOutdated)
	}
	if sender.AppBuild < CurrentMinPeerBuild {
		return fmt.Errorf("peer build %d below minimum %d: %w",
			sender.AppBuild, CurrentMinPeerBuild, ErrPeerOutdated)
	}
	return nil
}

// RequiredBuildFor returns the minimum peer build for a known protocol_v,
// or ErrUnsupportedProtocol for an unknown one.
func RequiredBuildFor(protocolV int) (int, error) {
	build, ok := MinPeerBuildByProtocol[protocolV]
	if !ok {
		return 0, fmt.Errorf("protocol version %d: %w", protocolV, ErrUnsupportedProtocol)
	}
	return build, nil
}

// NewUpdateRequiredPayload builds the canonical update payload. The message
// names both sides in human terms: "Update FuseItAll on <device> to
// <requiredVersion> (build >= N); current <currentVersion>". When the
// required build has no known version (future build we never mapped), it
// falls back to the legacy "Update FuseItAll on <device> to build >= N" so
// old readers still get a usable sentence. Builds gate; versions display.
func NewUpdateRequiredPayload(device string, requiredBuild int) UpdateRequiredPayload {
	requiredVersion := AppVersionForBuild(requiredBuild)
	msg := fmt.Sprintf("Update FuseItAll on %s to build >= %d", device, requiredBuild)
	if requiredVersion != "" {
		msg = fmt.Sprintf("Update FuseItAll on %s to %s (build >= %d); current %s (build %d)",
			device, requiredVersion, requiredBuild, CurrentAppVersion, CurrentBuild)
	}
	return UpdateRequiredPayload{
		Code:            CodeUpdateRequired,
		Message:         msg,
		RequiredBuild:   requiredBuild,
		RequiredVersion: requiredVersion,
		CurrentVersion:  CurrentAppVersion,
		CurrentBuild:    CurrentBuild,
		Device:          device,
	}
}

// NewPeerUpdateRequiredPayload builds the local-gate update payload: the
// peer must reach requiredBuild, and "current" names the peer's known build,
// never our own. Callers pass the cached peer build (0 when unknown); an
// unknown peer build falls back to the build-only sentence so the message
// never claims the peer already runs a version it may not have.
func NewPeerUpdateRequiredPayload(device string, requiredBuild, peerBuild int) UpdateRequiredPayload {
	requiredVersion := AppVersionForBuild(requiredBuild)
	peerVersion := AppVersionForBuild(peerBuild)
	msg := fmt.Sprintf("Update FuseItAll on %s to build >= %d", device, requiredBuild)
	switch {
	case requiredVersion != "" && peerVersion != "":
		msg = fmt.Sprintf("Update FuseItAll on %s to %s (build >= %d); current %s (build %d)",
			device, requiredVersion, requiredBuild, peerVersion, peerBuild)
	case requiredVersion != "":
		msg = fmt.Sprintf("Update FuseItAll on %s to %s (build >= %d)",
			device, requiredVersion, requiredBuild)
	}
	return UpdateRequiredPayload{
		Code:            CodeUpdateRequired,
		Message:         msg,
		RequiredBuild:   requiredBuild,
		RequiredVersion: requiredVersion,
		CurrentVersion:  peerVersion,
		CurrentBuild:    peerBuild,
		Device:          device,
	}
}

// NewEnvelope builds an outbound envelope stamped with our protocol_v.
// Capabilities are copied; a nil slice becomes [] so the required field is
// always present on the wire.
func NewEnvelope(msgType string, sender SenderInfo, capabilities []string, payload any) (Envelope, error) {
	var raw json.RawMessage
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return Envelope{}, fmt.Errorf("marshal %s payload: %w", msgType, err)
		}
		raw = encoded
	}
	caps := append([]string{}, capabilities...)
	return Envelope{
		ProtocolV:    CurrentProtocolV,
		Type:         msgType,
		Sender:       sender,
		Capabilities: caps,
		Payload:      raw,
	}, nil
}

// ParseAndGateHeader decodes only the header prefix first and gates
// protocol_v before the full decode, so a newer-protocol peer is rejected
// without trusting the rest of its bytes. It then gates peer versions.
// Unknown JSON fields are ignored by encoding/json.
func ParseAndGateHeader(raw []byte) (Header, error) {
	var pre struct {
		ProtocolV int `json:"protocol_v"`
	}
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&pre); err != nil {
		return Header{}, fmt.Errorf("decode envelope header: %w", err)
	}
	if err := CheckProtocolVersion(pre.ProtocolV); err != nil {
		return Header{}, err
	}
	var hdr Header
	if err := json.Unmarshal(raw, &hdr); err != nil {
		return Header{}, fmt.Errorf("decode envelope header: %w", err)
	}
	if err := CheckPeerVersion(hdr.Sender); err != nil {
		return hdr, err
	}
	return hdr, nil
}

// ParsePairQR decodes a scanned QR payload. A missing "v" means format 1;
// v > 1 is a newer QR we cannot read (ErrUnsupportedProtocol); unknown
// fields are ignored so older readers tolerate newer QRs' extras.
func ParsePairQR(raw []byte) (PairPayload, error) {
	var pre struct {
		V *int `json:"v"`
	}
	if err := json.Unmarshal(raw, &pre); err != nil {
		return PairPayload{}, fmt.Errorf("decode pair qr: %w", err)
	}
	version := 1
	if pre.V != nil {
		version = *pre.V
	}
	if version != 1 {
		return PairPayload{}, fmt.Errorf("pair qr version %d: %w", version, ErrUnsupportedProtocol)
	}
	var payload PairPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return PairPayload{}, fmt.Errorf("decode pair qr: %w", err)
	}
	if payload.V == 0 {
		payload.V = 1
	}
	return payload, nil
}

// EncodePairQR encodes a pair payload for QR display, normalized to v1.
func EncodePairQR(payload PairPayload) ([]byte, error) {
	payload.V = 1
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode pair qr: %w", err)
	}
	return raw, nil
}

// IsCapabilitySupported reports whether the peer's capability list contains
// the required capability.
func IsCapabilitySupported(caps []string, required string) bool {
	for _, cap := range caps {
		if cap == required {
			return true
		}
	}
	return false
}
