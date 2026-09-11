// SPDX-License-Identifier: AGPL-3.0-only

package backend

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"time"

	"fuseitall/core"
)

// decodePairPubKey decodes the base64 identity public key for QR rebuilds.
// An ed25519 public key is 32 bytes; anything else fails closed.
func decodePairPubKey(enc string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return nil, fmt.Errorf("decode pair pubkey: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("pair pubkey must be %d bytes, got %d", ed25519.PublicKeySize, len(raw))
	}
	return ed25519.PublicKey(raw), nil
}

// deleteLastDeviceFile removes the persisted last-phone file. Missing files
// are fine; other failures are wrapped for the caller to log and return.
func deleteLastDeviceFile() error {
	path, err := DeviceFilePath()
	if err != nil {
		return fmt.Errorf("resolve last device path: %w", err)
	}
	if derr := os.Remove(path); derr != nil && !os.IsNotExist(derr) {
		return fmt.Errorf("delete last device: %w", derr)
	}
	return nil
}

// QRInputs are the static ingredients of the pairing QR, stashed at startup
// so forget flows can rebuild the QR after rotating the token. Host/port
// are the boot-time LAN coordinates; a DHCP change keeps the rotated QR
// dialable via the remembered-candidate redial path.
type QRInputs struct {
	DeviceName  string
	Platform    string
	Host        string
	Port        int
	Fingerprint string
	PubKey      ed25519.PublicKey
	Candidates  string
}

// ConfigurePairing stashes the QR rebuild inputs. Called once by main.go;
// tests that never rotate can skip it (rotation fails closed without it).
// Scalar args only so the Wails binding stays JSON-representable;
// pubkeyBase64 is the base64 identity public key from the pair payload.
// candidates is an optional comma-separated list of candidate host IPs.
func (s *Service) ConfigurePairing(deviceName, platform, host string, port int, fingerprint, pubkeyBase64, candidates string) {
	pub, err := decodePairPubKey(pubkeyBase64)
	s.mu.Lock()
	s.qrInputs = QRInputs{
		DeviceName: deviceName, Platform: platform, Host: host,
		Port: port, Fingerprint: fingerprint, PubKey: pub,
		Candidates: candidates,
	}
	s.qrConfigured = err == nil
	s.mu.Unlock()
}

// bindServer hands the service the live core server so token rotation
// propagates to request verification. Unexported (like WrapHandler, it
// takes a type with no JSON representation, so Wails must not bind it);
// ServePairServer calls it at startup.
func (s *Service) bindServer(srv *core.Server) {
	s.mu.Lock()
	s.coreServer = srv
	s.mu.Unlock()
}

// rotatePairing mints a fresh pair token (persisted, live server updated)
// and rebuilds the QR JSON. Old-token peers get 403 from here on, which
// phones surface as "Mac unpaired this device — scan its new QR". Fail
// closed: any step failing leaves token, server, QR, and disk untouched.
func (s *Service) rotatePairing() error {
	s.mu.Lock()
	qr, ok := s.qrInputs, s.qrConfigured
	srv := s.coreServer
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("pairing inputs not configured")
	}
	if srv == nil {
		return fmt.Errorf("core server not bound")
	}
	newToken, err := RotatePairFileToken()
	if err != nil {
		return fmt.Errorf("rotate pair token: %w", err)
	}
	pair := core.MakePairPayload(qr.DeviceName, qr.Platform, qr.Host, qr.Port, qr.Fingerprint, qr.PubKey, newToken, qr.Candidates)
	raw, err := core.EncodePairQR(pair)
	if err != nil {
		return fmt.Errorf("encode rotated pair qr: %w", err)
	}
	if err := srv.SetToken(newToken); err != nil {
		return err
	}
	if s.db != nil {
		if newDBKey, err := core.DeriveDBKey(newToken); err == nil {
			_ = s.db.SetKey(newDBKey)
		}
	}
	s.mu.Lock()
	s.token = newToken
	s.pairJSON = string(raw)
	s.mu.Unlock()
	s.appendLine("pair token rotated")
	return nil
}

// ingestUnpairBody drops the peer after an accepted phone goodbye (HTTP 200
// through the full version + token gate). No token rotation here: the phone
// already wiped itself, so changing the QR would only force a pointless
// re-scan for the next pairing.
func (s *Service) ingestUnpairBody() {
	s.mu.Lock()
	had := s.lastHost != "" && s.lastPort > 0
	s.peerHost, s.peerPort = "", 0
	s.peerFingerprint = ""
	s.lastSeen = time.Time{}
	s.lastHost, s.lastPort = "", 0
	s.candidateHosts = nil
	s.lastDeviceSeen = time.Time{}
	s.deviceName, s.deviceModel, s.customName = "", "", ""
	s.batteryPct, s.hasBattery, s.charging = 0, false, false
	s.batteryAt = time.Time{}
	s.filesPermission, s.photosPermission = "", ""
	// Same identity drop as ForgetLastDevice: never reuse the departed
	// phone's cached build for the next pairing's version gate.
	s.peerPlatform, s.peerBuild, s.peerVersion = "", 0, ""
	s.peerCapabilities = nil
	s.peerLearnedAt = time.Time{}
	s.lastUpdateSet, s.lastUpdateSelf = false, false
	s.lastUpdateMsg, s.lastUpdateReqVer, s.lastUpdateCurVer = "", "", ""
	s.lastUpdateReqBuild = 0
	s.lastRotationKind = ""
	s.lastRotationLog = time.Time{}
	s.lastRejectKind = ""
	s.lastRejectUnix = 0
	s.lastAcceptUnix = 0
	s.mu.Unlock()
	if had {
		if err := deleteLastDeviceFile(); err != nil {
			s.appendLine("last device delete failed: " + err.Error())
		}
	}
	s.appendLine("phone unpaired")
}
