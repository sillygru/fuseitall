// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

// PairPayload is the QR content: everything the scanning side needs to find
// us (host/port), pin us (fingerprint), and authenticate (token, pubkey).
// The "v" field is the QR format version, defaulting to 1 when absent.
type PairPayload struct {
	V           int    `json:"v"`
	DeviceName  string `json:"device_name"`
	Platform    string `json:"platform"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Fingerprint string `json:"fingerprint"`
	PubKey      string `json:"pubkey"`
	Token       string `json:"token"`
	// Code is the human-typable 6-digit pairing code derived from Token
	// (see PairingCode). Additive: readers that do not know it ignore it.
	Code string `json:"code,omitempty"`
}

// MakePairPayload builds the QR payload. The format version is fixed at 1
// internally (no version args): readers default a missing "v" to 1 and
// reject anything newer via ParsePairQR.
func MakePairPayload(deviceName, platform, host string, port int, fingerprint string, pub ed25519.PublicKey, token string) PairPayload {
	return PairPayload{
		V:           1,
		DeviceName:  deviceName,
		Platform:    platform,
		Host:        host,
		Port:        port,
		Fingerprint: fingerprint,
		PubKey:      base64.StdEncoding.EncodeToString(pub),
		Token:       token,
		Code:        PairingCode(token),
	}
}

// PairingCode derives the 6-digit, zero-padded, human-typable pairing code
// from a pair token. It is anti-mistake UX plus explicit user consent — NOT
// a security boundary: the token in the scanned QR remains the secret, and
// a swapped QR defeats the code exactly as it defeats a fingerprint check.
// Deterministic (same token => same code) and pure.
func PairingCode(token string) string {
	sum := sha256.Sum256([]byte("fuseitall-code|1|" + token))
	n := binary.BigEndian.Uint32(sum[:4]) % 1000000
	return fmt.Sprintf("%06d", n)
}

// VerifyToken compares pair tokens in constant time. Mismatched lengths
// fail closed (ConstantTimeCompare returns 0); callers treat false as
// unauthorized without distinguishing why.
func VerifyToken(expected, provided string) bool {
	return subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1
}

// RotatePairToken mints a fresh 128-bit pairing token (32 hex chars) from
// crypto/rand.
func RotatePairToken() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate pair token: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}
