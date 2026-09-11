// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"crypto/rand"
	"strings"
	"testing"
)

func TestDeriveSubkeys(t *testing.T) {
	master := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	subkeys, err := DeriveSubkeys(master)
	if err != nil {
		t.Fatalf("DeriveSubkeys failed: %v", err)
	}

	if len(subkeys.AuthToken) != 64 {
		t.Errorf("expected 64 hex chars AuthToken, got %d", len(subkeys.AuthToken))
	}
	if len(subkeys.DBKey) != 32 {
		t.Errorf("expected 32 bytes DBKey, got %d", len(subkeys.DBKey))
	}
	if len(subkeys.PayloadKey) != 32 {
		t.Errorf("expected 32 bytes PayloadKey, got %d", len(subkeys.PayloadKey))
	}

	// Deterministic
	subkeys2, err := DeriveSubkeys(master)
	if err != nil {
		t.Fatalf("DeriveSubkeys second call failed: %v", err)
	}
	if subkeys.AuthToken != subkeys2.AuthToken {
		t.Errorf("AuthToken not deterministic")
	}

	// Empty master fails closed
	if _, err := DeriveSubkeys(""); err == nil {
		t.Errorf("expected error on empty master secret")
	}
}

func TestEncryptDecryptString(t *testing.T) {
	key := make([]byte, KeySize256)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	messages := []string{
		"Hello world!",
		"",
		"A much longer message with emojis: 🔐 🚀 💻 and special characters: !@#$%^&*()_+{}[]:;\"'\\|<>,.?/",
		"Multi-line\nmessage\r\nwith\ttabs",
	}

	for _, msg := range messages {
		encrypted, err := EncryptString(key, msg)
		if err != nil {
			t.Fatalf("EncryptString failed on %q: %v", msg, err)
		}

		if !strings.HasPrefix(encrypted, EncryptedPrefix) {
			t.Errorf("encrypted string missing prefix: %s", encrypted)
		}

		decrypted, err := DecryptString(key, encrypted)
		if err != nil {
			t.Fatalf("DecryptString failed on %q: %v", msg, err)
		}

		if decrypted != msg {
			t.Errorf("roundtrip mismatch: got %q, want %q", decrypted, msg)
		}

		// Fresh nonce each time
		encrypted2, err := EncryptString(key, msg)
		if err != nil {
			t.Fatal(err)
		}
		if encrypted == encrypted2 {
			t.Errorf("nonces must be unique per encryption")
		}
	}
}

func TestDecryptString_Tamper(t *testing.T) {
	key := make([]byte, KeySize256)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	encrypted, err := EncryptString(key, "Secret message")
	if err != nil {
		t.Fatal(err)
	}

	// Wrong key fails
	wrongKey := make([]byte, KeySize256)
	if _, err := rand.Read(wrongKey); err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptString(wrongKey, encrypted); err == nil {
		t.Errorf("expected error with wrong key")
	}

	// Tampered ciphertext fails
	parts := strings.Split(encrypted, ":")
	if len(parts) >= 3 {
		tampered := parts[0] + ":" + parts[1] + ":" + parts[2]
		// Flip a character in ciphertext
		lastChar := tampered[len(tampered)-1]
		if lastChar == 'a' {
			tampered = tampered[:len(tampered)-1] + "b"
		} else {
			tampered = tampered[:len(tampered)-1] + "a"
		}
		if _, err := DecryptString(key, tampered); err == nil {
			t.Errorf("expected error with tampered ciphertext")
		}
	}

	// Invalid prefix fails
	if _, err := DecryptString(key, "plain string without prefix"); err == nil {
		t.Errorf("expected error on missing prefix")
	}
}

func TestEncryptDecryptBytes(t *testing.T) {
	key := make([]byte, KeySize256)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	raw := []byte("binary payload test bytes 1234567890")
	encrypted, err := EncryptBytes(key, raw)
	if err != nil {
		t.Fatal(err)
	}

	decrypted, err := DecryptBytes(key, encrypted)
	if err != nil {
		t.Fatal(err)
	}

	if string(decrypted) != string(raw) {
		t.Errorf("roundtrip mismatch")
	}
}
