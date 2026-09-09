// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"errors"
	"time"

	"fuseitall/core"
)

// TransferBatch groups one user drop (one or many files) so the UI can show
// current-file + total progress and cancel the whole drop at once. Batches
// are Mac-local only: transfer_ids still ride the wire unchanged, so no
// protocol or peer update is involved. Counters are derived live from the
// member transfers (same fileMu), so per-chunk updates cannot drift.
type TransferBatch struct {
	ID         string
	TotalFiles int
	TotalBytes int64
	Status     string // running | done | error | cancelled
	CreatedAt  time.Time
	// completedAt marks the last member finishing; the batch is pruned
	// uploadBatchWindow after it so recent drops stay visible.
	completedAt time.Time
}

// TransferBatchView is the Wails-bound batch row.
type TransferBatchView struct {
	ID          string `json:"id"`
	TotalFiles  int    `json:"total_files"`
	DoneFiles   int    `json:"done_files"`
	TotalBytes  int64  `json:"total_bytes"`
	DoneBytes   int64  `json:"done_bytes"`
	CurrentPath string `json:"current_path"`
	Status      string `json:"status"`
	Progress    int    `json:"progress"` // 0..100 by bytes
}

const uploadBatchWindow = 60 * time.Second

// BeginUploadBatch starts a batch for one user drop. totalFiles/totalBytes
// are the frontend's best-effort estimates (bytes may be 0 when unknown);
// per-file reality still comes from the member transfers.
func (s *Service) BeginUploadBatch(totalFiles int, totalBytes int64) (string, error) {
	if totalFiles < 0 || totalFiles > 10000 {
		return "", errors.New("invalid file count")
	}
	if totalBytes < 0 || totalBytes > 1<<50 {
		return "", errors.New("invalid byte count")
	}
	if totalFiles == 0 {
		totalFiles = 1
	}
	id, err := freshTransferID()
	if err != nil {
		return "", err
	}
	s.fileMu.Lock()
	if s.batches == nil {
		s.batches = make(map[string]*TransferBatch)
	}
	if s.transfers == nil {
		s.transfers = make(map[string]*FileTransfer)
	}
	s.batches[id] = &TransferBatch{
		ID: id, TotalFiles: totalFiles, TotalBytes: totalBytes,
		Status: "running", CreatedAt: time.Now(),
	}
	s.fileMu.Unlock()
	s.emitBatchesChanged()
	return id, nil
}

// AttachTransferToBatch links one transfer to a batch. Idempotent and
// late-safe: counters derive from transfers, so attaching after the first
// chunk still converges. Unknown ids fail closed.
func (s *Service) AttachTransferToBatch(transferID, batchID string) (string, error) {
	s.fileMu.Lock()
	tr, ok := s.transfers[transferID]
	b, bok := s.batches[batchID]
	if !ok || tr == nil {
		s.fileMu.Unlock()
		return "", errors.New("unknown transfer")
	}
	if !bok || b == nil {
		s.fileMu.Unlock()
		return "", errors.New("unknown upload batch")
	}
	if b.Status != "running" {
		s.fileMu.Unlock()
		return "", errors.New("upload batch not running")
	}
	tr.BatchID = batchID
	s.fileMu.Unlock()
	s.emitBatchesChanged()
	return "Attached.", nil
}

// GetTransferBatches returns live batch rows with derived counters. Member
// transfers older than the batch window are pruned with the batch so a
// finished drop keeps its rows visible for a minute, not 5 seconds.
func (s *Service) GetTransferBatches() []TransferBatchView {
	s.fileMu.Lock()
	if s.batches == nil {
		s.fileMu.Unlock()
		return []TransferBatchView{}
	}
	now := time.Now()
	for id, b := range s.batches {
		if b == nil {
			delete(s.batches, id)
			continue
		}
		running := false
		for _, tr := range s.transfers {
			if tr != nil && tr.BatchID == id && tr.Status == "running" {
				running = true
				break
			}
		}
		if !running && !b.completedAt.IsZero() && now.Sub(b.completedAt) > uploadBatchWindow {
			delete(s.batches, id)
			for tid, tr := range s.transfers {
				if tr != nil && tr.BatchID == id {
					removePartFile(tr, tid)
					delete(s.transfers, tid)
				}
			}
		} else if !running && b.completedAt.IsZero() && b.Status == "running" {
			// No running members but never stamped (e.g. all pruned early):
			// stamp now so the window starts.
			b.completedAt = now
		}
	}
	out := make([]TransferBatchView, 0, len(s.batches))
	for _, b := range s.batches {
		if b == nil {
			continue
		}
		var doneFiles, totalMembers int
		var doneBytes int64
		current := ""
		var currentAt time.Time
		status := b.Status
		anyRunning, anyError, anyCancelled, allDone := false, false, false, true
		for _, tr := range s.transfers {
			if tr == nil || tr.BatchID != b.ID {
				continue
			}
			totalMembers++
			doneBytes += tr.DoneSize
			if tr.Status == "running" {
				anyRunning = true
				allDone = false
			} else if tr.Status == "error" {
				anyError = true
				allDone = false
			} else if tr.Status == "cancelled" {
				anyCancelled = true
				allDone = false
			} else if tr.Status == "done" {
				doneFiles++
			} else {
				allDone = false
			}
			if !tr.completedAt.IsZero() && tr.completedAt.After(currentAt) {
				currentAt = tr.completedAt
				current = tr.Path
			} else if tr.Status == "running" && current == "" {
				current = tr.Path
			}
		}
		// Prefer a running member as current.
		for _, tr := range s.transfers {
			if tr != nil && tr.BatchID == b.ID && tr.Status == "running" {
				current = tr.Path
				break
			}
		}
		switch {
		case anyRunning:
			status = "running"
		case anyCancelled && !anyError:
			status = "cancelled"
		case anyError:
			status = "error"
		case totalMembers > 0 && allDone:
			status = "done"
		}
		prog := 0
		if b.TotalBytes > 0 {
			prog = int(doneBytes * 100 / b.TotalBytes)
			if prog < 0 {
				prog = 0
			}
			if prog > 100 {
				prog = 100
			}
		} else if status == "done" {
			prog = 100
		}
		out = append(out, TransferBatchView{
			ID: b.ID, TotalFiles: b.TotalFiles, DoneFiles: doneFiles,
			TotalBytes: b.TotalBytes, DoneBytes: doneBytes,
			CurrentPath: current, Status: status, Progress: prog,
		})
	}
	s.fileMu.Unlock()
	return out
}

// CancelUploadBatch cancels every running member of a batch and marks the
// batch cancelled. Staged phone bytes are discarded per member via
// file-cancel (best-effort, after the lock). Never resumable: batch cancel
// is user intent.
func (s *Service) CancelUploadBatch(batchID string) (string, error) {
	s.fileMu.Lock()
	b, ok := s.batches[batchID]
	if !ok || b == nil {
		s.fileMu.Unlock()
		return "", errors.New("unknown upload batch")
	}
	if b.Status != "running" {
		// Still cancel stray running members, but report terminal state.
		s.fileMu.Unlock()
		return "", errors.New("upload batch not running")
	}
	b.Status = "cancelled"
	b.completedAt = time.Now()
	type cancelReq struct {
		transferID, path string
	}
	var reqs []cancelReq
	for id, tr := range s.transfers {
		if tr == nil || tr.BatchID != batchID || tr.Status != "running" {
			continue
		}
		tr.Status = "cancelled"
		tr.completedAt = time.Now()
		tr.Resumable = false
		if tr.file != nil {
			_ = tr.file.Close()
			tr.file = nil
		}
		removePartFile(tr, id)
		delete(s.uploadSessions, id)
		delete(s.browserUploads, id)
		if ch, ok := s.transferWaiters[id]; ok {
			select {
			case ch <- errors.New("transfer cancelled"):
			default:
			}
		}
		reqs = append(reqs, cancelReq{transferID: id, path: tr.Path})
	}
	s.fileMu.Unlock()
	for _, r := range reqs {
		payload := core.FileCancelPayload{TransferID: r.transferID, Path: r.path}
		_ = s.sendFeatureToPhone(core.TypeFileCancel, &payload)
	}
	s.emitTransfersChanged()
	s.emitBatchesChanged()
	return "Cancelled.", nil
}

// markBatchMemberDone stamps batch completion when no members run anymore.
// Callers must hold no locks; it emits only when the batch just finished.
func (s *Service) refreshBatchCompletion(batchID string) {
	if batchID == "" {
		return
	}
	s.fileMu.Lock()
	b, ok := s.batches[batchID]
	if !ok || b == nil || b.Status != "running" {
		s.fileMu.Unlock()
		return
	}
	for _, tr := range s.transfers {
		if tr != nil && tr.BatchID == batchID && tr.Status == "running" {
			s.fileMu.Unlock()
			return
		}
	}
	b.completedAt = time.Now()
	s.fileMu.Unlock()
	s.emitBatchesChanged()
}

func (s *Service) emitBatchesChanged() {
	emitWailsEvent("batches:changed", s.GetTransferBatches())
}
