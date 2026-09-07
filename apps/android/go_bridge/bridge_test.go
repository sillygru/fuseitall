// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"fuseitall/core"
)

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

// Accepted Mac-initiated feature posts are queued whole for Dart;
// rejected ones (wrong type, bad token) never reach the queue.
func TestFeatureEventsQueueAcceptedOnly(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	srv, err := core.NewServer("pair-token", "android", phoneCaps(), logger)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	featEvents = make(chan string, 64)
	ts := httptest.NewServer(sniffAcceptedPings(srv.Handler()))
	defer ts.Close()
	sender := core.SenderInfo{Platform: "macos", AppBuild: core.CurrentBuild, MinPeerBuild: core.CurrentMinPeerBuild}
	caps := phoneCaps()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := ts.Client()
	if _, err := core.SendFeature(ctx, client, ts.URL, "pair-token", sender, caps,
		core.TypeClipPush, &core.ClipPushPayload{Text: "hi", ChangedAt: 9, Origin: "mac"}); err != nil {
		t.Fatalf("clip-push: %v", err)
	}
	raw, ok := goPollEvent()
	if !ok {
		t.Fatal("accepted clip-push must queue an event")
	}
	var env struct {
		Type    string `json:"type"`
		Payload struct {
			Text string `json:"text"`
		} `json:"payload"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("event decode: %v", err)
	}
	if env.Type != core.TypeClipPush || env.Payload.Text != "hi" {
		t.Fatalf("event = %q, want clip-push hi", raw)
	}
	// Wrong type on the route is rejected and never queued.
	env2, err := core.NewEnvelope(core.TypePing, sender, caps, core.PingPayload{Nonce: "n"})
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	body, _ := json.Marshal(env2)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/clip", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer pair-token")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("wrong-type post: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if _, ok := goPollEvent(); ok {
		t.Fatal("rejected post must not queue an event")
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

func TestGoLastErrorSurfacesDetail(t *testing.T) {
	_ = goStop()
	if _, ok := goStart("", 0); ok {
		t.Fatal("empty token must fail")
	}
	if got := goLastError(); got == "" || !strings.Contains(got, "pair token") {
		t.Fatalf("goLastError = %q, want pair token detail", got)
	}
	_ = goStop()
	raw, ok := goStart("tok", 0)
	if !ok {
		t.Fatalf("goStart failed: lastError=%q", goLastError())
	}
	t.Cleanup(func() { _ = goStop() })
	if got := goLastError(); got != "" {
		t.Fatalf("goLastError after success = %q, want empty", got)
	}
	if _, ok := goStart("tok2", 0); ok {
		t.Fatal("double start must fail")
	}
	if got := goLastError(); !strings.Contains(got, "already started") {
		t.Fatalf("goLastError double start = %q, want already started", got)
	}
	_ = raw
}
