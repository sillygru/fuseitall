// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"fuseitall/core"
)

func TestParsePeerPing(t *testing.T) {
	pingWith := func(payload string) []byte {
		return []byte(`{"protocol_v":1,"type":"ping","sender":{"platform":"android","app_build":1,"min_peer_build":1},"capabilities":["ping"],"payload":` + payload + `}`)
	}
	cases := []struct {
		name string
		body []byte
		port int
		ok   bool
	}{
		{"valid reply_port", pingWith(`{"nonce":"abc","sent_at":1,"reply_port":18790}`), 18790, true},
		{"missing reply_port", pingWith(`{"nonce":"abc"}`), 0, false},
		{"zero port", pingWith(`{"nonce":"abc","reply_port":0}`), 0, false},
		{"port too large", pingWith(`{"nonce":"abc","reply_port":65536}`), 0, false},
		{"negative port", pingWith(`{"nonce":"abc","reply_port":-1}`), 0, false},
		{"wrong type", []byte(`{"protocol_v":1,"type":"pong","payload":{"reply_port":18790}}`), 0, false},
		{"malformed", []byte(`{not json`), 0, false},
		{"empty", nil, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			port, ok := ParsePeerPing(tc.body)
			if ok != tc.ok || port != tc.port {
				t.Fatalf("ParsePeerPing = (%d,%v), want (%d,%v)", port, ok, tc.port, tc.ok)
			}
		})
	}
}

func TestParseUpdateReply(t *testing.T) {
	updateBody := func(device string, required int) []byte {
		env, err := core.NewEnvelope(core.TypeError,
			core.SenderInfo{Platform: "android", AppBuild: 1, MinPeerBuild: 1},
			[]string{core.CapabilityPing}, core.NewUpdateRequiredPayload(device, required))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(env)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	msg, self, ok := ParseUpdateReply(updateBody("android", core.CurrentBuild))
	if !ok || self {
		t.Fatalf("peer-outdated reply = (%q,%v,%v), want self=false ok=true", msg, self, ok)
	}
	if want := "Update FuseItAll on android to " + core.CurrentAppVersion; !strings.Contains(msg, want) {
		t.Fatalf("message = %q, want substring %q", msg, want)
	}
	if !strings.Contains(msg, "current "+core.CurrentAppVersion) {
		t.Fatalf("message = %q, want current version", msg)
	}
	_, self, ok = ParseUpdateReply(updateBody("macos", core.CurrentBuild+1))
	if !ok || !self {
		t.Fatalf("self-outdated reply: self=%v ok=%v, want self=true ok=true", self, ok)
	}
	for name, body := range map[string][]byte{
		"pong type":       []byte(`{"protocol_v":1,"type":"pong","payload":{}}`),
		"malformed":       []byte(`{nope`),
		"non-update code": []byte(`{"protocol_v":1,"type":"error","payload":{"code":"UNAUTHORIZED","message":"nope"}}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, ok := ParseUpdateReply(body); ok {
				t.Fatal("ParseUpdateReply = ok, want not-ok")
			}
		})
	}
}

func TestUpdateRequiredLine(t *testing.T) {
	const msg = "Update FuseItAll on android to build >= 2"
	if got := UpdateRequiredLine(msg, false); got != UpdateRequiredPrefix+msg {
		t.Fatalf("peer line = %q", got)
	}
	if got := UpdateRequiredLine(msg, true); !strings.HasPrefix(got, UpdateRequiredSelfPrefix) || !strings.HasSuffix(got, msg) {
		t.Fatalf("self line = %q", got)
	}
}

func TestSplitRemoteHost(t *testing.T) {
	if got := SplitRemoteHost("192.168.1.5:52311"); got != "192.168.1.5" {
		t.Fatalf("ipv4 = %q", got)
	}
	if got := SplitRemoteHost("[fe80::1]:52311"); got != "fe80::1" {
		t.Fatalf("ipv6 = %q", got)
	}
	if got := SplitRemoteHost("bare"); got != "bare" {
		t.Fatalf("fallback = %q", got)
	}
}

func TestPeerBaseURL(t *testing.T) {
	if got := PeerBaseURL("192.168.1.5", 18790); got != "https://192.168.1.5:18790" {
		t.Fatalf("ipv4 url = %q", got)
	}
	if got := PeerBaseURL("fe80::1", 18790); got != "https://[fe80::1]:18790" {
		t.Fatalf("ipv6 url = %q", got)
	}
}

func TestWrapHandlerCapturesPeerOnAccept(t *testing.T) {
	svc := NewService("{}", "fp", "tok", NewLogBuffer(10))
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"type":"pong"}`))
	})
	body := `{"protocol_v":1,"type":"ping","sender":{"platform":"android","app_build":1,"min_peer_build":1},"capabilities":["ping"],"payload":{"nonce":"n","reply_port":18790}}`
	req := httptest.NewRequest(http.MethodPost, "/ping", strings.NewReader(body))
	req.RemoteAddr = "192.168.1.5:52311"
	rec := httptest.NewRecorder()
	WrapHandler(svc, next).ServeHTTP(rec, req)
	if got := svc.GetPeerAddr(); got != "192.168.1.5:18790" {
		t.Fatalf("GetPeerAddr = %q, want %q", got, "192.168.1.5:18790")
	}
	if rec.Code != http.StatusOK || rec.Body.String() != `{"type":"pong"}` {
		t.Fatalf("response altered: code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestWrapHandlerSurfacesInboundUpdateRequired(t *testing.T) {
	svc := NewService("{}", "fp", "tok", NewLogBuffer(10))
	env, err := core.NewEnvelope(core.TypeError,
		core.SenderInfo{Platform: "macos", AppBuild: 1, MinPeerBuild: 1},
		[]string{core.CapabilityPing}, core.NewUpdateRequiredPayload("android", core.CurrentBuild))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUpgradeRequired)
		_, _ = w.Write(raw)
	})
	req := httptest.NewRequest(http.MethodPost, "/ping", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	WrapHandler(svc, next).ServeHTTP(rec, req)
	if got := svc.GetPeerAddr(); got != "" {
		t.Fatalf("rejected ping must not capture peer, got %q", got)
	}
	lines := svc.GetLog()
	if len(lines) != 1 || !strings.HasPrefix(lines[0], UpdateRequiredPrefix) {
		t.Fatalf("log = %q, want single UPDATE_REQUIRED line", lines)
	}
	if !strings.Contains(lines[0], "Update FuseItAll on android to ") {
		t.Fatalf("message not verbatim: %q", lines[0])
	}
	notice := svc.GetUpdateNotice()
	if !notice.Active || notice.Self {
		t.Fatalf("typed notice = %+v, want active peer-outdated", notice)
	}
	if !strings.Contains(notice.Message, "Update FuseItAll on android to ") {
		t.Fatalf("typed message not verbatim: %q", notice.Message)
	}
	if notice.RequiredVersion != core.CurrentAppVersion || notice.CurrentVersion != core.CurrentAppVersion {
		t.Fatalf("typed versions = %+v, want %q", notice, core.CurrentAppVersion)
	}
}

func TestParsePeerPingFullExtractsFingerprint(t *testing.T) {
	body := []byte(`{"protocol_v":1,"type":"ping","sender":{"platform":"android","app_build":1,"min_peer_build":1},"capabilities":["ping"],"payload":{"nonce":"n","reply_port":18790,"reply_fingerprint":"AB:CD 12"}}`)
	port, fp, ok := ParsePeerPingFull(body)
	if !ok || port != 18790 || fp != "abcd12" {
		t.Fatalf("full = (%d,%q,%v), want (18790,abcd12,true)", port, fp, ok)
	}
	// Old phone without fingerprint: port still parses, fp empty.
	old := []byte(`{"protocol_v":1,"type":"ping","sender":{"platform":"android","app_build":1,"min_peer_build":1},"capabilities":["ping"],"payload":{"nonce":"n","reply_port":18790}}`)
	if p, fp, ok := ParsePeerPingFull(old); !ok || p != 18790 || fp != "" {
		t.Fatalf("old phone = (%d,%q,%v), want (18790,,true)", p, fp, ok)
	}
}

func TestSetPeerRePinsFingerprint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	svc.setPeer("192.168.1.5", 18790, "aaa")
	svc.mu.Lock()
	old := svc.peerFingerprint
	svc.mu.Unlock()
	if old != "aaa" {
		t.Fatalf("pin = %q, want aaa", old)
	}
	// Rotated phone cert arrives over the authenticated channel: re-pin.
	svc.setPeer("192.168.1.5", 18791, "bbb")
	svc.mu.Lock()
	cur := svc.peerFingerprint
	svc.mu.Unlock()
	if cur != "bbb" {
		t.Fatalf("re-pin = %q, want bbb", cur)
	}
	found := false
	for _, line := range svc.GetLog() {
		if strings.HasPrefix(line, "phone cert updated fingerprint=") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("log = %q, want cert-updated line", svc.GetLog())
	}
}

func TestPeerTTLExpiry(t *testing.T) {
	svc := NewService("{}", "fp", "tok", NewLogBuffer(10))
	if svc.IsPaired() || svc.GetPeerAddr() != "" {
		t.Fatal("fresh service must be unpaired with empty peer addr")
	}
	svc.setPeer("192.168.1.5", 18790)
	if !svc.IsPaired() || svc.GetPeerAddr() != "192.168.1.5:18790" {
		t.Fatal("freshly captured peer must be paired with addr")
	}
	svc.mu.Lock()
	svc.lastSeen = time.Now().Add(-(peerTTL - 5*time.Second))
	svc.mu.Unlock()
	if !svc.IsPaired() || svc.GetPeerAddr() == "" {
		t.Fatal("peer within TTL must stay paired")
	}
	// Boundary: exactly peerTTL old is expired (Since >= peerTTL).
	svc.mu.Lock()
	svc.lastSeen = time.Now().Add(-peerTTL)
	svc.mu.Unlock()
	if svc.IsPaired() || svc.GetPeerAddr() != "" {
		t.Fatal("peer at exactly TTL must be expired")
	}
	svc.mu.Lock()
	svc.lastSeen = time.Now().Add(-(peerTTL + 5*time.Second))
	svc.mu.Unlock()
	if svc.IsPaired() || svc.GetPeerAddr() != "" {
		t.Fatal("peer past TTL must be unpaired with empty addr")
	}
}

func TestIsPeerLost(t *testing.T) {
	upd := &core.UpdateRequiredError{Message: "Update FuseItAll on android to build >= 2", RequiredBuild: 2}
	if isPeerLost(upd) {
		t.Fatal("update-required must not count as peer lost")
	}
	authErr := errors.New(`peer error UNAUTHORIZED: invalid pair token`)
	if isPeerLost(authErr) {
		t.Fatal("auth rejection must not count as peer lost")
	}
	mismatch := errors.New(`post ping: Post "https://1.2.3.4:1/ping": certificate fingerprint mismatch`)
	if isPeerLost(mismatch) {
		t.Fatal("cert mismatch must not count as peer lost (keep peer, wait to re-pin)")
	}
	if !isCertMismatch(mismatch) {
		t.Fatal("cert mismatch must be detected")
	}
	if !isPeerLost(errors.New("post ping: dial tcp: connection refused")) {
		t.Fatal("dial failure must count as peer lost")
	}
}

func TestSendPingDialFailureClearsPeer(t *testing.T) {
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	// Port 1 on loopback refuses: a dial/network failure, not update/auth.
	svc.setPeer("127.0.0.1", 1)
	if _, err := svc.SendPingToPhone(); err == nil {
		t.Fatal("ping to refused port must fail")
	}
	if svc.IsPaired() || svc.GetPeerAddr() != "" {
		t.Fatal("dial failure must clear the peer")
	}
	found := false
	for _, line := range svc.GetLog() {
		if strings.HasPrefix(line, "phone peer lost (") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("log = %q, want a 'phone peer lost (...)' line", svc.GetLog())
	}
	// Typed update state untouched by a dial failure.
	if notice := svc.GetUpdateNotice(); notice.Active {
		t.Fatalf("dial failure must not set update notice: %+v", notice)
	}
}

func TestLastDeviceSurvivesClearAndRestart(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	if got := svc.GetLastDevice(); got.HasDevice {
		t.Fatalf("fresh service must have no last device: %+v", got)
	}
	svc.setPeer("192.168.1.5", 18790)
	if got := svc.GetLastDevice(); !got.HasDevice || got.Addr != "192.168.1.5:18790" {
		t.Fatalf("after capture GetLastDevice = %+v, want addr 192.168.1.5:18790", got)
	}
	svc.clearPeer()
	if svc.IsPaired() {
		t.Fatal("clearPeer must drop the ephemeral peer")
	}
	remembered := svc.GetLastDevice()
	if !remembered.HasDevice || remembered.Addr != "192.168.1.5:18790" {
		t.Fatalf("remembered device must survive clearPeer: %+v", remembered)
	}
	// Restart: NewService reloads the persisted device while staying unpaired.
	svc2 := NewService("{}", "fp", "tok", NewLogBuffer(20))
	got := svc2.GetLastDevice()
	if !got.HasDevice || got.Addr != "192.168.1.5:18790" {
		t.Fatalf("restart GetLastDevice = %+v, want remembered addr", got)
	}
	if svc2.IsPaired() {
		t.Fatal("restart must stay unpaired until a fresh ping or reconnect")
	}
}

func TestReconnectNoDeviceFailsClosed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(10))
	if _, err := svc.ReconnectToLastDevice(); err == nil {
		t.Fatal("reconnect with no device must fail closed")
	}
}

func TestForgetLastDeviceClearsMemoryAndDisk(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc, _, _ := testPairingService(t, NewLogBuffer(20))
	svc.setPeer("192.168.1.5", 18790, "aaa")
	if _, err := svc.ForgetLastDevice(); err != nil {
		t.Fatalf("forget must succeed with a remembered phone: %v", err)
	}
	if svc.IsPaired() {
		t.Fatal("forget must drop the ephemeral peer")
	}
	if got := svc.GetLastDevice(); got.HasDevice {
		t.Fatalf("forget must clear the remembered phone: %+v", got)
	}
	svc.mu.Lock()
	pin := svc.peerFingerprint
	svc.mu.Unlock()
	if pin != "" {
		t.Fatalf("forget must clear the phone pin, got %q", pin)
	}
	// Restart: the forgotten phone must not come back from disk.
	svc2 := NewService("{}", "fp", "tok", NewLogBuffer(20))
	if got := svc2.GetLastDevice(); got.HasDevice {
		t.Fatalf("forgotten phone must stay gone after restart: %+v", got)
	}
}

// testPairingService builds a service wired like main.go: real pair file,
// live core server, QR inputs, bound server. Returns the service, the
// server, and the pre-rotation token.
func testPairingService(t *testing.T, logs *LogBuffer) (*Service, *core.Server, string) {
	t.Helper()
	id, token, cert, fp, err := LoadOrCreatePairState(nil)
	if err != nil {
		t.Fatalf("pair state: %v", err)
	}
	srv, err := core.NewServerWithCert(token, "macos",
		[]string{core.CapabilityPing, core.CapabilityNotifications, core.CapabilityClipboard, core.CapabilitySettingsSync},
		nil, cert, fp)
	if err != nil {
		t.Fatalf("core server: %v", err)
	}
	pair := core.MakePairPayload("Test Mac", "macos", "192.168.1.2", 18789, fp, id.PublicKey, token)
	raw, err := core.EncodePairQR(pair)
	if err != nil {
		t.Fatalf("pair qr: %v", err)
	}
	svc := NewService(string(raw), fp, token, logs)
	svc.ConfigurePairing("Test Mac", "macos", "192.168.1.2", 18789, fp, pair.PubKey)
	svc.bindServer(srv)
	return svc, srv, token
}

func TestForgetRotatesToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc, srv, oldToken := testPairingService(t, NewLogBuffer(20))
	svc.setPeer("192.168.1.5", 18790, "aaa")
	before := svc.GetPairJSON()
	if _, err := svc.ForgetLastDevice(); err != nil {
		t.Fatalf("forget must rotate: %v", err)
	}
	if svc.GetPairJSON() == before {
		t.Fatal("forget must rebuild the QR with a fresh token")
	}
	// Old token must now 403 through the live server (the forgotten
	// phone's next ping unpairs itself instead of re-capturing).
	pingBody := []byte(`{"protocol_v":1,"type":"ping","sender":{"platform":"android","app_build":2,"min_peer_build":1},"capabilities":["ping"],"payload":{"nonce":"n","sent_at":1}}`)
	req := httptest.NewRequest(http.MethodPost, "/ping", strings.NewReader(string(pingBody)))
	req.Header.Set("Authorization", "Bearer "+oldToken)
	req.RemoteAddr = "192.168.1.5:50000"
	rec := httptest.NewRecorder()
	WrapHandler(svc, srv.Handler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("old token status = %d, want 403", rec.Code)
	}
}

func TestIngestUnpairDropsPeerWithoutRotation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc, _, _ := testPairingService(t, NewLogBuffer(20))
	svc.setPeer("192.168.1.5", 18790, "aaa")
	before := svc.GetPairJSON()
	svc.ingestUnpairBody()
	if svc.IsPaired() {
		t.Fatal("goodbye must drop the ephemeral peer")
	}
	if got := svc.GetLastDevice(); got.HasDevice {
		t.Fatalf("goodbye must clear the remembered phone: %+v", got)
	}
	if svc.GetPairJSON() != before {
		t.Fatal("goodbye must not rotate the token (phone already wiped)")
	}
}

func TestRotationLogCollapse(t *testing.T) {
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	svc.logRotationOnce("peer-lost", "phone peer lost (refused)")
	svc.logRotationOnce("peer-lost", "phone peer lost (refused)")
	if got := len(svc.GetLog()); got != 1 {
		t.Fatalf("repeat within window must collapse, log len = %d", got)
	}
	svc.logRotationOnce("cert-mismatch", "phone cert mismatch (x)")
	if got := len(svc.GetLog()); got != 2 {
		t.Fatalf("new kind must be loud, log len = %d", got)
	}
	// Fresh inbound ping resets the window: same kind logs again.
	svc.setPeer("192.168.1.5", 18790)
	svc.logRotationOnce("peer-lost", "phone peer lost (refused)")
	if got := len(svc.GetLog()); got != 4 {
		t.Fatalf("after re-capture the failure must be loud again, log len = %d", got)
	}
}

func TestForgetLastDeviceFailsClosed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(10))
	if _, err := svc.ForgetLastDevice(); err == nil {
		t.Fatal("forget with no device must fail closed")
	}
}

func TestHeartbeatTickKeepsLastDeviceOnDialFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	// Refused loopback port: reconnect dials, fails, and must keep the card.
	svc.setPeer("127.0.0.1", 1)
	svc.mu.Lock()
	svc.peerHost, svc.peerPort = "", 0
	svc.lastSeen = time.Time{}
	svc.mu.Unlock()
	svc.HeartbeatTick()
	if got := svc.GetLastDevice(); !got.HasDevice || got.Addr != "127.0.0.1:1" {
		t.Fatalf("heartbeat dial failure must keep last device: %+v", got)
	}
}

func TestSendPingExpiredPeerFailsClosed(t *testing.T) {
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	svc.setPeer("127.0.0.1", 1)
	svc.mu.Lock()
	svc.lastSeen = time.Now().Add(-(peerTTL + time.Second))
	svc.mu.Unlock()
	if _, err := svc.SendPingToPhone(); err == nil {
		t.Fatal("ping with expired peer must fail closed without dialing")
	} else if !strings.Contains(err.Error(), "no phone peer") {
		t.Fatalf("err = %q, want no-phone-peer failure", err)
	}
}

func TestParsePeerDevice(t *testing.T) {
	pingWith := func(payload string) []byte {
		return []byte(`{"protocol_v":1,"type":"ping","sender":{"platform":"android","app_build":1,"min_peer_build":1},"capabilities":["ping"],"payload":` + payload + `}`)
	}
	full := ParsePeerDevice(pingWith(`{"nonce":"n","reply_port":18790,"device_name":"OnePlus 15R","model":"CPH2767","battery_pct":78,"charging":true}`))
	if !full.HasName || full.DeviceName != "OnePlus 15R" {
		t.Fatalf("name = (%q,%v), want (OnePlus 15R,true)", full.DeviceName, full.HasName)
	}
	if !full.HasModel || full.Model != "CPH2767" {
		t.Fatalf("model = (%q,%v), want (CPH2767,true)", full.Model, full.HasModel)
	}
	if !full.HasBattery || full.BatteryPct != 78 || !full.HasCharging || !full.Charging {
		t.Fatalf("battery = %+v, want 78/charging", full)
	}
	// Old phone: no facts, all absent, never an error.
	old := ParsePeerDevice(pingWith(`{"nonce":"n","reply_port":18790}`))
	if old.HasName || old.HasModel || old.HasBattery || old.HasCharging {
		t.Fatalf("old phone facts = %+v, want all absent", old)
	}
	// Out-of-range battery drops battery AND charging together.
	bad := ParsePeerDevice(pingWith(`{"nonce":"n","battery_pct":150,"charging":true}`))
	if bad.HasBattery || bad.HasCharging {
		t.Fatalf("bad battery facts = %+v, want absent", bad)
	}
	// Over-long names are dropped, not stored.
	long := ParsePeerDevice(pingWith(`{"nonce":"n","device_name":"` + strings.Repeat("x", 65) + `"}`))
	if long.HasName {
		t.Fatalf("long name must be dropped: %+v", long)
	}
	for name, body := range map[string][]byte{
		"wrong type": []byte(`{"protocol_v":1,"type":"pong","payload":{"device_name":"X"}}`),
		"malformed":  []byte(`{nope`),
		"empty":      nil,
	} {
		t.Run(name, func(t *testing.T) {
			if got := ParsePeerDevice(body); got.HasName || got.HasModel || got.HasBattery {
				t.Fatalf("facts = %+v, want all absent", got)
			}
		})
	}
}

func TestPeerFactsFlowToDisplay(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	facts := ParsePeerDevice([]byte(`{"protocol_v":1,"type":"ping","payload":{"nonce":"n","device_name":"OnePlus 15R","model":"CPH2767","battery_pct":42,"charging":false}}`))
	svc.setPeerWithFacts("192.168.1.5", 18790, "aaa", facts)
	peer := svc.GetPeerDevice()
	if !peer.HasDevice || peer.DisplayName != "OnePlus 15R" || peer.Model != "CPH2767" {
		t.Fatalf("peer = %+v, want advertised name/model", peer)
	}
	if peer.BatteryPct == nil || *peer.BatteryPct != 42 || peer.Charging == nil || *peer.Charging {
		t.Fatalf("peer battery = %+v, want 42/not-charging", peer)
	}
	// Rename alias wins; later adverts (incl. a phone-side rename) keep the
	// alias while updating the stored advertised name underneath.
	if _, err := svc.SetCustomName("Travel Phone"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	renamed := ParsePeerDevice([]byte(`{"protocol_v":1,"type":"ping","payload":{"nonce":"n","device_name":"New Name","model":"CPH2767","battery_pct":43}}`))
	svc.setPeerWithFacts("192.168.1.5", 18790, "aaa", renamed)
	got := svc.GetLastDevice()
	if got.DisplayName != "Travel Phone" || got.DeviceName != "New Name" || got.CustomName != "Travel Phone" {
		t.Fatalf("after phone rename: %+v, want alias kept + advert updated", got)
	}
	// Clearing falls back to the current advertised name; restart preserves it.
	if _, err := svc.SetCustomName("  "); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := svc.GetLastDevice(); got.DisplayName != "New Name" {
		t.Fatalf("cleared display = %q, want New Name", got.DisplayName)
	}
	svc2 := NewService("{}", "fp", "tok", NewLogBuffer(20))
	if got := svc2.GetLastDevice(); !got.HasDevice || got.Model != "CPH2767" || got.BatteryPct == nil || *got.BatteryPct != 43 {
		t.Fatalf("restart device = %+v, want persisted facts", got)
	}
	// Facts never reach the log (device names are PII).
	for _, line := range svc.GetLog() {
		if strings.Contains(line, "OnePlus") || strings.Contains(line, "Travel Phone") {
			t.Fatalf("log leaks device name: %q", line)
		}
	}
}

func TestSetCustomNameFailsClosed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(10))
	if _, err := svc.SetCustomName("x"); err == nil {
		t.Fatal("rename with no device must fail closed")
	}
	svc.setPeer("127.0.0.1", 1)
	if _, err := svc.SetCustomName(strings.Repeat("x", 65)); err == nil {
		t.Fatal("over-long rename must fail closed")
	}
	if got := svc.GetLastDevice(); got.CustomName != "" {
		t.Fatalf("failed rename must not store alias: %+v", got)
	}
}

func TestDisplayPhoneName(t *testing.T) {
	if got := displayPhoneName("Alias", "Advertised"); got != "Alias" {
		t.Fatalf("= %q, want alias", got)
	}
	if got := displayPhoneName("", "Advertised"); got != "Advertised" {
		t.Fatalf("= %q, want advertised", got)
	}
	if got := displayPhoneName("", ""); got != "" {
		t.Fatalf("= %q, want empty (UI falls back to Phone)", got)
	}
}
