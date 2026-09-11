// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"encoding/base64"
	"testing"
)

func TestSanitizeFilePolicy(t *testing.T) {
	for _, ok := range []string{"", FilePolicyOverwrite, FilePolicyIfNewer} {
		if !SanitizeFilePolicy(ok) {
			t.Fatalf("policy %q must pass", ok)
		}
	}
	for _, bad := range []string{FilePolicySkip, FilePolicyKeepBoth, FilePolicyStop, "OVERWRITE", "merge"} {
		if SanitizeFilePolicy(bad) {
			t.Fatalf("policy %q must fail (sender-side only or unknown)", bad)
		}
	}
}

func TestSanitizeFileChunkPolicy(t *testing.T) {
	base := FileChunkPayload{
		Nonce: "n", TransferID: "abcdef0123456789", Path: "a.txt",
		TotalSize: 3, ChunkIndex: 0, TotalChunks: 1, DataB64: "YWJj",
	}
	if !SanitizeFileChunk(base) {
		t.Fatal("legacy empty policy must pass")
	}
	withOverwrite := base
	withOverwrite.Policy = FilePolicyOverwrite
	if !SanitizeFileChunk(withOverwrite) {
		t.Fatal("overwrite policy must pass")
	}
	withNewer := base
	withNewer.Policy = FilePolicyIfNewer
	withNewer.SourceMtime = 1700000000
	if !SanitizeFileChunk(withNewer) {
		t.Fatal("if_newer with mtime must pass")
	}
	withSkip := base
	withSkip.Policy = FilePolicySkip
	if SanitizeFileChunk(withSkip) {
		t.Fatal("skip must never ride the wire")
	}
	withBadMtime := base
	withBadMtime.Policy = FilePolicyIfNewer
	withBadMtime.SourceMtime = -1
	if SanitizeFileChunk(withBadMtime) {
		t.Fatal("negative source_mtime must fail")
	}
}

func TestSanitizeFileAck(t *testing.T) {
	if !SanitizeFileAck(FileAckPayload{Nonce: "n", TransferID: "abcdef0123456789", OK: true}) {
		t.Fatal("ok ack must pass")
	}
	if !SanitizeFileAck(FileAckPayload{Nonce: "n", TransferID: "abcdef0123456789", Error: "sha mismatch"}) {
		t.Fatal("nack with reason must pass")
	}
	if SanitizeFileAck(FileAckPayload{TransferID: "abcdef0123456789"}) {
		t.Fatal("missing nonce must fail")
	}
	if SanitizeFileAck(FileAckPayload{Nonce: "n", TransferID: "short"}) {
		t.Fatal("bad transfer id must fail")
	}
	if FeaturePath(TypeFileAck) != "/files" {
		t.Fatalf("file-ack must ride /files, got %q", FeaturePath(TypeFileAck))
	}
}

func TestSanitizeFileCancel(t *testing.T) {
	if !SanitizeFileCancel(FileCancelPayload{Nonce: "n", TransferID: "abcdef0123456789", Path: "a.txt"}) {
		t.Fatal("cancel with path hint must pass")
	}
	if !SanitizeFileCancel(FileCancelPayload{Nonce: "n", TransferID: "abcdef0123456789"}) {
		t.Fatal("cancel without path must pass (locator is best-effort)")
	}
	if SanitizeFileCancel(FileCancelPayload{TransferID: "abcdef0123456789"}) {
		t.Fatal("missing nonce must fail")
	}
	if SanitizeFileCancel(FileCancelPayload{Nonce: "n", TransferID: "short"}) {
		t.Fatal("bad transfer id must fail")
	}
	if SanitizeFileCancel(FileCancelPayload{Nonce: "n", TransferID: "abcdef0123456789", Path: "../evil"}) {
		t.Fatal("traversal path must fail")
	}
	if FeaturePath(TypeFileCancel) != "/files" {
		t.Fatalf("file-cancel must ride /files, got %q", FeaturePath(TypeFileCancel))
	}
}

func TestIsSourceNewer(t *testing.T) {
	if !IsSourceNewer(200, 100, 10, 10) {
		t.Fatal("newer mtime must win")
	}
	if IsSourceNewer(100, 200, 10, 10) {
		t.Fatal("older mtime must lose")
	}
	if IsSourceNewer(100, 100, 10, 10) {
		t.Fatal("equal mtime+size must count as same (skip)")
	}
	if !IsSourceNewer(100, 100, 11, 10) {
		t.Fatal("equal mtime with different size must count as newer")
	}
}

func TestKeepBothName(t *testing.T) {
	existing := map[string]struct{}{"photo.png": {}, "photo (2).png": {}}
	if got := KeepBothName("other.png", existing); got != "other.png" {
		t.Fatalf("free name must pass through, got %q", got)
	}
	if got := KeepBothName("photo.png", existing); got != "photo (3).png" {
		t.Fatalf("collision must number up, got %q", got)
	}
	dirExisting := map[string]struct{}{"docs/note": {}}
	if got := KeepBothName("docs/note", dirExisting); got != "docs/note (2)" {
		t.Fatalf("extensionless collision failed, got %q", got)
	}
}

func TestFileChunkSizeForPeer(t *testing.T) {
	if got := FileChunkSizeForPeer(true); got != MaxFileChunkRaw {
		t.Fatalf("large-capable peer must get 4 MiB, got %d", got)
	}
	if got := FileChunkSizeForPeer(false); got != LegacyFileChunkRaw {
		t.Fatalf("legacy peer must get 1 MiB, got %d", got)
	}
	if MaxFileChunkRaw != 4<<20 {
		t.Fatalf("max chunk must be 4 MiB, got %d", MaxFileChunkRaw)
	}
}

func TestTotalChunksForSize(t *testing.T) {
	if got := TotalChunksForSize(0, MaxFileChunkRaw); got != 1 {
		t.Fatalf("empty file must be 1 chunk, got %d", got)
	}
	if got := TotalChunksForSize(int64(LegacyFileChunkRaw), LegacyFileChunkRaw); got != 1 {
		t.Fatalf("1 MiB at legacy stride must be 1 chunk, got %d", got)
	}
	if got := TotalChunksForSize(5<<20, MaxFileChunkRaw); got != 2 {
		t.Fatalf("5 MiB at 4 MiB stride must be 2 chunks, got %d", got)
	}
	if got := TotalChunksForSize(5<<20, LegacyFileChunkRaw); got != 5 {
		t.Fatalf("5 MiB at legacy stride must be 5 chunks, got %d", got)
	}
}

func TestSanitizeFileChunkStrides(t *testing.T) {
	legacyChunk := make([]byte, LegacyFileChunkRaw)
	for i := range legacyChunk {
		legacyChunk[i] = byte(i)
	}
	// Legacy 1 MiB two-chunk transfer: first chunk full stride.
	first := FileChunkPayload{
		Nonce: "n", TransferID: "abcdef0123456789", Path: "a.bin",
		TotalSize: int64(LegacyFileChunkRaw) + 3, ChunkIndex: 0, TotalChunks: 2,
		Offset: 0, DataB64: base64.StdEncoding.EncodeToString(legacyChunk),
	}
	if !SanitizeFileChunk(first) {
		t.Fatal("legacy 1 MiB first chunk must pass")
	}
	// 4 MiB stride two-chunk transfer (5 MiB total).
	bigChunk := make([]byte, MaxFileChunkRaw)
	for i := range bigChunk {
		bigChunk[i] = byte(i >> 8)
	}
	bigFirst := FileChunkPayload{
		Nonce: "n", TransferID: "abcdef0123456789", Path: "b.bin",
		TotalSize: (5 << 20), ChunkIndex: 0, TotalChunks: 2,
		Offset: 0, DataB64: base64.StdEncoding.EncodeToString(bigChunk),
	}
	if !SanitizeFileChunk(bigFirst) {
		t.Fatal("4 MiB first chunk must pass")
	}
	// Mismatched stride: legacy-sized body claiming the 4 MiB chunk count.
	mismatched := first
	mismatched.TotalSize = 5 << 20
	mismatched.TotalChunks = 2
	if SanitizeFileChunk(mismatched) {
		t.Fatal("1 MiB body under a 4 MiB-2-chunk shape must fail")
	}
	// Unknown chunk count matches neither stride.
	badCount := first
	badCount.TotalChunks = 7
	if SanitizeFileChunk(badCount) {
		t.Fatal("chunk count matching neither stride must fail")
	}
	// Single decode path must agree with the validating path.
	raw, ok := DecodeFileChunkData(first.DataB64)
	if !ok || len(raw) != LegacyFileChunkRaw {
		t.Fatal("DecodeFileChunkData must round-trip the legacy chunk once")
	}
}
