// SPDX-License-Identifier: AGPL-3.0-only

package backend

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestRunUploadTasksEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	if err := svc.runUploadTasks(nil, ""); err != nil {
		t.Fatalf("empty tasks must succeed, got %v", err)
	}
}

func TestCompleteUploadSendNil(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	if err := svc.completeUploadSend(nil); err == nil {
		t.Fatal("nil send result must fail closed")
	}
}

func TestMaxParallelFileUploadsRaised(t *testing.T) {
	if maxParallelFileUploads < 8 {
		t.Fatalf("parallelism must cover song-folder bursts, got %d", maxParallelFileUploads)
	}
}

func TestParentRemoteDir(t *testing.T) {
	if got := parentRemoteDir("a/b/c.mp3"); got != "a/b" {
		t.Fatalf("parent of nested path = %q, want a/b", got)
	}
	if got := parentRemoteDir("song.mp3"); got != "" {
		t.Fatalf("top-level parent = %q, want empty", got)
	}
	if got := parentRemoteDir(""); got != "" {
		t.Fatalf("empty parent = %q, want empty", got)
	}
}

func TestMkdirGateMarkWait(t *testing.T) {
	g := newMkdirGate()
	g.markSent("songs")
	done := make(chan struct{})
	go func() { g.wait("songs"); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("marked gate must release waiters")
	}
	// Idempotent re-mark must not panic.
	g.markSent("songs")
	// Nil and empty gates never block.
	var nilGate *mkdirGate
	nilGate.wait("songs")
	g.wait("")
}

func TestMkdirGateWaitBlocksUntilMarked(t *testing.T) {
	g := newMkdirGate()
	done := make(chan struct{})
	go func() { g.wait("late"); close(done) }()
	select {
	case <-done:
		t.Fatal("unmarked gate must block")
	case <-time.After(50 * time.Millisecond):
	}
	g.markSent("late")
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("late mark must release waiters")
	}
}

func TestRunUploadStreamEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	ch := make(chan uploadTask)
	close(ch)
	if err := svc.runUploadStream(ch, ""); err != nil {
		t.Fatalf("empty stream must succeed, got %v", err)
	}
}

func TestRunUploadStreamCancelledSkips(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	batchID, err := svc.BeginUploadBatch(1, 0)
	if err != nil {
		t.Fatalf("begin batch: %v", err)
	}
	if _, err := svc.CancelUploadBatch(batchID); err != nil {
		t.Fatalf("cancel batch: %v", err)
	}
	ch := make(chan uploadTask, 1)
	ch <- uploadTask{localPath: "/tmp/never", remotePath: "never", policy: ""}
	close(ch)
	if err := svc.runUploadStream(ch, batchID); err == nil {
		t.Fatal("cancelled stream must fail closed")
	}
	// No transfer may have been registered for the skipped task.
	svc.fileMu.Lock()
	defer svc.fileMu.Unlock()
	for _, tr := range svc.transfers {
		if tr.Path == "never" {
			t.Fatal("cancelled task must not register a transfer")
		}
	}
}

func TestSendUploadChunksDetectsChangedFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	dir := t.TempDir()
	f := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(f, []byte("12345678"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	stale, err := os.Stat(f)
	if err != nil {
		t.Fatalf("stat seed: %v", err)
	}
	// Grow the file after the walk statted it: the sender must refuse to
	// splice the new bytes under the old size.
	if err := os.WriteFile(f, []byte("1234567890123456"), 0o600); err != nil {
		t.Fatalf("grow file: %v", err)
	}
	task := uploadTask{localPath: f, remotePath: "song.mp3", policy: "", info: stale}
	if _, err := svc.sendUploadChunks(task, ""); err == nil {
		t.Fatal("changed file must fail closed")
	} else if !strings.Contains(strings.ToLower(err.Error()), "changed") {
		t.Fatalf("changed file must say so, got %v", err)
	}
}
