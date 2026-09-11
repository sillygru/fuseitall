// SPDX-License-Identifier: AGPL-3.0-only

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

func TestSanitizeDeviceLabel(t *testing.T) {
	if got, ok := SanitizeDeviceLabel("  OnePlus 15R  "); !ok || got != "OnePlus 15R" {
		t.Fatalf("trimmed = (%q,%v), want (OnePlus 15R,true)", got, ok)
	}
	for _, bad := range []string{"", "   ", string(make([]rune, MaxDeviceLabelLen+1))} {
		if _, ok := SanitizeDeviceLabel(bad); ok {
			t.Fatalf("label %q must be rejected", bad)
		}
	}
}

func TestSanitizeBatteryPct(t *testing.T) {
	for _, pct := range []int{0, 55, 100} {
		if !SanitizeBatteryPct(pct) {
			t.Fatalf("pct %d must be accepted", pct)
		}
	}
	for _, pct := range []int{-1, 101} {
		if SanitizeBatteryPct(pct) {
			t.Fatalf("pct %d must be rejected", pct)
		}
	}
}

func TestPingPayloadDeviceFactsRoundTrip(t *testing.T) {
	pct := 78
	charging := true
	payload := PingPayload{Nonce: "n", SentAt: 1, DeviceName: "OnePlus 15R", Model: "CPH2767", BatteryPct: &pct, Charging: &charging}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var decoded PingPayload
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.DeviceName != "OnePlus 15R" || decoded.Model != "CPH2767" ||
		decoded.BatteryPct == nil || *decoded.BatteryPct != 78 ||
		decoded.Charging == nil || !*decoded.Charging {
		t.Fatalf("round trip = %+v", decoded)
	}
	// Unknown-free minimal payload still decodes (old senders).
	var minimal PingPayload
	if err := json.Unmarshal([]byte(`{"nonce":"n","sent_at":1}`), &minimal); err != nil {
		t.Fatal(err)
	}
	if minimal.BatteryPct != nil || minimal.Charging != nil {
		t.Fatalf("minimal = %+v, want absent battery", minimal)
	}
}
