// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"encoding/json"
	"testing"
)

func TestSanitizeDNDState(t *testing.T) {
	cases := []struct {
		name    string
		payload DNDStatePayload
		wantOK  bool
	}{
		{
			name: "valid enabled",
			payload: DNDStatePayload{
				Nonce:         "nonce-123",
				Enabled:       true,
				UpdatedMs:     1700000000,
				HasPermission: true,
				Origin:        "android",
			},
			wantOK: true,
		},
		{
			name: "missing nonce",
			payload: DNDStatePayload{
				Nonce:   "",
				Enabled: false,
			},
			wantOK: false,
		},
		{
			name: "negative timestamp",
			payload: DNDStatePayload{
				Nonce:     "n",
				UpdatedMs: -1,
			},
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := SanitizeDNDState(tc.payload)
			if ok != tc.wantOK {
				t.Fatalf("SanitizeDNDState() ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && got.Origin != "android" && tc.payload.Origin == "android" {
				t.Fatalf("Origin = %q, want android", got.Origin)
			}
		})
	}
}

func TestSanitizeDNDSet(t *testing.T) {
	valid := DNDSetPayload{
		Nonce:   "set-1",
		Enabled: true,
		Origin:  "mac",
	}
	got, ok := SanitizeDNDSet(valid)
	if !ok || got.Origin != "mac" || !got.Enabled {
		t.Fatalf("SanitizeDNDSet() = %+v, %v; want valid mac enabled", got, ok)
	}

	invalid := DNDSetPayload{
		Nonce: "",
	}
	if _, ok := SanitizeDNDSet(invalid); ok {
		t.Fatalf("SanitizeDNDSet() should reject empty nonce")
	}
}

func TestDNDEnvelopeSerialization(t *testing.T) {
	sender := CurrentSender("android")
	caps := []string{CapabilityPing, CapabilityDND}
	state := DNDStatePayload{
		Nonce:         "n1",
		Enabled:       true,
		UpdatedMs:     1234567,
		HasPermission: true,
		Origin:        "android",
	}

	env, err := NewEnvelope(TypeDNDState, sender, caps, state)
	if err != nil {
		t.Fatalf("NewEnvelope() error = %v", err)
	}

	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}

	var decoded Envelope
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal envelope error = %v", err)
	}

	var decodedPayload DNDStatePayload
	if err := DecodePayload(decoded, &decodedPayload); err != nil {
		t.Fatalf("DecodePayload error = %v", err)
	}

	if !decodedPayload.Enabled || !decodedPayload.HasPermission || decodedPayload.UpdatedMs != 1234567 {
		t.Fatalf("Decoded payload mismatch: %+v", decodedPayload)
	}
}
