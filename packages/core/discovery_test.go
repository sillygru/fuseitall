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
