// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Package core is the pure-Go backend for the FuseItAll BASE milestone:
// QR pairing, manual ping, and version gating. It never imports UI code.
package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

const (
	// TypePing is a manual ping request. Capability: CapabilityPing.
	TypePing = "ping"
	// TypePong is the reply to a ping, echoing the request nonce.
	TypePong = "pong"
	// TypeError carries a machine-readable failure. Never silent-drop:
	// every rejected request gets one of these.
	TypeError = "error"

	// CodeUpdateRequired means one side must update before talking.
	CodeUpdateRequired = "UPDATE_REQUIRED"
	// CodeUnauthorized means the pair token did not verify.
	CodeUnauthorized = "UNAUTHORIZED"
	// CodeBadRequest means the request was malformed or mistyped.
	CodeBadRequest = "BAD_REQUEST"

	// CapabilityPing advertises support for manual ping.
	CapabilityPing = "ping"
)

// ErrMissingPayload is returned when an envelope has no payload to decode.
var ErrMissingPayload = errors.New("missing payload")

// Envelope is the wire message. It is the only cross-language contract
// alongside the JSON schemas in packages/proto. Unknown fields are ignored
// on decode; encoders MUST NOT rely on extra fields being read.
type Envelope struct {
	ProtocolV    int             `json:"protocol_v"`
	Type         string          `json:"type"`
	Sender       SenderInfo      `json:"sender"`
	Capabilities []string        `json:"capabilities"`
	Payload      json.RawMessage `json:"payload,omitempty"`
}

// PingPayload is the body of a TypePing envelope. DeviceName, Model,
// BatteryPct, and Charging are the phone's self-advertised identity,
// carried on every ping including the heartbeat. All are optional and
// fail-soft: receivers ignore absent or invalid values and keep going.
// The Mac's local rename alias overrides DeviceName for display only.
type PingPayload struct {
	Nonce      string `json:"nonce"`
	SentAt     int64  `json:"sent_at"`
	DeviceName string `json:"device_name,omitempty"`
	Model      string `json:"model,omitempty"`
	BatteryPct *int   `json:"battery_pct,omitempty"`
	Charging   *bool  `json:"charging,omitempty"`
}

// MaxDeviceLabelLen caps advertised device_name/model lengths. Longer
// values are rejected by SanitizeDeviceLabel (fail-soft: field ignored).
const MaxDeviceLabelLen = 64

// SanitizeDeviceLabel trims an advertised label and reports whether it is
// usable. Pure. Empty or over-long input returns ok=false so callers keep
// their previous value instead of storing garbage.
func SanitizeDeviceLabel(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || len([]rune(trimmed)) > MaxDeviceLabelLen {
		return "", false
	}
	return trimmed, true
}

// SanitizeBatteryPct reports whether pct is a usable 0..100 level. Pure.
func SanitizeBatteryPct(pct int) bool {
	return pct >= 0 && pct <= 100
}

// PongPayload is the body of a TypePong envelope. Nonce echoes the ping.
type PongPayload struct {
	Nonce      string `json:"nonce"`
	ReceivedAt int64  `json:"received_at"`
}

// ErrorPayload is the body of a TypeError envelope.
type ErrorPayload struct {
	Code          string `json:"code"`
	Message       string `json:"message"`
	RequiredBuild int    `json:"required_build,omitempty"`
	Device        string `json:"device,omitempty"`
}

// UpdateRequiredPayload is the canonical body for error/UPDATE_REQUIRED.
// Build it with NewUpdateRequiredPayload so the message stays canonical.
type UpdateRequiredPayload struct {
	Code          string `json:"code"`
	Message       string `json:"message"`
	RequiredBuild int    `json:"required_build"`
	Device        string `json:"device"`
}

// UpdateRequiredError is returned by the client when the peer answers
// error/UPDATE_REQUIRED. Unwrap maps to ErrLocalOutdated when our build is
// below the required build, else ErrPeerOutdated.
type UpdateRequiredError struct {
	Device        string
	RequiredBuild int
	Message       string
	LocalOutdated bool
}

// Error implements the error interface.
func (e *UpdateRequiredError) Error() string { return e.Message }

// Unwrap returns ErrLocalOutdated or ErrPeerOutdated for errors.Is/As.
func (e *UpdateRequiredError) Unwrap() error {
	if e.LocalOutdated {
		return ErrLocalOutdated
	}
	return ErrPeerOutdated
}

// DecodePayload decodes an envelope payload into out. Fail closed on absent
// or malformed payloads.
func DecodePayload(env Envelope, out any) error {
	if len(env.Payload) == 0 {
		return fmt.Errorf("decode %s payload: %w", env.Type, ErrMissingPayload)
	}
	if reflect.TypeOf(out).Kind() != reflect.Pointer || reflect.ValueOf(out).IsNil() {
		return fmt.Errorf("decode %s payload: %w", env.Type, errors.New("out must be a non-nil pointer"))
	}
	if err := json.Unmarshal(env.Payload, out); err != nil {
		return fmt.Errorf("decode %s payload: %w", env.Type, err)
	}
	return nil
}
