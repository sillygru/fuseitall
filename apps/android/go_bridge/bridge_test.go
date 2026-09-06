// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package main

import "testing"

// The phone-side server must bind all interfaces so the LAN peer (Mac)
// can connect to the advertised reply_port over Wi-Fi. 127.0.0.1 would
// loopback-isolate it (connection refused from the Mac).
func TestPhoneBindAddrExposesLAN(t *testing.T) {
	if phoneBindAddr != "0.0.0.0" {
		t.Fatalf("phoneBindAddr = %q, want %q", phoneBindAddr, "0.0.0.0")
	}
}

func startWithRandomPort(t *testing.T, start func(token string, port int) (string, bool)) string {
	t.Helper()
	raw, ok := start("pair-token", 0)
	if !ok {
		t.Fatal("start must succeed on ephemeral port")
	}
	t.Cleanup(func() { _ = goStop() })
	return raw
}

func fingerprintOf(t *testing.T, raw string) string {
	t.Helper()
	if len(raw) < 3 {
		t.Fatalf("bad start result %q", raw)
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] == ':' {
			return raw[i+1:]
		}
	}
	t.Fatalf("bad start result %q", raw)
	return ""
}

// Stable identity: a restart with persisted PEMs must keep the fingerprint.
func TestGoStartWithCertKeepsFingerprint(t *testing.T) {
	raw := startWithRandomPort(t, goStart)
	want := fingerprintOf(t, raw)
	certPEM, keyPEM := goCertPEM(), goKeyPEM()
	if certPEM == "" || keyPEM == "" {
		t.Fatal("minted PEMs must be readable after start")
	}
	if got := goStop(); got != 0 {
		t.Fatalf("goStop = %d, want 0", got)
	}
	raw2, ok := goStartWithCert("pair-token", 0, certPEM, keyPEM)
	if !ok {
		t.Fatal("restart with persisted cert must succeed")
	}
	t.Cleanup(func() { _ = goStop() })
	if got := fingerprintOf(t, raw2); got != want {
		t.Fatalf("restart fingerprint = %q, want stable %q", got, want)
	}
}

func TestGoStartWithCertRejectsBadPEM(t *testing.T) {
	if _, ok := goStartWithCert("pair-token", 0, "bad", "bad"); ok {
		t.Fatal("bad PEM must fail closed")
		_ = goStop()
	}
	if _, ok := goStartWithCert("", 0, "a", "b"); ok {
		t.Fatal("empty token must fail closed")
		_ = goStop()
	}
}
