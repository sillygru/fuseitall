// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	// NonceSizeStandard is the 96-bit (12 byte) standard nonce size for AES-GCM.
	NonceSizeStandard = 12

	// KeySize256 is the 32-byte (256-bit) key size for AES-256.
	KeySize256 = 32

	// EncryptedPrefix marks encrypted string fields in storage.
	EncryptedPrefix = "enc:v1:"
)

// Subkeys holds the domain-separated cryptographic keys derived from a master secret.
type Subkeys struct {
	AuthToken  string // Bearer token for HTTP/WebSocket authentication (hex string)
	DBKey      []byte // 256-bit AES key for database encryption
	PayloadKey []byte // 256-bit AES key for payload encryption
}

// DeriveSubkeys derives domain-separated cryptographic keys from a master pairing secret using HKDF-SHA256.
func DeriveSubkeys(masterSecret string) (Subkeys, error) {
	if len(masterSecret) == 0 {
		return Subkeys{}, errors.New("master secret must not be empty")
	}

	secretBytes := []byte(masterSecret)

	authBytes, err := hkdf.Key(sha256.New, secretBytes, nil, "fuseitall-auth-v1", KeySize256)
	if err != nil {
		return Subkeys{}, fmt.Errorf("derive auth token: %w", err)
	}

	dbKey, err := hkdf.Key(sha256.New, secretBytes, nil, "fuseitall-db-v1", KeySize256)
	if err != nil {
		return Subkeys{}, fmt.Errorf("derive db key: %w", err)
	}

	payloadKey, err := hkdf.Key(sha256.New, secretBytes, nil, "fuseitall-payload-v1", KeySize256)
	if err != nil {
		return Subkeys{}, fmt.Errorf("derive payload key: %w", err)
	}

	return Subkeys{
		AuthToken:  hex.EncodeToString(authBytes),
		DBKey:      dbKey,
		PayloadKey: payloadKey,
	}, nil
}

// DeriveDBKey derives the 256-bit AES-GCM database encryption key from a master secret using HKDF-SHA256.
func DeriveDBKey(masterSecret string) ([]byte, error) {
	if len(masterSecret) == 0 {
		return nil, errors.New("master secret must not be empty")
	}
	return hkdf.Key(sha256.New, []byte(masterSecret), nil, "fuseitall-db-v1", KeySize256)
}

// EncryptBytes encrypts plaintext with AES-256-GCM using a freshly generated 12-byte random nonce.
// Returns [12-byte nonce || ciphertext || 16-byte tag].
func EncryptBytes(key, plaintext []byte) ([]byte, error) {
	if len(key) != KeySize256 {
		return nil, fmt.Errorf("invalid key size: expected %d bytes, got %d", KeySize256, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create aes cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	// Seal appends ciphertext + tag to nonce
	out := gcm.Seal(nonce, nonce, plaintext, nil)
	return out, nil
}

// DecryptBytes decrypts ciphertext [12-byte nonce || ciphertext || 16-byte tag] using AES-256-GCM.
func DecryptBytes(key, ciphertext []byte) ([]byte, error) {
	if len(key) != KeySize256 {
		return nil, fmt.Errorf("invalid key size: expected %d bytes, got %d", KeySize256, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create aes cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize+gcm.Overhead() {
		return nil, errors.New("ciphertext too short")
	}

	nonce := ciphertext[:nonceSize]
	data := ciphertext[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt gcm: %w", err)
	}

	return plaintext, nil
}

// EncryptString encrypts a UTF-8 string with AES-256-GCM and formats it with EncryptedPrefix:
// "enc:v1:<12-byte nonce hex>:<ciphertext+tag hex>".
// An empty string encrypts to an encrypted representation as well to hide length/emptiness.
func EncryptString(key []byte, plaintext string) (string, error) {
	encrypted, err := EncryptBytes(key, []byte(plaintext))
	if err != nil {
		return "", err
	}
	nonceHex := hex.EncodeToString(encrypted[:NonceSizeStandard])
	dataHex := hex.EncodeToString(encrypted[NonceSizeStandard:])
	return fmt.Sprintf("%s%s:%s", EncryptedPrefix, nonceHex, dataHex), nil
}

// DecryptString decrypts an encrypted string created by EncryptString.
// If the string does not have EncryptedPrefix, it returns an error (fail closed).
func DecryptString(key []byte, encoded string) (string, error) {
	if !strings.HasPrefix(encoded, EncryptedPrefix) {
		return "", errors.New("missing encrypted string prefix")
	}

	trimmed := strings.TrimPrefix(encoded, EncryptedPrefix)
	parts := strings.SplitN(trimmed, ":", 2)
	if len(parts) != 2 {
		return "", errors.New("invalid encrypted string format")
	}

	nonce, err := hex.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("decode nonce hex: %w", err)
	}
	if len(nonce) != NonceSizeStandard {
		return "", fmt.Errorf("invalid nonce length: %d", len(nonce))
	}

	data, err := hex.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode ciphertext hex: %w", err)
	}

	raw := append(nonce, data...)
	decryptedBytes, err := DecryptBytes(key, raw)
	if err != nil {
		return "", err
	}

	return string(decryptedBytes), nil
}
