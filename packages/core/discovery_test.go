// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package core

import "testing"

func TestDiscoveryTXTRoundTrip(t *testing.T) {
	rec := DiscoveryRecord{Fingerprint: "ABCDEF", Host: "192.168.1.5", Port: 18789, Build: 2}
	m := BuildDiscoveryTXT(rec)
	got, ok := ParseDiscoveryTXT(m)
	if !ok {
		t.Fatal("expected ok")
	}
	if got.Host != "192.168.1.5" || got.Port != 18789 || got.Fingerprint != "abcdef" {
		t.Fatalf("unexpected record: %+v", got)
	}
}

func TestMergeCandidateHostsDedupes(t *testing.T) {
	got := MergeCandidateHosts("192.168.1.6", []string{"192.168.1.5", "192.168.1.6", "bad"})
	if len(got) != 2 || got[0] != "192.168.1.6" || got[1] != "192.168.1.5" {
		t.Fatalf("unexpected merge: %v", got)
	}
}

func TestOrderedPeerTargetsPrimaryFirst(t *testing.T) {
	got := OrderedPeerTargets("192.168.1.6", 18789, []string{"192.168.1.5", "192.168.1.6"})
	if len(got) != 2 {
		t.Fatalf("unexpected targets: %v", got)
	}
	if got[0] != "192.168.1.6:18789" {
		t.Fatalf("primary must be first: %v", got)
	}
}

func TestDiscoveryBeaconRoundTrip(t *testing.T) {
	rec := DiscoveryRecord{Fingerprint: "AABBCCDD", Host: "10.0.0.25", Port: 18789, Build: 3}
	data, err := EncodeDiscoveryBeacon(rec)
	if err != nil {
		t.Fatalf("encode beacon: %v", err)
	}
	got, err := ParseDiscoveryBeacon(data)
	if err != nil {
		t.Fatalf("parse beacon: %v", err)
	}
	if got.Host != "10.0.0.25" || got.Port != 18789 || got.Fingerprint != "aabbccdd" || got.V != 1 {
		t.Fatalf("unexpected beacon: %+v", got)
	}
}

func TestDiscoveryBeaconValidation(t *testing.T) {
	// Out of range port
	badPort := []byte(`{"v":1,"fp":"abc","host":"10.0.0.1","port":99999}`)
	if _, err := ParseDiscoveryBeacon(badPort); err == nil {
		t.Fatal("expected error on out-of-range port")
	}
	// Missing host
	noHost := []byte(`{"v":1,"fp":"abc","host":"","port":18789}`)
	if _, err := ParseDiscoveryBeacon(noHost); err == nil {
		t.Fatal("expected error on empty host")
	}
	// Future version
	futureV := []byte(`{"v":99,"fp":"abc","host":"10.0.0.1","port":18789}`)
	if _, err := ParseDiscoveryBeacon(futureV); err == nil {
		t.Fatal("expected error on future version")
	}
}

func TestDiscoveryProbeRoundTrip(t *testing.T) {
	data, err := EncodeDiscoveryProbe("AABB1122")
	if err != nil {
		t.Fatalf("encode probe: %v", err)
	}
	got, err := ParseDiscoveryProbe(data)
	if err != nil {
		t.Fatalf("parse probe: %v", err)
	}
	if got.Fingerprint != "aabb1122" || got.Type != "probe" || got.V != 1 {
		t.Fatalf("unexpected probe: %+v", got)
	}
}

func TestDiscoveryProbeValidation(t *testing.T) {
	// Not a probe
	notProbe := []byte(`{"v":1,"type":"beacon","fp":"aabb"}`)
	if _, err := ParseDiscoveryProbe(notProbe); err == nil {
		t.Fatal("expected error on non-probe type")
	}
	// Empty fingerprint
	noFP := []byte(`{"v":1,"type":"probe","fp":""}`)
	if _, err := ParseDiscoveryProbe(noFP); err == nil {
		t.Fatal("expected error on empty fp")
	}
}

