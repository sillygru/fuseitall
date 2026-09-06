// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package core

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

// testServer starts the server's handler behind real TLS using the server's
// own self-signed cert, so the round trip exercises the TOFU pin.
func testServer(t *testing.T, srv *Server) string {
	t.Helper()
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{srv.TLSCertificate()},
		MinVersion:   tls.VersionTLS12,
	})
	if err != nil {
		t.Fatalf("tls listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() { _ = http.Serve(listener, srv.Handler()) }()
	return "https://" + listener.Addr().String()
}

func testPair(t *testing.T, platform string) (*Server, *http.Client) {
	t.Helper()
	token, err := RotatePairToken()
	if err != nil {
		t.Fatalf("RotatePairToken: %v", err)
	}
	srv, err := NewServer(token, platform, []string{CapabilityPing}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	client, err := NewTOFUClient(srv.CertFingerprint())
	if err != nil {
		t.Fatalf("NewTOFUClient: %v", err)
	}
	return srv, client
}

func TestPingRoundTripTLS(t *testing.T) {
	srv, client := testPair(t, "mac")
	baseURL := testServer(t, srv)
	sender := SenderInfo{Platform: "android", AppBuild: CurrentBuild, MinPeerBuild: CurrentMinPeerBuild}
	pong, err := SendPing(context.Background(), client, baseURL, srv.token, sender, []string{CapabilityPing}, time.Now())
	if err != nil {
		t.Fatalf("SendPing = %v, want pong", err)
	}
	if pong.Nonce == "" || pong.ReceivedAt <= 0 {
		t.Fatalf("pong = %+v, want echoed nonce and received_at", pong)
	}
}

func TestPingWrongToken(t *testing.T) {
	srv, client := testPair(t, "mac")
	baseURL := testServer(t, srv)
	sender := SenderInfo{Platform: "android", AppBuild: CurrentBuild, MinPeerBuild: CurrentMinPeerBuild}
	if _, err := SendPing(context.Background(), client, baseURL, "deadbeef-dead-beef-dead-beefdeadbeef", sender, []string{CapabilityPing}, time.Now()); err == nil {
		t.Fatal("SendPing(wrong token) = nil, want unauthorized error")
	}
}

func TestPingTOFUMismatch(t *testing.T) {
	srv, _ := testPair(t, "mac")
	baseURL := testServer(t, srv)
	bad, err := NewTOFUClient(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatalf("NewTOFUClient: %v", err)
	}
	sender := SenderInfo{Platform: "android", AppBuild: CurrentBuild, MinPeerBuild: CurrentMinPeerBuild}
	if _, err := SendPing(context.Background(), bad, baseURL, srv.token, sender, []string{CapabilityPing}, time.Now()); err == nil {
		t.Fatal("SendPing(wrong pin) = nil, want TLS failure")
	}
	if _, err := NewTOFUClient("not-hex"); err == nil {
		t.Fatal("NewTOFUClient(garbage) = nil, want fail-closed error")
	}
}

func TestPingPeerOutdatedGetsUpdateRequired(t *testing.T) {
	srv, client := testPair(t, "mac")
	baseURL := testServer(t, srv)
	sender := SenderInfo{Platform: "android", AppBuild: 0, MinPeerBuild: 0}
	_, err := SendPing(context.Background(), client, baseURL, srv.token, sender, []string{CapabilityPing}, time.Now())
	if !errors.Is(err, ErrPeerOutdated) {
		t.Fatalf("SendPing(peer-older) = %v, want ErrPeerOutdated", err)
	}
	var updateErr *UpdateRequiredError
	if !errors.As(err, &updateErr) {
		t.Fatalf("SendPing(peer-older) = %T, want *UpdateRequiredError", err)
	}
	if !strings.HasPrefix(updateErr.Message, "Update FuseItAll on ") {
		t.Fatalf("message = %q, want canonical prefix", updateErr.Message)
	}
}

func TestPingMissingCapabilityGetsUpdateRequired(t *testing.T) {
	srv, client := testPair(t, "mac")
	baseURL := testServer(t, srv)
	sender := SenderInfo{Platform: "android", AppBuild: CurrentBuild, MinPeerBuild: CurrentMinPeerBuild}
	_, err := SendPing(context.Background(), client, baseURL, srv.token, sender, nil, time.Now())
	if !errors.Is(err, ErrPeerOutdated) {
		t.Fatalf("SendPing(no caps) = %v, want ErrPeerOutdated via UPDATE_REQUIRED", err)
	}
}

func TestHealth(t *testing.T) {
	srv, client := testPair(t, "mac")
	baseURL := testServer(t, srv)
	resp, err := client.Get(baseURL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /health = %d, want 200", resp.StatusCode)
	}
}

func TestServerWithCertRoundTrip(t *testing.T) {
	srv, err := NewServer("tok", "mac", []string{CapabilityPing}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	certPEM, keyPEM, err := EncodeTLSCertPEM(srv.TLSCertificate())
	if err != nil {
		t.Fatal(err)
	}
	cert, fp, err := ParseTLSCertPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	if fp != srv.CertFingerprint() {
		t.Fatalf("fp = %q, want %q", fp, srv.CertFingerprint())
	}
	srv2, err := NewServerWithCert("tok", "mac", []string{CapabilityPing}, nil, cert, fp)
	if err != nil {
		t.Fatalf("reuse persisted cert: %v", err)
	}
	if srv2.CertFingerprint() != fp {
		t.Fatal("reused server must keep the stable fingerprint")
	}
	if _, err := NewServerWithCert("tok", "mac", nil, nil, cert, strings.Repeat("00", 32)); err == nil {
		t.Fatal("mismatched fingerprint must fail closed")
	}
	if _, _, err := ParseTLSCertPEM([]byte("nope"), []byte("nope")); err == nil {
		t.Fatal("garbage PEM must fail closed")
	}
}

func TestNewServerRejectsEmptySecrets(t *testing.T) {
	if _, err := NewServer("", "mac", nil, nil); err == nil {
		t.Fatal("NewServer(empty token) = nil, want error")
	}
	if _, err := NewServer("token", "", nil, nil); err == nil {
		t.Fatal("NewServer(empty platform) = nil, want error")
	}
}
