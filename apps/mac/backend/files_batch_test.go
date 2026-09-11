// SPDX-License-Identifier: AGPL-3.0-only

package backend

import (
	"testing"
	"time"
)

func TestBeginUploadBatchValidation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	if _, err := svc.BeginUploadBatch(-1, 0); err == nil {
		t.Fatal("negative file count must fail")
	}
	if _, err := svc.BeginUploadBatch(1, -1); err == nil {
		t.Fatal("negative bytes must fail")
	}
	id, err := svc.BeginUploadBatch(3, 300)
	if err != nil || id == "" {
		t.Fatalf("begin batch: %v", err)
	}
	batches := svc.GetTransferBatches()
	if len(batches) != 1 || batches[0].Status != "running" {
		t.Fatalf("batches = %+v, want one running", batches)
	}
}

func TestBatchProgressDerivesFromMembers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	id, err := svc.BeginUploadBatch(2, 100)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	svc.fileMu.Lock()
	svc.transfers["aaaaaaaaaaaaaaaa"] = &FileTransfer{
		ID: "aaaaaaaaaaaaaaaa", Path: "a.txt", Direction: "upload",
		TotalSize: 60, DoneSize: 30, Status: "running", BatchID: id,
	}
	svc.transfers["bbbbbbbbbbbbbbbb"] = &FileTransfer{
		ID: "bbbbbbbbbbbbbbbb", Path: "b.txt", Direction: "upload",
		TotalSize: 40, DoneSize: 40, Status: "done", BatchID: id,
		completedAt: time.Now(),
	}
	svc.fileMu.Unlock()
	batches := svc.GetTransferBatches()
	if len(batches) != 1 {
		t.Fatalf("batches = %d, want 1", len(batches))
	}
	b := batches[0]
	if b.DoneBytes != 70 || b.Progress != 70 {
		t.Fatalf("batch = %+v, want 70/100 bytes", b)
	}
	if b.DoneFiles != 1 || b.Status != "running" || b.CurrentPath == "" {
		t.Fatalf("batch = %+v, want running with current path", b)
	}
}

func TestCancelUploadBatchCancelsMembers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	id, err := svc.BeginUploadBatch(2, 10)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	svc.fileMu.Lock()
	svc.transfers["aaaaaaaaaaaaaaaa"] = &FileTransfer{
		ID: "aaaaaaaaaaaaaaaa", Path: "a.txt", Direction: "upload",
		Status: "running", BatchID: id,
	}
	svc.transfers["bbbbbbbbbbbbbbbb"] = &FileTransfer{
		ID: "bbbbbbbbbbbbbbbb", Path: "b.txt", Direction: "upload",
		Status: "done", DoneSize: 5, TotalSize: 5, BatchID: id,
		completedAt: time.Now(),
	}
	svc.fileMu.Unlock()
	if _, err := svc.CancelUploadBatch(id); err != nil {
		t.Fatalf("cancel batch: %v", err)
	}
	svc.fileMu.Lock()
	running := svc.transfers["aaaaaaaaaaaaaaaa"].Status
	done := svc.transfers["bbbbbbbbbbbbbbbb"].Status
	svc.fileMu.Unlock()
	if running != "cancelled" {
		t.Fatalf("member status = %q, want cancelled", running)
	}
	if done != "done" {
		t.Fatalf("finished member status = %q, want done", done)
	}
	if _, err := svc.CancelUploadBatch(id); err == nil {
		t.Fatal("second cancel must fail (terminal)")
	}
	if _, err := svc.CancelUploadBatch("ffffffffffffffff"); err == nil {
		t.Fatal("unknown batch must fail")
	}
}

func TestAttachTransferToBatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	bid, _ := svc.BeginUploadBatch(1, 10)
	svc.fileMu.Lock()
	svc.transfers["aaaaaaaaaaaaaaaa"] = &FileTransfer{
		ID: "aaaaaaaaaaaaaaaa", Path: "a.txt", Direction: "upload", Status: "running",
	}
	svc.fileMu.Unlock()
	if _, err := svc.AttachTransferToBatch("aaaaaaaaaaaaaaaa", bid); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if _, err := svc.AttachTransferToBatch("0000000000000000", bid); err == nil {
		t.Fatal("unknown transfer must fail")
	}
	if _, err := svc.AttachTransferToBatch("aaaaaaaaaaaaaaaa", "ffffffffffffffff"); err == nil {
		t.Fatal("unknown batch must fail")
	}
}
