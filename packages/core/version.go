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
	// CurrentBuild is this build's numeric build.
	CurrentBuild = 1
	// CurrentMinPeerBuild is the oldest peer build this build talks to.
	CurrentMinPeerBuild = 1
)

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

// SenderInfo identifies the sender of an envelope.
type SenderInfo struct {
	Platform     string `json:"platform"`
	AppBuild     int    `json:"app_build"`
	MinPeerBuild int    `json:"min_peer_build"`
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
// format is frozen: "Update FuseItAll on <device> to build >= N".
func NewUpdateRequiredPayload(device string, requiredBuild int) UpdateRequiredPayload {
	return UpdateRequiredPayload{
		Code:          CodeUpdateRequired,
		Message:       fmt.Sprintf("Update FuseItAll on %s to build >= %d", device, requiredBuild),
		RequiredBuild: requiredBuild,
		Device:        device,
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
