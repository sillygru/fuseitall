// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package core

import (
	"encoding/json"
	"testing"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	sender := SenderInfo{Platform: "mac", AppBuild: CurrentBuild, MinPeerBuild: CurrentMinPeerBuild}
	env, err := NewEnvelope(TypePing, sender, []string{CapabilityPing}, PingPayload{Nonce: "n", SentAt: 1})
	if err != nil {
		t.Fatalf("NewEnvelope = %v", err)
	}
	if env.ProtocolV != CurrentProtocolV || env.Type != TypePing {
		t.Fatalf("envelope = %+v, want stamped protocol_v/type", env)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal = %v", err)
	}
	var decoded Envelope
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal = %v", err)
	}
	var ping PingPayload
	if err := DecodePayload(decoded, &ping); err != nil {
		t.Fatalf("DecodePayload = %v", err)
	}
	if ping.Nonce != "n" {
		t.Fatalf("nonce = %q, want %q", ping.Nonce, "n")
	}
}

func TestDecodePayloadMissing(t *testing.T) {
	env := Envelope{ProtocolV: 1, Type: TypePong}
	var pong PongPayload
	if err := DecodePayload(env, &pong); err == nil {
		t.Fatal("DecodePayload(empty) = nil, want fail-closed error")
	}
}

func TestNewEnvelopeNilCaps(t *testing.T) {
	env, err := NewEnvelope(TypePing, SenderInfo{}, nil, nil)
	if err != nil {
		t.Fatalf("NewEnvelope = %v", err)
	}
	if env.Capabilities == nil {
		t.Fatal("capabilities = nil, want [] so the required field is present")
	}
}
