// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Identity is a device's long-term ed25519 identity keypair.
type Identity struct {
	PrivateKey ed25519.PrivateKey
	PublicKey  ed25519.PublicKey
}

// GenerateIdentity creates a fresh ed25519 identity using crypto/rand.
func GenerateIdentity() (Identity, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Identity{}, fmt.Errorf("generate identity: %w", err)
	}
	return Identity{PrivateKey: priv, PublicKey: pub}, nil
}

// Fingerprint returns the hex SHA-256 of a public key for manual
// cross-device verification ("do these codes match?").
func Fingerprint(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:])
}
