// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
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
