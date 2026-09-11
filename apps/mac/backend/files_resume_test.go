// SPDX-License-Identifier: AGPL-3.0-only

package backend

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fuseitall/core"
)

func statRespBody(t *testing.T, transferID string, next, total int, errMsg string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"type": "file-stat-resp",
		"payload": map[string]any{
			"nonce": "n1", "transfer_id": transferID,
			"next_chunk": next, "total_chunks": total, "error": errMsg,
		},
	})
	if err != nil {
		t.Fatalf("marshal stat-resp: %v", err)
	}
	return raw
}

func TestParseFileStatResp(t *testing.T) {
	p, ok := ParseFileStatResp(statRespBody(t, "abcdef0123456789", 3, 9, ""))
	if !ok || p.NextChunk != 3 || p.TotalChunks != 9 {
		t.Fatalf("parse = %+v %v, want next 3 of 9", p, ok)
	}
	if _, ok := ParseFileStatResp([]byte(`{"type":"file-stat-resp","payload":{"nonce":"n"}}`)); ok {
		t.Fatal("missing transfer must fail")
	}
	if _, ok := ParseFileStatResp(statRespBody(t, "abcdef0123456789", 10, 9, "")); ok {
		t.Fatal("next past total must fail")
	}
}

func TestIngestFileStatRespSignalsWaiter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	id := "abcdef0123456789"
	svc.fileMu.Lock()
	svc.statWaiters = map[string]chan FileStatResult{id: make(chan FileStatResult, 1)}
	svc.fileMu.Unlock()
	svc.ingestFileStatRespBody(statRespBody(t, id, 4, 9, ""))
	select {
	case r := <-svc.statWaiters[id]:
		if r.NextChunk != 4 || r.TotalChunks != 9 || r.StatErr != "" {
			t.Fatalf("stat result = %+v, want next 4 of 9", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stat waiter not signaled")
	}
}

func TestQueryUploadStatOfflineRestarts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	if got := svc.queryUploadStat("abcdef0123456789"); got != 0 {
		t.Fatalf("offline stat = %d, want 0 (restart)", got)
	}
}

func TestResumeUploadUnknownAndChanged(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	if _, err := svc.ResumeUpload("abcdef0123456789"); err == nil {
		t.Fatal("unknown session must fail")
	}
	dir := t.TempDir()
	fp := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(fp, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(fp)
	svc.uploadSessions = map[string]*NativeUploadSession{
		"abcdef0123456789": {TransferID: "abcdef0123456789", LocalPath: fp, RemotePath: "a.bin", TotalSize: 999, TotalChunks: 1},
	}
	_ = info
	if _, err := svc.ResumeUpload("abcdef0123456789"); err == nil {
		t.Fatal("changed file must fail closed")
	}
	if _, ok := svc.uploadSessions["abcdef0123456789"]; ok {
		t.Fatal("changed file must drop the session")
	}
}

func TestSweepUploadSessions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	svc.uploadSessions = map[string]*NativeUploadSession{
		"old": {TransferID: "old", CreatedAt: time.Now().Add(-time.Hour)},
		"new": {TransferID: "new", CreatedAt: time.Now()},
	}
	svc.transfers = map[string]*FileTransfer{
		"old": {ID: "old", Status: "error", Resumable: true},
		"new": {ID: "new", Status: "error", Resumable: true},
	}
	svc.sweepUploadSessions(time.Now())
	if _, ok := svc.uploadSessions["old"]; ok {
		t.Fatal("expired session must be swept")
	}
	if _, ok := svc.uploadSessions["new"]; !ok {
		t.Fatal("fresh session must survive")
	}
	if svc.transfers["old"].Resumable {
		t.Fatal("swept session must clear the retry flag")
	}
}

func TestRehashBrowserChunkComposes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	id := "abcdef0123456789"
	zeros := make([]byte, core.LegacyFileChunkRaw)
	one := []byte("x")
	svc.browserUploads = map[string]*BrowserUploadSession{
		id: {ID: id, RemotePath: "f.bin", TotalSize: int64(len(zeros) + 1), TotalChunks: 2, Hash: sha256.New()},
	}
	if err := svc.RehashBrowserChunk(id, base64.StdEncoding.EncodeToString(zeros)); err != nil {
		t.Fatalf("rehash chunk 0: %v", err)
	}
	if err := svc.RehashBrowserChunk(id, base64.StdEncoding.EncodeToString(one)); err != nil {
		t.Fatalf("rehash chunk 1: %v", err)
	}
	want := sha256.Sum256(append(zeros, one...))
	got := svc.browserUploads[id].Hash.Sum(nil)
	if hex.EncodeToString(got) != hex.EncodeToString(want[:]) {
		t.Fatal("replayed hash must equal whole-file sha256")
	}
	if err := svc.RehashBrowserChunk("0123456789abcdef", "eA=="); err == nil {
		t.Fatal("unknown session must fail")
	}
}

func TestDecodeBrowserSlice(t *testing.T) {
	sess := &BrowserUploadSession{TotalSize: int64(core.LegacyFileChunkRaw) + 1, TotalChunks: 2}
	zeros := make([]byte, core.LegacyFileChunkRaw)
	if _, err := decodeBrowserSlice(base64.StdEncoding.EncodeToString(zeros), sess, 0); err != nil {
		t.Fatalf("full first slice must pass: %v", err)
	}
	if _, err := decodeBrowserSlice("eA==", sess, 1); err != nil {
		t.Fatalf("1-byte tail must pass: %v", err)
	}
	if _, err := decodeBrowserSlice("eA==", sess, 0); err == nil {
		t.Fatal("short first slice must fail")
	}
	if _, err := decodeBrowserSlice("!!!", sess, 0); err == nil {
		t.Fatal("bad base64 must fail")
	}
	if _, err := decodeBrowserSlice("eA==", sess, 5); err == nil {
		t.Fatal("past-end index must fail")
	}
}
