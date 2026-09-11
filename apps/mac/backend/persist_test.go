// SPDX-License-Identifier: AGPL-3.0-only

package backend

import (
	"bytes"
	"encoding/base64"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(ioDiscard{}, nil)) }

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

func TestLoadOrCreateRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	id1, tok1, cert1, fp1, err := LoadOrCreatePairState(testLogger())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if tok1 == "" || len(id1.PublicKey) == 0 {
		t.Fatal("created state must have token and pubkey")
	}
	if len(cert1.Certificate) == 0 || fp1 == "" {
		t.Fatal("created state must have tls cert and fingerprint (stable QR)")
	}
	path, err := PairFilePath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(tmp, "Library", "Application Support", "FuseItAll", "pair.json"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("pair file mode = %o, want 600", info.Mode().Perm())
	}

	id2, tok2, cert2, fp2, err := LoadOrCreatePairState(testLogger())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if tok2 != tok1 || !bytes.Equal(id2.PublicKey, id1.PublicKey) {
		t.Fatal("second load must return the same identity and token (stable QR)")
	}
	if fp2 != fp1 || !bytes.Equal(cert2.Certificate[0], cert1.Certificate[0]) {
		t.Fatal("second load must reuse the same tls cert (stable fingerprint)")
	}
}

func TestLoadOrCreateRecoversFromCorrupt(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	path, err := PairFilePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	id, tok, cert, fp, err := LoadOrCreatePairState(testLogger())
	if err != nil {
		t.Fatalf("recreate: %v", err)
	}
	if tok == "" || len(id.PublicKey) == 0 {
		t.Fatal("recreated state must be usable")
	}
	if len(cert.Certificate) == 0 || fp == "" {
		t.Fatal("recreated state must have a fresh tls cert")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(raw, []byte("{corrupt")) {
		t.Fatal("corrupt file must be overwritten")
	}
}

func TestDecodePairFileRejectsBadSeed(t *testing.T) {
	bad := `{"identity_seed":"` + base64.StdEncoding.EncodeToString([]byte("short")) + `","token":"abc"}`
	if _, _, _, _, err := decodePairFile([]byte(bad)); err == nil {
		t.Fatal("short seed must fail closed")
	}
	empty := `{"identity_seed":"` + base64.StdEncoding.EncodeToString(make([]byte, 32)) + `","token":""}`
	if _, _, _, _, err := decodePairFile([]byte(empty)); err == nil {
		t.Fatal("empty token must fail closed")
	}
}

func TestLoadOrCreateMigratesPreCertFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	path, err := PairFilePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	// Pre-cert shape: seed + token only (old installs).
	seed := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if err := os.WriteFile(path, []byte(`{"identity_seed":"`+seed+`","token":"keep-me"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, tok, _, fp, err := LoadOrCreatePairState(testLogger())
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if tok != "keep-me" {
		t.Fatalf("migration must keep the token (stable code), got %q", tok)
	}
	if fp == "" {
		t.Fatal("migration must mint a tls cert")
	}
	// Second load reuses the minted cert.
	_, _, _, fp2, err := LoadOrCreatePairState(testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if fp2 != fp {
		t.Fatal("migrated cert must be stable")
	}
}

func TestStoreLoadLastDeviceRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	want := LastDevice{Host: "192.168.1.5", Port: 18790, LastSeenUnix: 1700000000, Fingerprint: "abc123"}
	if err := StoreLastDevice(want); err != nil {
		t.Fatalf("store: %v", err)
	}
	path, err := DeviceFilePath()
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("device file mode = %o, want 600", info.Mode().Perm())
	}
	got, ok, err := LoadLastDevice()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !ok {
		t.Fatalf("load ok=false, want true")
	}
	if got.Host != want.Host || got.Port != want.Port || got.Fingerprint != want.Fingerprint {
		t.Fatalf("load = %+v, want %+v", got, want)
	}
	if len(got.CandidateHosts) != 1 || got.CandidateHosts[0] != want.Host {
		t.Fatalf("candidates = %v, want [%s]", got.CandidateHosts, want.Host)
	}
}

func TestLoadLastDeviceAbsentAndCorrupt(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if _, ok, err := LoadLastDevice(); err != nil || ok {
		t.Fatalf("absent = %v,%v, want false,nil", ok, err)
	}
	path, err := DeviceFilePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadLastDevice(); err == nil {
		t.Fatal("corrupt device file must error (caller warns, QR still works)")
	}
	if _, err := decodeLastDevice([]byte(`{"host":"","port":18790}`)); err == nil {
		t.Fatal("empty host must fail closed")
	}
	if _, err := decodeLastDevice([]byte(`{"host":"h","port":0}`)); err == nil {
		t.Fatal("zero port must fail closed")
	}
}

func TestStorePairStateTightensPerms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pair.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := storePairState(path, make([]byte, 32), "tok"); err != nil {
		t.Fatalf("store: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("pair file mode = %o, want 600", fi.Mode().Perm())
	}
}
