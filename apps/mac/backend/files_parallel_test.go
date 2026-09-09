// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"encoding/json"
	"testing"

	"fuseitall/core"
)

func TestShouldEmitProgress(t *testing.T) {
	if !shouldEmitProgress(0, 10) {
		t.Fatal("first chunk must emit")
	}
	if !shouldEmitProgress(9, 10) {
		t.Fatal("last chunk must emit")
	}
	if !shouldEmitProgress(4, 10) {
		t.Fatal("every Nth chunk must emit")
	}
	if shouldEmitProgress(3, 10) {
		t.Fatal("intermediate chunks must coalesce")
	}
	if !shouldEmitProgress(0, 1) {
		t.Fatal("single chunk must emit")
	}
}

func TestNegotiatedChunkSizeLegacyByDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	if got := svc.negotiatedChunkSize(); got != core.LegacyFileChunkRaw {
		t.Fatalf("unknown peer must stay legacy, got %d", got)
	}
	if svc.peerSupportsLargeChunks() {
		t.Fatal("unknown peer must not support large chunks")
	}
}

func TestPeerSupportsLargeChunksGated(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	svc.mu.Lock()
	svc.peerBuild = 10
	svc.peerCapabilities = []string{core.CapabilityFiles, core.CapabilityFilesLargeChunk}
	svc.mu.Unlock()
	if svc.peerSupportsLargeChunks() {
		t.Fatal("build 10 peer must stay legacy even with the capability")
	}
	svc.mu.Lock()
	svc.peerBuild = 11
	svc.mu.Unlock()
	if !svc.peerSupportsLargeChunks() {
		t.Fatal("build 11 peer with the capability must use large chunks")
	}
	if got := svc.negotiatedChunkSize(); got != core.MaxFileChunkRaw {
		t.Fatalf("negotiated size must be 4 MiB, got %d", got)
	}
	svc.mu.Lock()
	svc.peerCapabilities = []string{core.CapabilityFiles}
	svc.mu.Unlock()
	if svc.peerSupportsLargeChunks() {
		t.Fatal("missing capability must stay legacy")
	}
}

func TestShouldEmitDownloadProgress(t *testing.T) {
	chunkEnv := func(idx, total int) core.Envelope {
		raw, err := json.Marshal(map[string]int{"chunk_index": idx, "total_chunks": total})
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		return core.Envelope{Type: core.TypeFileChunk, Payload: json.RawMessage(raw)}
	}
	// Non-chunk file types always emit.
	for _, typ := range []string{core.TypeFileListResp, core.TypeFileAck, core.TypeFileCancel, core.TypeFileStatResp} {
		if !shouldEmitDownloadProgress(core.Envelope{Type: typ}) {
			t.Fatalf("type %s must emit", typ)
		}
	}
	// Chunk rule mirrors shouldEmitProgress: first, every Nth, last emit.
	if !shouldEmitDownloadProgress(chunkEnv(0, 10)) {
		t.Fatal("first download chunk must emit")
	}
	if !shouldEmitDownloadProgress(chunkEnv(9, 10)) {
		t.Fatal("last download chunk must emit")
	}
	if !shouldEmitDownloadProgress(chunkEnv(4, 10)) {
		t.Fatal("every Nth download chunk must emit")
	}
	if shouldEmitDownloadProgress(chunkEnv(3, 10)) {
		t.Fatal("intermediate download chunks must coalesce")
	}
	if !shouldEmitDownloadProgress(chunkEnv(0, 1)) {
		t.Fatal("single download chunk must emit")
	}
	// Fail open on progress: unparseable or shapeless payloads emit.
	if !shouldEmitDownloadProgress(core.Envelope{Type: core.TypeFileChunk, Payload: json.RawMessage(`{`)}) {
		t.Fatal("unparseable chunk payload must emit")
	}
	if !shouldEmitDownloadProgress(core.Envelope{Type: core.TypeFileChunk, Payload: json.RawMessage(`{}`)}) {
		t.Fatal("chunk payload without count must emit")
	}
}

func TestDownloadChunkKBFor(t *testing.T) {
	if got := downloadChunkKBFor(5<<20, 2); got != core.MaxFileChunkRaw>>10 {
		t.Fatalf("4 MiB stride must report 4096, got %d", got)
	}
	if got := downloadChunkKBFor((1<<20)+3, 2); got != core.LegacyFileChunkRaw>>10 {
		t.Fatalf("legacy stride must report 1024, got %d", got)
	}
	if got := downloadChunkKBFor(3, 1); got != core.LegacyFileChunkRaw>>10 {
		t.Fatalf("single chunk must report legacy, got %d", got)
	}
}
