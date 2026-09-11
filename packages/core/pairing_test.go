// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"errors"
	"testing"
)

func TestIdentity(t *testing.T) {
	a, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity = %v, want nil", err)
	}
	b, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity = %v, want nil", err)
	}
	if string(a.PublicKey) == string(b.PublicKey) {
		t.Fatal("two identities share a public key, want distinct")
	}
	fp := Fingerprint(a.PublicKey)
	if len(fp) != 64 {
		t.Fatalf("Fingerprint length = %d, want 64 hex chars", len(fp))
	}
	if fp != Fingerprint(a.PublicKey) {
		t.Fatal("Fingerprint unstable across calls")
	}
	if fp == Fingerprint(b.PublicKey) {
		t.Fatal("distinct keys share a fingerprint")
	}
}

func TestPairPayloadRoundTrip(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity = %v", err)
	}
	token, err := RotatePairToken()
	if err != nil {
		t.Fatalf("RotatePairToken = %v", err)
	}
	made := MakePairPayload(" test-mac ", "mac", "10.0.0.2", 8443, zeros64(), id.PublicKey, token)
	if made.V != 1 {
		t.Fatalf("MakePairPayload V = %d, want 1", made.V)
	}
	if made.Code != PairingCode(token) || len(made.Code) != 6 {
		t.Fatalf("MakePairPayload Code = %q, want PairingCode(token)", made.Code)
	}
	raw, err := EncodePairQR(made)
	if err != nil {
		t.Fatalf("EncodePairQR = %v", err)
	}
	got, err := ParsePairQR(raw)
	if err != nil {
		t.Fatalf("ParsePairQR = %v", err)
	}
	if got != made {
		t.Fatalf("round trip = %+v, want %+v", got, made)
	}
}

func TestPairingCode(t *testing.T) {
	a, b := PairingCode("token-1"), PairingCode("token-1")
	if a != b {
		t.Fatalf("PairingCode not deterministic: %q vs %q", a, b)
	}
	if len(a) != 6 {
		t.Fatalf("PairingCode length = %d, want 6", len(a))
	}
	for _, r := range a {
		if r < '0' || r > '9' {
			t.Fatalf("PairingCode %q not all digits", a)
		}
	}
	if got := PairingCode("token-2"); got == a {
		t.Fatalf("PairingCode collision across tokens: %q", got)
	}
	if got := PairingCode(""); len(got) != 6 {
		t.Fatalf("PairingCode(\"\") length = %d, want 6", len(got))
	}
}

func TestVerifyToken(t *testing.T) {
	token, err := RotatePairToken()
	if err != nil {
		t.Fatalf("RotatePairToken = %v", err)
	}
	if !VerifyToken(token, token) {
		t.Fatal("VerifyToken(same) = false, want true")
	}
	if VerifyToken(token, "wrong-token-value-00000000000000") {
		t.Fatal("VerifyToken(wrong) = true, want false")
	}
	if VerifyToken(token, token[:len(token)-1]) {
		t.Fatal("VerifyToken(shorter) = true, want false (fail closed on length)")
	}
	if VerifyToken(token, "") {
		t.Fatal("VerifyToken(empty) = true, want false")
	}
}

func TestRotatePairToken(t *testing.T) {
	a, err := RotatePairToken()
	if err != nil {
		t.Fatalf("RotatePairToken = %v", err)
	}
	b, err := RotatePairToken()
	if err != nil {
		t.Fatalf("RotatePairToken = %v", err)
	}
	if len(a) != 32 {
		t.Fatalf("token length = %d, want 32 hex chars (128-bit)", len(a))
	}
	if a == b {
		t.Fatal("two rotated tokens match, want unique")
	}
}

func TestUpdateRequiredErrorUnwrap(t *testing.T) {
	local := &UpdateRequiredError{Device: "mac", RequiredBuild: CurrentBuild + 1, Message: "m", LocalOutdated: true}
	if !errors.Is(local, ErrLocalOutdated) {
		t.Fatal("local update error does not match ErrLocalOutdated")
	}
	peer := &UpdateRequiredError{Device: "android", RequiredBuild: CurrentBuild, Message: "m"}
	if !errors.Is(peer, ErrPeerOutdated) {
		t.Fatal("peer update error does not match ErrPeerOutdated")
	}
}
