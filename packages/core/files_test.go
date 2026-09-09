// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package core

import (
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
