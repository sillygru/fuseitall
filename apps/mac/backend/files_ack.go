// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"fuseitall/core"
)

// fileAckTimeout bounds delivery confirmation. Hashing a multi-GB part file
// on the phone takes a while; the transfer shows running at 100% meanwhile.
// Peers without the files-ack capability never wait at all (legacy path).
const fileAckTimeout = 5 * time.Minute

// peerSupportsFileAck reports whether the connected phone confirms uploads
// via file-ack. Absence (old builds, unknown peer) means legacy
// fire-and-forget: done after the last chunk send, no waiting.
func (s *Service) peerSupportsFileAck() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return core.IsCapabilitySupported(s.peerCapabilities, core.CapabilityFilesAck)
}

// registerAckWaiter creates the ack channel for a transfer about to send its
// final chunk. Register-before-send closes the race where the phone's ack
// beats the waiter setup. The returned wait blocks until the ack arrives or
// the timeout elapses, then releases the channel.
func (s *Service) registerAckWaiter(transferID string) func(time.Duration) error {
	s.fileMu.Lock()
	if s.transferWaiters == nil {
		s.transferWaiters = make(map[string]chan error)
	}
	ch := make(chan error, 1)
	s.transferWaiters[transferID] = ch
	s.fileMu.Unlock()
	return func(timeout time.Duration) error {
		defer func() {
			s.fileMu.Lock()
			delete(s.transferWaiters, transferID)
			s.fileMu.Unlock()
		}()
		select {
		case err := <-ch:
			return err
		case <-time.After(timeout):
			return errors.New("phone did not confirm delivery")
		}
	}
}

// ParseFileAck decodes and validates a file-ack payload.
func ParseFileAck(body []byte) (core.FileAckPayload, bool) {
	var env struct {
		Type    string             `json:"type"`
		Payload core.FileAckPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return core.FileAckPayload{}, false
	}
	if env.Type != core.TypeFileAck {
		return core.FileAckPayload{}, false
	}
	if !core.SanitizeFileAck(env.Payload) {
		return core.FileAckPayload{}, false
	}
	return env.Payload, true
}

// ingestFileAckBody folds a delivery confirmation into transfer state. Acks
// with no waiter (late, legacy, or unknown transfer) still flip the record
// so the progress UI converges. Never throws: malformed acks are dropped
// after the token gate already authenticated the sender.
func (s *Service) ingestFileAckBody(body []byte) {
	p, ok := ParseFileAck(body)
	if !ok {
		return
	}
	s.fileMu.Lock()
	if ch, ok := s.transferWaiters[p.TransferID]; ok {
		select {
		case ch <- ackError(p):
		default:
		}
	}
	if tr, ok := s.transfers[p.TransferID]; ok {
		if p.OK {
			if tr.Status == "running" {
				tr.Status = "done"
				tr.DoneSize = tr.TotalSize
				tr.completedAt = time.Now()
			}
		} else if tr.Status == "running" {
			tr.Status = "error"
			tr.Error = ackReason(p)
			tr.completedAt = time.Now()
		}
	}
	s.fileMu.Unlock()
	s.appendLine(fmt.Sprintf("file ack transfer=%s ok=%v", p.TransferID, p.OK))
	s.emitTransfersChanged()
}

func ackError(p core.FileAckPayload) error {
	if p.OK {
		return nil
	}
	return errors.New(ackReason(p))
}

func ackReason(p core.FileAckPayload) string {
	if p.Error != "" {
		return p.Error
	}
	return "phone rejected file"
}

// ParseFileCancel decodes and validates a cancel payload.
func ParseFileCancel(body []byte) (core.FileCancelPayload, bool) {
	var env struct {
		Type    string               `json:"type"`
		Payload core.FileCancelPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return core.FileCancelPayload{}, false
	}
	if env.Type != core.TypeFileCancel {
		return core.FileCancelPayload{}, false
	}
	if !core.SanitizeFileCancel(env.Payload) {
		return core.FileCancelPayload{}, false
	}
	return env.Payload, true
}

// ingestFileCancelBody drops a phone-initiated abort: the local download
// with this transfer id (if any) stops and its staged part is discarded.
// Unknown ids are ignored; the sender already gave up on them.
func (s *Service) ingestFileCancelBody(body []byte) {
	p, ok := ParseFileCancel(body)
	if !ok {
		return
	}
	s.fileMu.Lock()
	if tr, ok := s.transfers[p.TransferID]; ok && tr.Status == "running" {
		tr.Status = "cancelled"
		tr.completedAt = time.Now()
		if tr.file != nil {
			_ = tr.file.Close()
			tr.file = nil
		}
		removePartFile(tr, p.TransferID)
	}
	if ch, ok := s.transferWaiters[p.TransferID]; ok {
		select {
		case ch <- errors.New("transfer cancelled"):
		default:
		}
	}
	s.fileMu.Unlock()
	s.appendLine("file cancel received transfer=" + p.TransferID)
	s.emitTransfersChanged()
}

// removePartFile best-effort deletes a download's staged part file.
// Callers must hold fileMu; it never blocks on network, only disk.
func removePartFile(tr *FileTransfer, transferID string) {
	if tr == nil || tr.Direction != "download" || tr.tmpPath == "" {
		return
	}
	_ = os.Remove(tr.tmpPath + ".part." + transferID)
}
