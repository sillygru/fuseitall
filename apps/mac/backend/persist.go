// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"crypto/ed25519"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"fuseitall/core"
)

// Load-or-create for the Mac's long-term pair state (ed25519 identity seed
// + pair token) so a restart keeps the same QR. Deleting the file below is
// the documented reset: the next launch mints a fresh identity and token.
//
// Token rotation is deferred: the token stays stable across restarts for
// the BASE milestone (no rotation UI yet).
//
// Pair fields stay core-shaped: identity via core.GenerateIdentity, token
// via core.RotatePairToken; only the 32-byte ed25519 seed is persisted and
// the keypair is rebuilt with ed25519.NewKeyFromSeed on load.

// pairFile is the on-disk shape: base64 32-byte ed25519 seed + pair token +
// PEM-encoded TLS cert/key. The TLS cert is persisted so the QR fingerprint
// stays stable across restarts; old files without it are migrated (cert
// minted, identity + token kept) instead of rotating the pairing code.
type pairFile struct {
	Seed    string `json:"identity_seed"`
	Token   string `json:"token"`
	CertPEM string `json:"tls_cert_pem,omitempty"`
	KeyPEM  string `json:"tls_key_pem,omitempty"`
}

// PairFilePath returns ~/Library/Application Support/FuseItAll/pair.json.
// It derives the home dir from os.UserHomeDir so tests can override it via
// t.Setenv("HOME", tmp).
func PairFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, "Library", "Application Support", "FuseItAll", "pair.json"), nil
}

// LoadOrCreatePairState loads the persisted identity + token + TLS cert, or
// mints and stores them (0600 file mode) on first launch. A corrupt file is
// treated like a missing one: warn, mint fresh, overwrite. A valid pre-cert
// file (no PEM blocks) keeps its identity + token and only mints the cert,
// so the pairing code stays stable while the QR gains a stable fingerprint.
func LoadOrCreatePairState(logger *slog.Logger) (core.Identity, string, tls.Certificate, string, error) {
	if logger == nil {
		logger = slog.Default()
	}
	path, err := PairFilePath()
	if err != nil {
		return core.Identity{}, "", tls.Certificate{}, "", err
	}
	if raw, rerr := os.ReadFile(path); rerr == nil {
		id, token, cert, fp, perr := decodePairFile(raw)
		if perr == nil {
			if len(cert.Certificate) != 0 {
				logger.Info("pair state loaded", "path", path)
				return id, token, cert, fp, nil
			}
			// Migration: identity + token valid, TLS cert absent (pre-cert
			// build). Mint the cert, keep the code stable.
			minted, mfp, merr := core.GenerateSelfSignedCert()
			if merr != nil {
				return core.Identity{}, "", tls.Certificate{}, "", merr
			}
			if serr := storePairStateWithCert(path, id.PrivateKey.Seed(), token, minted); serr != nil {
				return core.Identity{}, "", tls.Certificate{}, "", serr
			}
			logger.Info("pair state migrated with tls cert", "path", path)
			return id, token, minted, mfp, nil
		}
		logger.Warn("pair state corrupt, recreating", "path", path, "err", perr)
	} else if !os.IsNotExist(rerr) {
		return core.Identity{}, "", tls.Certificate{}, "", fmt.Errorf("read pair state: %w", rerr)
	}
	id, err := core.GenerateIdentity()
	if err != nil {
		return core.Identity{}, "", tls.Certificate{}, "", err
	}
	token, err := core.RotatePairToken()
	if err != nil {
		return core.Identity{}, "", tls.Certificate{}, "", err
	}
	cert, fp, err := core.GenerateSelfSignedCert()
	if err != nil {
		return core.Identity{}, "", tls.Certificate{}, "", err
	}
	if err := storePairStateWithCert(path, id.PrivateKey.Seed(), token, cert); err != nil {
		return core.Identity{}, "", tls.Certificate{}, "", err
	}
	logger.Info("pair state created", "path", path)
	return id, token, cert, fp, nil
}

// decodePairFile validates and rebuilds the identity + token + TLS cert.
// Missing PEM blocks are not an error here: the caller migrates by minting
// a cert while keeping identity + token. Malformed PEM blocks are an error
// (treated as corrupt, full recreate). Pure except for PEM parsing.
func decodePairFile(raw []byte) (core.Identity, string, tls.Certificate, string, error) {
	var pf pairFile
	if err := json.Unmarshal(raw, &pf); err != nil {
		return core.Identity{}, "", tls.Certificate{}, "", fmt.Errorf("decode pair state: %w", err)
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(pf.Seed))
	if err != nil {
		return core.Identity{}, "", tls.Certificate{}, "", fmt.Errorf("decode identity seed: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		return core.Identity{}, "", tls.Certificate{}, "", fmt.Errorf("identity seed must be %d bytes, got %d", ed25519.SeedSize, len(seed))
	}
	if strings.TrimSpace(pf.Token) == "" {
		return core.Identity{}, "", tls.Certificate{}, "", fmt.Errorf("pair token must not be empty")
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	id := core.Identity{PrivateKey: priv, PublicKey: pub}
	token := strings.TrimSpace(pf.Token)
	if strings.TrimSpace(pf.CertPEM) == "" || strings.TrimSpace(pf.KeyPEM) == "" {
		return id, token, tls.Certificate{}, "", nil
	}
	cert, fp, err := core.ParseTLSCertPEM([]byte(pf.CertPEM), []byte(pf.KeyPEM))
	if err != nil {
		return core.Identity{}, "", tls.Certificate{}, "", fmt.Errorf("decode tls cert: %w", err)
	}
	return id, token, cert, fp, nil
}

// LastDevice is the last phone return path the Mac learned from an accepted
// ping. Persisted so a restart still shows the phone name, model, and battery
// and can auto-reconnect without a fresh QR scan. No secrets: host/port are
// LAN coordinates, fingerprint is the TOFU pin (public cert hash), never the
// pair token. DeviceName/Model/Battery are the phone's latest advertised
// facts; CustomName is the Mac-local rename alias that overrides DeviceName
// for display (empty = no override). Battery fields are pointers so unknown
// stays absent instead of colliding with a real 0% / not-charging reading.
type LastDevice struct {
	Host           string   `json:"host"`
	Port           int      `json:"port"`
	LastSeenUnix   int64    `json:"last_seen_unix"`
	Fingerprint    string   `json:"fingerprint,omitempty"`
	DeviceName     string   `json:"device_name,omitempty"`
	Model          string   `json:"model,omitempty"`
	BatteryPct     *int     `json:"battery_pct,omitempty"`
	Charging       *bool    `json:"charging,omitempty"`
	BatteryUnix    int64    `json:"battery_unix,omitempty"`
	CustomName     string   `json:"custom_name,omitempty"`
	CandidateHosts []string `json:"candidate_hosts,omitempty"`
}

// DeviceFilePath returns ~/Library/Application Support/FuseItAll/device.json.
func DeviceFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, "Library", "Application Support", "FuseItAll", "device.json"), nil
}

// LoadLastDevice returns the persisted last phone, or has=false when absent.
// A corrupt file returns an error so the caller can warn; it never fails
// closed (the QR flow still works, there is just no last-device card).
func LoadLastDevice() (LastDevice, bool, error) {
	path, err := DeviceFilePath()
	if err != nil {
		return LastDevice{}, false, err
	}
	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return LastDevice{}, false, nil
		}
		return LastDevice{}, false, fmt.Errorf("read last device: %w", rerr)
	}
	dev, err := decodeLastDevice(raw)
	if err != nil {
		return LastDevice{}, false, err
	}
	return dev, true, nil
}

// decodeLastDevice validates the on-disk shape. Pure. Host/port stay strict
// (empty host or out-of-range port is an error); advertised device facts and
// the rename alias are fail-soft (invalid values are dropped, the device is
// still usable) so a hand-edited or future-shape file never breaks pairing.
func decodeLastDevice(raw []byte) (LastDevice, error) {
	var dev LastDevice
	if err := json.Unmarshal(raw, &dev); err != nil {
		return LastDevice{}, fmt.Errorf("decode last device: %w", err)
	}
	if strings.TrimSpace(dev.Host) == "" {
		return LastDevice{}, fmt.Errorf("last device host must not be empty")
	}
	if dev.Port < 1 || dev.Port > 65535 {
		return LastDevice{}, fmt.Errorf("last device port %d out of range", dev.Port)
	}
	if name, ok := core.SanitizeDeviceLabel(dev.DeviceName); ok {
		dev.DeviceName = name
	} else {
		dev.DeviceName = ""
	}
	if model, ok := core.SanitizeDeviceLabel(dev.Model); ok {
		dev.Model = model
	} else {
		dev.Model = ""
	}
	if dev.BatteryPct != nil && !core.SanitizeBatteryPct(*dev.BatteryPct) {
		dev.BatteryPct = nil
		dev.Charging = nil
		dev.BatteryUnix = 0
	}
	if dev.BatteryPct == nil {
		dev.Charging = nil
		dev.BatteryUnix = 0
	}
	if alias, ok := core.SanitizeDeviceLabel(dev.CustomName); ok {
		dev.CustomName = alias
	} else {
		dev.CustomName = ""
	}
	dev.CandidateHosts = core.MergeCandidateHosts(dev.Host, dev.CandidateHosts)
	return dev, nil
}

// RotatePairFileToken mints a fresh pair token, keeping the identity seed
// and TLS cert (fingerprint stable), and persists it. Used by forget flows
// so a forgotten phone's stored token stops verifying (403) instead of
// silently re-capturing the peer on its next ping. The caller must
// propagate the new token to the live server (SetToken) and rebuild the QR.
func RotatePairFileToken() (string, error) {
	path, err := PairFilePath()
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read pair state: %w", err)
	}
	id, _, cert, _, err := decodePairFile(raw)
	if err != nil {
		return "", err
	}
	newToken, err := core.RotatePairToken()
	if err != nil {
		return "", err
	}
	if err := storePairStateWithCert(path, id.PrivateKey.Seed(), newToken, cert); err != nil {
		return "", err
	}
	return newToken, nil
}

// StoreLastDevice writes the last phone coordinates with 0600 file mode
// (dir 0700), tightening pre-existing perms like storePairState.
func StoreLastDevice(dev LastDevice) error {
	path, err := DeviceFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create last device dir: %w", err)
	}
	raw, err := json.Marshal(dev)
	if err != nil {
		return fmt.Errorf("encode last device: %w", err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write last device: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod last device: %w", err)
	}
	return nil
}

// storePairState writes {seed, token} with 0600 file mode (dir 0700).
// WriteFile only applies the mode at creation, so Chmod tightens
// pre-existing files (e.g. 0644 from an older build) on every save.
// Kept for tests covering perm tightening; production uses
// storePairStateWithCert so the QR fingerprint stays stable.
func storePairState(path string, seed []byte, token string) error {
	cert, _, err := core.GenerateSelfSignedCert()
	if err != nil {
		return err
	}
	return storePairStateWithCert(path, seed, token, cert)
}

// storePairStateWithCert writes {seed, token, tls cert/key} with 0600 file
// mode (dir 0700), tightening pre-existing perms on every save.
func storePairStateWithCert(path string, seed []byte, token string, cert tls.Certificate) error {
	certPEM, keyPEM, err := core.EncodeTLSCertPEM(cert)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create pair state dir: %w", err)
	}
	raw, err := json.Marshal(pairFile{
		Seed:    base64.StdEncoding.EncodeToString(seed),
		Token:   token,
		CertPEM: string(certPEM),
		KeyPEM:  string(keyPEM),
	})
	if err != nil {
		return fmt.Errorf("encode pair state: %w", err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write pair state: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod pair state: %w", err)
	}
	return nil
}
