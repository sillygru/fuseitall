// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Package backend is the thin Wails adapter for the Mac shell: it exposes
// already-computed core state (pair JSON, cert fingerprint, server log) to
// the frontend and translates frontend intents (ping the phone) into core
// calls. It contains no pairing, gating, or transport logic — that lives in
// fuseitall/core. Wiring (identity, token, server) lives in main.go.
//
// Log-line protocol (frontend contract, parsed by App.svelte):
//   - "UPDATE_REQUIRED <msg>": a version gate produced a 426 — either our
//     server rejected the phone's ping, or our ping to the phone was
//     rejected. <msg> is the canonical core message verbatim
//     ("Update FuseItAll on <device> to build >= N"); the peer must update.
//   - "UPDATE_REQUIRED_SELF <msg>": same shape, but the required build is
//     above our own build, so this Mac is the outdated side.
//   - "phone peer captured host=<ip> port=<n>": informational, logged when an
//     accepted phone ping teaches us the return path.
//   - "phone cert pinned fingerprint=<hex>": informational, logged once when
//     the first outbound ping TOFU-accepts the phone's TLS cert.
//   - "ping to phone ok rtt_ms=<n>" / "ping to phone failed: <err>":
//     outbound ping outcomes; the RTT string also returns to the caller.
//   - "phone peer lost (<err>)": a dial/network failure cleared the captured
//     return path (not update-required, not auth); IsPaired flips false.
package backend

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"fuseitall/core"
)

const (
	// UpdateRequiredPrefix marks a log line carrying a canonical update
	// message where the peer must update. The frontend banners everything
	// after the prefix verbatim.
	UpdateRequiredPrefix = "UPDATE_REQUIRED "
	// UpdateRequiredSelfPrefix marks the same shape when this Mac must
	// update (required build above our build). Checked before
	// UpdateRequiredPrefix: the two prefixes never collide on the wire
	// ("UPDATE_REQUIRED_SELF ..." has no space after "UPDATE_REQUIRED").
	UpdateRequiredSelfPrefix = "UPDATE_REQUIRED_SELF "

	// senderPlatform must match the platform main.go passes to
	// core.NewServer; it stamps our outbound pings.
	senderPlatform = "macos"

	// peerTTL is how long a captured phone return path stays valid without
	// a fresh accepted ping. Past it, IsPaired flips false and GetPeerAddr
	// returns "" so the UI falls back to the Unpaired screen.
	peerTTL = 60 * time.Second
)

// LogBuffer is a bounded in-memory line sink shared with the slog handler
// in main.go so the frontend can poll recent server events.
type LogBuffer struct {
	mu    sync.Mutex
	lines []string
	max   int
}

// NewLogBuffer returns a buffer keeping at most max lines. A non-positive
// max falls back to 200.
func NewLogBuffer(max int) *LogBuffer {
	if max <= 0 {
		max = 200
	}
	return &LogBuffer{max: max}
}

// Write implements io.Writer. Each call appends whole lines; partial-line
// fragments ride along with the next write, which is fine for log display.
func (b *LogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, line := range strings.Split(string(p), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		b.lines = append(b.lines, line)
	}
	if overflow := len(b.lines) - b.max; overflow > 0 {
		b.lines = append([]string{}, b.lines[overflow:]...)
	}
	return len(p), nil
}

// Snapshot returns a copy of the buffered lines, oldest first.
func (b *LogBuffer) Snapshot() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string{}, b.lines...)
}

// Service exposes pair state and phone-ping actions to the Wails frontend.
// Pair fields are set once at startup by main.go; peer coordinates are
// learned later from accepted phone pings (see WrapHandler) and remembered
// on disk (see persist.go) so a restart still shows the last phone.
type Service struct {
	pairJSON    string
	fingerprint string
	token       string
	logs        *LogBuffer

	mu              sync.Mutex
	peerHost        string
	peerPort        int
	peerFingerprint string
	// lastSeen is the last accepted phone ping; IsPaired/GetPeerAddr expire
	// peerHost/peerPort peerTTL after it.
	lastSeen time.Time
	// lastHost/lastPort/lastDeviceSeen survive TTL expiry and dial failures
	// (clearPeer keeps them) so GetLastDevice can render "Last connected"
	// and ReconnectToLastDevice can redial without a fresh QR scan.
	lastHost       string
	lastPort       int
	lastDeviceSeen time.Time
	// deviceName/deviceModel/battery* are the phone's latest advertised
	// facts, learned from accepted pings only (same trust point as the
	// return path). customName is the Mac-local rename alias: when non-empty
	// the UI shows it instead of deviceName. hasBattery distinguishes an
	// unknown battery from a real 0% reading.
	deviceName  string
	deviceModel string
	batteryPct  int
	hasBattery  bool
	charging    bool
	batteryAt   time.Time
	customName  string
	// lastUpdate tracks the newest version-gate outcome for the typed
	// GetUpdateNotice binding; the log keeps the human-readable history.
	lastUpdateMsg      string
	lastUpdateSelf     bool
	lastUpdateSet      bool
	lastUpdateReqVer   string
	lastUpdateCurVer   string
	lastUpdateReqBuild int
	// lastRotationKind/lastRotationLog collapse the expected transient spam
	// while the phone re-announces after a restart (stale port = refused,
	// stale pin = mismatch): repeats of the same kind within the window
	// stay quiet instead of logging every heartbeat/manual retry.
	lastRotationKind string
	lastRotationLog  time.Time
}

// heartbeatInterval mirrors the Android 20s heartbeat so both sides
// re-advertise presence across DHCP/port changes.
const heartbeatInterval = 20 * time.Second

// LastDeviceNotice is the typed last-phone state for the frontend: the
// offline "Last connected" card. Empty when no phone ever paired.
// DeviceName/Model/BatteryPct/Charging are the phone's latest advertised
// facts (nil battery fields = unknown, never 0% by default). CustomName is
// the Mac-local rename alias; DisplayName is what the UI shows
// (custom alias, else advertised name, else "" and the UI falls back to
// "Phone" so older frontends keep working).
type LastDeviceNotice struct {
	HasDevice    bool
	Host         string
	Port         int
	Addr         string
	LastSeenUnix int64
	DeviceName   string
	Model        string
	BatteryPct   *int
	Charging     *bool
	BatteryUnix  int64
	CustomName   string
	DisplayName  string
}

// NewService translates core outputs into the Wails-bound service. token is
// the pair token the phone already holds: it authenticates our ping back to
// the phone via Authorization: Bearer. The last device is loaded
// best-effort: a missing/corrupt file just means no offline card.
func NewService(pairJSON, fingerprint, token string, logs *LogBuffer) *Service {
	if logs == nil {
		logs = NewLogBuffer(0)
	}
	s := &Service{pairJSON: pairJSON, fingerprint: fingerprint, token: token, logs: logs}
	if dev, ok, err := LoadLastDevice(); err == nil && ok {
		s.lastHost, s.lastPort = dev.Host, dev.Port
		if dev.LastSeenUnix > 0 {
			s.lastDeviceSeen = time.Unix(dev.LastSeenUnix, 0)
		}
		if dev.Fingerprint != "" {
			s.peerFingerprint = dev.Fingerprint
		}
		s.deviceName, s.deviceModel, s.customName = dev.DeviceName, dev.Model, dev.CustomName
		if dev.BatteryPct != nil {
			s.batteryPct, s.hasBattery = *dev.BatteryPct, true
			s.charging = dev.Charging != nil && *dev.Charging
			if dev.BatteryUnix > 0 {
				s.batteryAt = time.Unix(dev.BatteryUnix, 0)
			}
		}
	}
	return s
}

// GetPairJSON returns the EncodePairQR JSON shown as the pairing QR.
func (s *Service) GetPairJSON() string { return s.pairJSON }

// GetFingerprint returns the hex SHA-256 of the server TLS cert (TOFU pin).
func (s *Service) GetFingerprint() string { return s.fingerprint }

// GetLog returns recent core server log lines, oldest first.
func (s *Service) GetLog() []string { return s.logs.Snapshot() }

// GetPeerAddr returns "host:port" learned from the phone's last accepted
// ping, or "" while unknown or expired (past peerTTL). The frontend keeps
// [Ping phone] disabled until this is non-empty.
func (s *Service) GetPeerAddr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.peerHost == "" || s.peerPort <= 0 {
		return ""
	}
	if time.Since(s.lastSeen) >= peerTTL {
		return ""
	}
	return net.JoinHostPort(s.peerHost, strconv.Itoa(s.peerPort))
}

// UpdateNotice is the typed version-gate state for the frontend: the single
// source of truth, so UI never scrapes log text. Message stays verbatim
// (canonical core text, never paraphrased). RequiredVersion/CurrentVersion
// are display-only and may be "" from older peers; RequiredBuild gates.
type UpdateNotice struct {
	Active          bool
	Self            bool
	Message         string
	RequiredVersion string
	CurrentVersion  string
	RequiredBuild   int
}

// GetAppVersion returns this Mac build's human version (0.1.0 launch).
// Typed binding for the footer/About row; mirrors core.CurrentAppVersion.
func (s *Service) GetAppVersion() string { return core.CurrentAppVersion }

// IsPaired reports whether an accepted phone ping taught us the return path
// recently (within peerTTL). Typed binding: the frontend derives `paired`
// from this, never from log text. It flips false peerTTL after the last
// accepted ping, or immediately after a dial failure clears the peer.
func (s *Service) IsPaired() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.peerHost == "" || s.peerPort <= 0 {
		return false
	}
	return time.Since(s.lastSeen) < peerTTL
}

// GetUpdateNotice returns the newest version-gate outcome, or inactive.
// Typed binding: the frontend banners this verbatim, never parses GetLog.
func (s *Service) GetUpdateNotice() UpdateNotice {
	s.mu.Lock()
	defer s.mu.Unlock()
	return UpdateNotice{
		Active:          s.lastUpdateSet,
		Self:            s.lastUpdateSelf,
		Message:         s.lastUpdateMsg,
		RequiredVersion: s.lastUpdateReqVer,
		CurrentVersion:  s.lastUpdateCurVer,
		RequiredBuild:   s.lastUpdateReqBuild,
	}
}

// setUpdate records a version-gate outcome (typed state) and mirrors it to
// the log with the parseable prefix (human history).
func (s *Service) setUpdate(msg string, self bool) {
	s.setUpdateDetail(msg, self, "", "", 0)
}

// setUpdateDetail records the full version-gate outcome including display
// versions. Builds gate; versions display.
func (s *Service) setUpdateDetail(msg string, self bool, reqVer, curVer string, reqBuild int) {
	s.mu.Lock()
	s.lastUpdateMsg, s.lastUpdateSelf, s.lastUpdateSet = msg, self, true
	s.lastUpdateReqVer, s.lastUpdateCurVer, s.lastUpdateReqBuild = reqVer, curVer, reqBuild
	s.mu.Unlock()
	s.appendLine(UpdateRequiredLine(msg, self))
}

// GetLastDevice returns the last phone this Mac paired with, even after the
// ephemeral peer expired or the app restarted. Typed binding for the offline
// "Last connected" card; HasDevice is false when no phone ever paired.
// Battery fields are nil while unknown; DisplayName is the rename alias when
// set, else the advertised name, else "" (the UI falls back to "Phone").
func (s *Service) GetLastDevice() LastDeviceNotice {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastHost == "" || s.lastPort <= 0 {
		return LastDeviceNotice{}
	}
	notice := LastDeviceNotice{
		HasDevice:    true,
		Host:         s.lastHost,
		Port:         s.lastPort,
		Addr:         net.JoinHostPort(s.lastHost, strconv.Itoa(s.lastPort)),
		LastSeenUnix: s.lastDeviceSeen.Unix(),
		DeviceName:   s.deviceName,
		Model:        s.deviceModel,
		CustomName:   s.customName,
		DisplayName:  displayPhoneName(s.customName, s.deviceName),
	}
	if s.hasBattery {
		pct, ch := s.batteryPct, s.charging
		notice.BatteryPct = &pct
		notice.Charging = &ch
		notice.BatteryUnix = s.batteryAt.Unix()
	}
	return notice
}

// GetPeerDevice returns the live advertised facts while the phone is paired
// (within peerTTL), or null-equivalent (HasDevice false) otherwise. Typed
// binding so the sidebar header shows model + battery without scraping logs.
func (s *Service) GetPeerDevice() LastDeviceNotice {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.peerHost == "" || s.peerPort <= 0 || time.Since(s.lastSeen) >= peerTTL {
		return LastDeviceNotice{}
	}
	notice := LastDeviceNotice{
		HasDevice:    true,
		Host:         s.peerHost,
		Port:         s.peerPort,
		Addr:         net.JoinHostPort(s.peerHost, strconv.Itoa(s.peerPort)),
		LastSeenUnix: s.lastSeen.Unix(),
		DeviceName:   s.deviceName,
		Model:        s.deviceModel,
		CustomName:   s.customName,
		DisplayName:  displayPhoneName(s.customName, s.deviceName),
	}
	if s.hasBattery {
		pct, ch := s.batteryPct, s.charging
		notice.BatteryPct = &pct
		notice.Charging = &ch
		notice.BatteryUnix = s.batteryAt.Unix()
	}
	return notice
}

// SetCustomName stores the Mac-local rename alias shown instead of the
// phone's advertised name. Empty clears the alias (falls back to the
// advertised name). Over-long input fails closed without touching state.
// The alias lives in device.json keyed to this phone; advertised facts keep
// updating underneath so clearing reveals the current phone name.
func (s *Service) SetCustomName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed != "" {
		alias, ok := core.SanitizeDeviceLabel(trimmed)
		if !ok {
			return "", fmt.Errorf("name must be 1..%d characters", core.MaxDeviceLabelLen)
		}
		trimmed = alias
	}
	s.mu.Lock()
	if s.lastHost == "" || s.lastPort <= 0 {
		s.mu.Unlock()
		return "", errors.New("no remembered phone to rename")
	}
	s.customName = trimmed
	dev := s.snapshotLastDeviceLocked()
	s.mu.Unlock()
	if err := StoreLastDevice(dev); err != nil {
		return "", fmt.Errorf("save custom name: %w", err)
	}
	if trimmed == "" {
		return "Name cleared. Showing the phone's own name.", nil
	}
	return "Phone renamed.", nil
}

// SendPingToPhone pings the phone's listener at the captured peer address
// using core.SendPing and the pair token. It returns a short RTT summary;
// the RTT is also appended to the log. An update-required reply is logged
// with the parseable prefix (verbatim canonical message) and returned as an
// error so the UI can banner immediately; an auth rejection is logged and
// returned with the peer kept. Any other (dial/network) failure clears the
// ephemeral peer (the remembered device stays) and logs
// "phone peer lost (<err>)" so IsPaired flips false on the next poll.
// Fail closed while peer coordinates are unknown.
func (s *Service) SendPingToPhone() (string, error) {
	s.mu.Lock()
	host, port, seen := s.peerHost, s.peerPort, s.lastSeen
	s.mu.Unlock()
	if host == "" || port <= 0 || time.Since(seen) >= peerTTL {
		return "", errors.New("no phone peer captured yet — ask the phone to ping this Mac first")
	}
	return s.pingPhone(host, port, true)
}

// ReconnectToLastDevice redials the remembered phone even after the
// ephemeral peer expired or the app restarted. Success refreshes the peer
// (IsPaired flips true); dial failures keep the remembered device so the UI
// still shows "Last connected". Fail closed when no phone ever paired.
func (s *Service) ReconnectToLastDevice() (string, error) {
	s.mu.Lock()
	host, port := s.lastHost, s.lastPort
	s.mu.Unlock()
	if host == "" || port <= 0 {
		return "", errors.New("no last device yet — pair with the QR first")
	}
	return s.pingPhone(host, port, false)
}

// ForgetLastDevice drops the remembered phone: it clears the ephemeral
// return path, the TOFU phone pin, and the persisted device.json so the UI
// falls back to the pairing flow. Pair identity and token are untouched
// (the QR keeps working). Fail closed when no phone was ever remembered;
// a disk-delete failure still clears memory but returns the wrapped error.
func (s *Service) ForgetLastDevice() (string, error) {
	s.mu.Lock()
	if s.lastHost == "" || s.lastPort <= 0 {
		s.mu.Unlock()
		return "", errors.New("no remembered phone to forget")
	}
	s.peerHost, s.peerPort = "", 0
	s.peerFingerprint = ""
	s.lastSeen = time.Time{}
	s.lastHost, s.lastPort = "", 0
	s.lastDeviceSeen = time.Time{}
	s.deviceName, s.deviceModel, s.customName = "", "", ""
	s.batteryPct, s.hasBattery, s.charging = 0, false, false
	s.batteryAt = time.Time{}
	s.lastRotationKind = ""
	s.lastRotationLog = time.Time{}
	s.mu.Unlock()
	path, err := DeviceFilePath()
	if err != nil {
		return "", fmt.Errorf("resolve last device path: %w", err)
	}
	if derr := os.Remove(path); derr != nil && !os.IsNotExist(derr) {
		s.appendLine("last device delete failed: " + derr.Error())
		return "", fmt.Errorf("delete last device: %w", derr)
	}
	s.appendLine("forgot last device")
	return "Phone forgotten. Scan the code to pair again.", nil
}

// HeartbeatTick is the 20s auto-reconnect tick (mirrors the Android
// heartbeat): when paired it refreshes presence with a ping; when expired it
// redials the remembered phone. Failures only land in the log — never an
// error return, never the update banner path beyond setUpdate.
func (s *Service) HeartbeatTick() {
	if s.IsPaired() {
		_, _ = s.SendPingToPhone()
		return
	}
	s.mu.Lock()
	has := s.lastHost != "" && s.lastPort > 0
	s.mu.Unlock()
	if has {
		_, _ = s.ReconnectToLastDevice()
	}
}

// pingPhone is the shared outbound-ping core: dial host:port, handle
// version-gate/auth/peer-lost identically for manual and reconnect pings.
// When clearEphemeral is true a dial failure clears the live peer (manual
// ping); reconnect keeps the live peer cleared already and only refreshes on
// success. Success refreshes both the ephemeral peer and the remembered
// device. Fail closed on client build errors.
func (s *Service) pingPhone(host string, port int, clearEphemeral bool) (string, error) {
	client, err := s.phoneClient()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	_, err = core.SendPing(ctx, client, PeerBaseURL(host, port), s.token,
		core.CurrentSender(senderPlatform),
		[]string{core.CapabilityPing}, start)
	if err != nil {
		var upd *core.UpdateRequiredError
		if errors.As(err, &upd) {
			s.setUpdateDetail(upd.Message, upd.RequiredBuild > core.CurrentBuild, upd.RequiredVersion, upd.CurrentVersion, upd.RequiredBuild)
			return "", fmt.Errorf("ping phone: %w", err)
		}
		if isCertMismatch(err) {
			// Reachable but rotated phone cert: keep the peer (inbound
			// pings still refresh presence) and wait for the phone's next
			// authenticated ping to re-pin via setPeer. Never clear here —
			// that caused the Online/Offline flap. Repeats collapse: the
			// phone re-announces on launch, so a tight retry loop stays
			// quiet instead of spamming mismatch lines.
			s.logRotationOnce("cert-mismatch",
				"phone cert mismatch ("+err.Error()+") — waiting for phone ping to re-pin")
		} else if isPeerLost(err) {
			if clearEphemeral {
				s.clearPeer()
			}
			// Same collapse for stale ports (phone restart = new ephemeral
			// port): first line is loud, repeats within the window are quiet.
			s.logRotationOnce("peer-lost", "phone peer lost ("+err.Error()+")")
		} else {
			s.appendLine("ping to phone failed: " + err.Error())
		}
		return "", fmt.Errorf("ping phone: %w", err)
	}
	rtt := time.Since(start)
	s.refreshPeer(host, port)
	s.appendLine("ping to phone ok rtt_ms=" + strconv.FormatInt(rtt.Milliseconds(), 10))
	return "Phone replied in " + strconv.FormatInt(rtt.Milliseconds(), 10) + " ms", nil
}

// WrapHandler returns next with peer capture. On an accepted POST /ping it
// records the phone's remote IP plus the payload's reply_port; on a 426 it
// lifts the canonical update message into the log with a parseable prefix.
// A package function (not a method) so Wails does not bind it: it takes an
// http.Handler, which has no JSON representation. All parsing lives in the
// pure helpers below; this stays thin.
func WrapHandler(s *Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ping" || r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		host := SplitRemoteHost(r.RemoteAddr)
		body, err := io.ReadAll(io.LimitReader(r.Body, core.MaxBodyBytes))
		if err != nil {
			// Body unreadable: core will reject it; nothing to learn.
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(nil))
			next.ServeHTTP(w, r)
			return
		}
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		switch rec.status {
		case http.StatusOK:
			if port, fp, ok := ParsePeerPingFull(body); ok {
				s.setPeerWithFacts(host, port, fp, ParsePeerDevice(body))
			}
		case http.StatusUpgradeRequired:
			detail, ok := ParseUpdateDetail(rec.body)
			if !ok {
				s.setUpdate("peer requires an update", false)
				return
			}
			s.setUpdateDetail(detail.Message, detail.Self, detail.RequiredVersion, detail.CurrentVersion, detail.RequiredBuild)
		}
	})
}

// ServePairServer serves the core ping handler over TLS with peer capture.
// A package function (not a method) so Wails does not bind it: *core.Server
// has no JSON representation. Thin wiring only: TLS settings mirror
// core.Server.Serve; the handler is wrapped so accepted phone pings teach us
// the return path.
func ServePairServer(s *Service, srv *core.Server, addr string) error {
	httpsSrv := &http.Server{
		Addr:              addr,
		Handler:           WrapHandler(s, srv.Handler()),
		ReadHeaderTimeout: 5 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{srv.TLSCertificate()},
		},
	}
	if err := httpsSrv.ListenAndServeTLS("", ""); err != nil {
		return fmt.Errorf("serve tls: %w", err)
	}
	return nil
}

// setPeer records validated phone coordinates and surfaces them in the log.
// Kept for existing callers (tests, redial paths) that carry no facts; the
// inbound path uses setPeerWithFacts. fp is variadic so old call sites keep
// compiling.
func (s *Service) setPeer(host string, port int, fp ...string) {
	newFP := ""
	if len(fp) > 0 {
		newFP = normalizeFingerprint(fp[0])
	}
	s.setPeerWithFacts(host, port, newFP, DeviceFacts{})
}

// setPeerWithFacts records validated phone coordinates plus the advertised
// device facts from the same accepted ping. It also refreshes the remembered
// device (persisted best-effort). fp is the phone's TLS fingerprint when the
// ping payload carries it (reply_fingerprint, additive and
// token-authenticated since this only runs on accepted pings): a changed fp
// re-pins so the next outbound ping stops mismatching after a phone
// reinstall or cert rotation. Advertised facts are fail-soft per field:
// absent values keep the previous reading, so older phones simply leave
// model/battery unknown. Facts never reach the log (device names are PII).
func (s *Service) setPeerWithFacts(host string, port int, fp string, facts DeviceFacts) {
	if host == "" || port <= 0 {
		return
	}
	newFP := normalizeFingerprint(fp)
	s.mu.Lock()
	now := time.Now()
	s.peerHost, s.peerPort = host, port
	s.lastSeen = now
	s.lastHost, s.lastPort, s.lastDeviceSeen = host, port, now
	oldFP := s.peerFingerprint
	if newFP != "" && newFP != oldFP {
		s.peerFingerprint = newFP
	}
	if facts.HasName {
		s.deviceName = facts.DeviceName
	}
	if facts.HasModel {
		s.deviceModel = facts.Model
	}
	if facts.HasBattery {
		s.batteryPct, s.hasBattery, s.batteryAt = facts.BatteryPct, true, now
		s.charging = facts.HasCharging && facts.Charging
	}
	dev := s.snapshotLastDeviceLocked()
	// Fresh inbound ping ends any rotation window: the next failure is a new
	// incident and must be loud again.
	s.lastRotationKind = ""
	s.lastRotationLog = time.Time{}
	s.mu.Unlock()
	s.persistSnapshot(dev)
	if newFP != "" && oldFP != "" && newFP != oldFP {
		s.appendLine("phone cert updated fingerprint=" + newFP)
	}
	s.appendLine("phone peer captured host=" + host + " port=" + strconv.Itoa(port))
}

// refreshPeer marks a successful outbound ping as fresh presence without the
// inbound "captured" log line (the RTT line already covers it). It keeps the
// remembered device current, persisted best-effort.
func (s *Service) refreshPeer(host string, port int) {
	if host == "" || port <= 0 {
		return
	}
	s.mu.Lock()
	now := time.Now()
	s.peerHost, s.peerPort = host, port
	s.lastSeen = now
	s.lastHost, s.lastPort, s.lastDeviceSeen = host, port, now
	dev := s.snapshotLastDeviceLocked()
	s.lastRotationKind = ""
	s.lastRotationLog = time.Time{}
	s.mu.Unlock()
	s.persistSnapshot(dev)
}

// clearPeer drops the ephemeral return path (host, port, freshness) while
// keeping the remembered device, the TOFU cert pin, and typed update state.
func (s *Service) clearPeer() {
	s.mu.Lock()
	s.peerHost, s.peerPort = "", 0
	s.lastSeen = time.Time{}
	s.mu.Unlock()
}

// snapshotLastDeviceLocked builds the persisted shape from current memory.
// Call with s.mu held. Facts and the rename alias ride along so every save
// preserves them.
func (s *Service) snapshotLastDeviceLocked() LastDevice {
	dev := LastDevice{
		Host:         s.lastHost,
		Port:         s.lastPort,
		LastSeenUnix: s.lastDeviceSeen.Unix(),
		Fingerprint:  s.peerFingerprint,
		DeviceName:   s.deviceName,
		Model:        s.deviceModel,
		CustomName:   s.customName,
	}
	if s.hasBattery {
		pct, ch := s.batteryPct, s.charging
		dev.BatteryPct = &pct
		dev.Charging = &ch
		dev.BatteryUnix = s.batteryAt.Unix()
	}
	return dev
}

// persistSnapshot writes the remembered phone best-effort: failures land
// in the log, never in the ping return path.
func (s *Service) persistSnapshot(dev LastDevice) {
	if err := StoreLastDevice(dev); err != nil {
		s.appendLine("last device save failed: " + err.Error())
	}
}

// isPeerLost reports whether a SendPingToPhone failure means the phone is
// gone (dial/network) rather than a version gate, an auth rejection, or a
// cert identity change. Update-required keeps the peer (reachable, outdated);
// auth keeps it too (wrong token, not a dead route); fingerprint mismatch
// keeps it as well (reachable, but rotated cert — clearing would flap
// Online/Offline on every heartbeat while the inbound ping still arrives).
// Pure.
func isPeerLost(err error) bool {
	var upd *core.UpdateRequiredError
	if errors.As(err, &upd) {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, core.CodeUnauthorized) {
		return false
	}
	if strings.Contains(strings.ToLower(msg), "fingerprint") || strings.Contains(strings.ToLower(msg), "certificate") {
		return false
	}
	return true
}

// isCertMismatch reports a TOFU pin failure vs dial/network. Pure.
func isCertMismatch(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "fingerprint") || strings.Contains(msg, "certificate")
}

func (s *Service) appendLine(line string) {
	_, _ = s.logs.Write([]byte(line + "\n"))
}

// rotationLogWindow is how long repeats of the same rotation failure stay
// quiet after the first loud line. Long enough to cover the phone's instant
// re-announce plus one heartbeat (20s), short enough that a genuinely stuck
// peer surfaces again.
const rotationLogWindow = 60 * time.Second

// logRotationOnce logs the first rotation failure loudly and suppresses
// repeats of the same kind within rotationLogWindow. A fresh setPeer or
// refreshPeer resets the window, so the next incident is loud again.
func (s *Service) logRotationOnce(kind, line string) {
	s.mu.Lock()
	now := time.Now()
	if kind == s.lastRotationKind && now.Sub(s.lastRotationLog) < rotationLogWindow {
		s.mu.Unlock()
		return
	}
	s.lastRotationKind = kind
	s.lastRotationLog = now
	s.mu.Unlock()
	s.appendLine(line)
}

// phoneClient returns a TLS client pinned to the phone's cert. The phone
// presents no client cert to our server (core.Serve has no ClientAuth), so
// no handshake on our side can teach us its fingerprint with zero core
// changes. Fallback: TOFU-accept the cert on the first outbound ping, pin
// it for later pings, and surface the pin in the log. The return path is
// still authenticated by the pair token (Bearer), which the phone proved
// over our QR-pinned channel when it pinged us first.
func (s *Service) phoneClient() (*http.Client, error) {
	s.mu.Lock()
	fp := s.peerFingerprint
	s.mu.Unlock()
	if fp != "" {
		client, err := core.NewTOFUClient(fp)
		if err != nil {
			return nil, fmt.Errorf("build pinned phone client: %w", err)
		}
		return client, nil
	}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				// security: first-use TOFU accept, pinned afterwards via
				// pinPeerFingerprint. InsecureSkipVerify is contained by
				// VerifyPeerCertificate, which never returns nil without
				// pinning what it saw.
				InsecureSkipVerify: true, // #nosec G402 -- first-use TOFU, pinned after, see above
				VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
					if len(rawCerts) == 0 {
						return errors.New("no peer certificate presented")
					}
					sum := sha256.Sum256(rawCerts[0])
					s.pinPeerFingerprint(hex.EncodeToString(sum[:]))
					return nil
				},
			},
		},
	}, nil
}

// pinPeerFingerprint pins the first phone cert seen and logs the pin once;
// later certs are ignored so a changed cert fails closed at NewTOFUClient.
// The pin is folded into the remembered device when one is known.
func (s *Service) pinPeerFingerprint(fp string) {
	s.mu.Lock()
	first := s.peerFingerprint == ""
	if first {
		s.peerFingerprint = fp
	}
	var dev LastDevice
	var has bool
	if s.lastHost != "" && s.lastPort > 0 {
		dev = s.snapshotLastDeviceLocked()
		has = true
	}
	s.mu.Unlock()
	if first {
		s.appendLine("phone cert pinned fingerprint=" + fp)
		if has {
			s.persistSnapshot(dev)
		}
	}
}

// ParsePeerPing extracts the phone's listening port from a raw ping envelope
// body. The Android side sends its ping with an extra payload field
// "reply_port"; core ignores unknown fields, so this reads the raw JSON.
// Pure: no I/O, no state. ok is false when the body is not a ping or the
// port is absent or outside 1-65535.
func ParsePeerPing(body []byte) (port int, ok bool) {
	port, _, ok = ParsePeerPingFull(body)
	return port, ok
}

// ParsePeerPingFull also extracts the optional "reply_fingerprint" (phone TLS
// pin) the phone advertises alongside reply_port. Empty fingerprint means an
// older phone build; callers keep the existing pin in that case. Pure.
func ParsePeerPingFull(body []byte) (port int, fingerprint string, ok bool) {
	var env struct {
		Type    string `json:"type"`
		Payload struct {
			ReplyPort        *int   `json:"reply_port"`
			ReplyFingerprint string `json:"reply_fingerprint"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return 0, "", false
	}
	if env.Type != core.TypePing || env.Payload.ReplyPort == nil {
		return 0, "", false
	}
	if p := *env.Payload.ReplyPort; p >= 1 && p <= 65535 {
		return p, normalizeFingerprint(env.Payload.ReplyFingerprint), true
	}
	return 0, "", false
}

// normalizeFingerprint lowercases and strips colons/whitespace users or the
// phone add when copying fingerprints. Pure.
func normalizeFingerprint(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, ":", "")
	s = strings.ReplaceAll(s, " ", "")
	return s
}

// UpdateDetail is the parsed 426 outcome with display versions.
type UpdateDetail struct {
	Message         string
	Self            bool
	RequiredVersion string
	CurrentVersion  string
	RequiredBuild   int
}

// ParseUpdateReply extracts the canonical update message from a captured 426
// response body. self is true when the required build exceeds our own build,
// i.e. this Mac is the outdated side. Pure: no I/O, no state.
func ParseUpdateReply(body []byte) (msg string, self bool, ok bool) {
	detail, ok := ParseUpdateDetail(body)
	if !ok {
		return "", false, false
	}
	return detail.Message, detail.Self, true
}

// ParseUpdateDetail extracts the full update outcome including display
// versions ("" when the peer predates app_version). Pure: no I/O, no state.
func ParseUpdateDetail(body []byte) (UpdateDetail, bool) {
	var env core.Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return UpdateDetail{}, false
	}
	if env.Type != core.TypeError {
		return UpdateDetail{}, false
	}
	var upd core.UpdateRequiredPayload
	if err := json.Unmarshal(env.Payload, &upd); err != nil {
		return UpdateDetail{}, false
	}
	if upd.Code != core.CodeUpdateRequired || upd.Message == "" {
		return UpdateDetail{}, false
	}
	return UpdateDetail{
		Message:         upd.Message,
		Self:            upd.RequiredBuild > core.CurrentBuild,
		RequiredVersion: upd.RequiredVersion,
		CurrentVersion:  upd.CurrentVersion,
		RequiredBuild:   upd.RequiredBuild,
	}, true
}

// UpdateRequiredLine formats a parseable update log line: the peer-outdated
// prefix, or the self-outdated prefix when this Mac must update. The message
// stays verbatim (canonical core text, never paraphrased).
func UpdateRequiredLine(msg string, self bool) string {
	if self {
		return UpdateRequiredSelfPrefix + msg
	}
	return UpdateRequiredPrefix + msg
}

// SplitRemoteHost returns the host half of a "host:port" remote address,
// falling back to the whole value when it has no port. Pure.
func SplitRemoteHost(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

// PeerBaseURL builds the https base URL for pinging the phone back. Pure.
func PeerBaseURL(host string, port int) string {
	return "https://" + net.JoinHostPort(host, strconv.Itoa(port))
}

// statusRecorder captures the status and (capped) body core wrote so the
// wrapper can learn peer coordinates and update messages. Thin: everything
// is forwarded to the real writer.
type statusRecorder struct {
	http.ResponseWriter
	status int
	body   []byte
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if len(r.body) < core.MaxBodyBytes {
		r.body = append(r.body, p...)
		if len(r.body) > core.MaxBodyBytes {
			r.body = r.body[:core.MaxBodyBytes]
		}
	}
	return r.ResponseWriter.Write(p)
}
