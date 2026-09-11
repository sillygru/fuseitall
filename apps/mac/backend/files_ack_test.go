// SPDX-License-Identifier: AGPL-3.0-only

package backend

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"fuseitall/core"
)

func ackBody(t *testing.T, transferID string, ok bool, errMsg string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"type": "file-ack",
		"payload": map[string]any{
			"nonce": "n1", "transfer_id": transferID, "ok": ok, "error": errMsg,
		},
	})
	if err != nil {
		t.Fatalf("marshal ack: %v", err)
	}
	return raw
}

func TestIngestFileAckFlipsRecord(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	id := "abcdef0123456789"
	svc.transfers = map[string]*FileTransfer{
		id: {ID: id, Path: "a.txt", Direction: "upload", Status: "running", TotalSize: 3, DoneSize: 3},
	}
	svc.ingestFileAckBody(ackBody(t, id, true, ""))
	tr := svc.transfers[id]
	if tr.Status != "done" {
		t.Fatalf("status = %q, want done after ok ack", tr.Status)
	}

	id2 := "1234567890abcdef"
	svc.transfers[id2] = &FileTransfer{ID: id2, Path: "b.txt", Direction: "upload", Status: "running", TotalSize: 3}
	svc.ingestFileAckBody(ackBody(t, id2, false, "sha mismatch"))
	tr2 := svc.transfers[id2]
	if tr2.Status != "error" || tr2.Error != "sha mismatch" {
		t.Fatalf("nack = %+v, want error with reason", tr2)
	}
}

func TestIngestFileAckSignalsWaiter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	id := "abcdef0123456789"
	wait := svc.registerAckWaiter(id)
	done := make(chan error, 1)
	go func() { done <- wait(5 * time.Second) }()
	svc.ingestFileAckBody(ackBody(t, id, true, ""))
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("wait = %v, want nil on ok ack", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiter not signaled by ok ack")
	}
}

func TestIngestFileAckNackSignalsError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	id := "abcdef0123456789"
	wait := svc.registerAckWaiter(id)
	done := make(chan error, 1)
	go func() { done <- wait(5 * time.Second) }()
	svc.ingestFileAckBody(ackBody(t, id, false, "sha mismatch"))
	select {
	case err := <-done:
		if err == nil || err.Error() != "sha mismatch" {
			t.Fatalf("wait = %v, want sha mismatch", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiter not signaled by nack")
	}
}

func TestWaitFileAckTimeout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	wait := svc.registerAckWaiter("abcdef0123456789")
	if err := wait(20 * time.Millisecond); err == nil {
		t.Fatal("missing ack must time out")
	}
}

func TestIngestFileAckMalformedIgnored(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	svc.transfers = map[string]*FileTransfer{
		"abcdef0123456789": {ID: "abcdef0123456789", Status: "running"},
	}
	svc.ingestFileAckBody([]byte(`{"type":"file-ack","payload":{"nonce":"n"}}`))
	if svc.transfers["abcdef0123456789"].Status != "running" {
		t.Fatal("malformed ack must not flip records")
	}
}

func cancelBody(t *testing.T, transferID, path string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"type": "file-cancel",
		"payload": map[string]any{
			"nonce": "n1", "transfer_id": transferID, "path": path,
		},
	})
	if err != nil {
		t.Fatalf("marshal cancel: %v", err)
	}
	return raw
}

func TestIngestFileCancelDropsTransferAndPart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	dir := t.TempDir()
	base := dir + "/f.part"
	if err := os.WriteFile(base+".part.abcdef0123456789", []byte("half"), 0o600); err != nil {
		t.Fatalf("seed part: %v", err)
	}
	id := "abcdef0123456789"
	svc.transfers = map[string]*FileTransfer{
		id: {ID: id, Path: "f.part", Direction: "download", Status: "running", tmpPath: base},
	}
	svc.ingestFileCancelBody(cancelBody(t, id, "f.part"))
	tr := svc.transfers[id]
	if tr.Status != "cancelled" {
		t.Fatalf("status = %q, want cancelled", tr.Status)
	}
	if _, err := os.Stat(base + ".part." + id); !os.IsNotExist(err) {
		t.Fatal("staged part must be removed on cancel")
	}
}

func TestIngestFileCancelUnknownIgnored(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	svc.transfers = map[string]*FileTransfer{}
	svc.ingestFileCancelBody(cancelBody(t, "abcdef0123456789", ""))
	svc.ingestFileCancelBody([]byte(`{"type":"file-cancel","payload":{"nonce":"n"}}`))
}

func TestFailTransferRemovesPart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	dir := t.TempDir()
	base := dir + "/g.bin"
	id := "1234567890abcdef"
	if err := os.WriteFile(base+".part."+id, []byte("half"), 0o600); err != nil {
		t.Fatalf("seed part: %v", err)
	}
	svc.transfers = map[string]*FileTransfer{
		id: {ID: id, Path: "g.bin", Direction: "download", Status: "running", tmpPath: base},
	}
	svc.failTransfer(id, "boom")
	if _, err := os.Stat(base + ".part." + id); !os.IsNotExist(err) {
		t.Fatal("staged part must be removed on failure")
	}
}

func TestSweepStagedParts(t *testing.T) {
	staging := t.TempDir()
	t.Setenv("TMPDIR", staging)
	base := staging + "/fuseitall-files"
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatalf("mkdir staging: %v", err)
	}
	if err := os.WriteFile(base+"/a.part.abcdef", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base+"/keep.txt", []byte("user file"), 0o600); err != nil {
		t.Fatal(err)
	}
	sweepStagedParts()
	if _, err := os.Stat(base + "/a.part.abcdef"); !os.IsNotExist(err) {
		t.Fatal("orphan part must be swept")
	}
	if _, err := os.Stat(base + "/keep.txt"); err != nil {
		t.Fatal("user files must survive the sweep")
	}
}

func TestPeerSupportsFileAck(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	if svc.peerSupportsFileAck() {
		t.Fatal("unknown peer must mean legacy (no wait)")
	}
	svc.learnPeer("android", 10, "0.10.0", []string{"ping", "files"})
	if svc.peerSupportsFileAck() {
		t.Fatal("peer without files-ack cap must mean legacy")
	}
	svc.learnPeer("android", 10, "0.10.0", []string{"ping", "files", core.CapabilityFilesAck})
	if !svc.peerSupportsFileAck() {
		t.Fatal("peer with files-ack cap must be supported")
	}
}
