// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"fuseitall/core"
)

// uploadResumeWindow bounds how long a failed upload stays retryable. Past
// it the session is swept and Retry restarts from zero with a new transfer.
const uploadResumeWindow = 30 * time.Minute

// fileStatTimeout bounds one offset query. Old peers never answer; the
// timeout then means restart-from-zero with the same transfer id, which old
// receivers accept as an ordinary upload.
const fileStatTimeout = 15 * time.Second

// NativeUploadSession retains a failed native upload for retry: the local
// file is still on disk, so resume re-reads it and sends only the missing
// tail reported by the phone. ChunkSize is the negotiated stride for the
// transfer (4 MiB for new peers, legacy 1 MiB otherwise) so resume offsets
// agree with the original send. Guarded by fileMu.
type NativeUploadSession struct {
	TransferID  string
	LocalPath   string
	RemotePath  string
	TotalSize   int64
	TotalChunks int
	ChunkSize   int
	Policy      string
	SourceMtime int64
	CreatedAt   time.Time
}

// FileStatResult carries one offset answer to a waiter.
type FileStatResult struct {
	NextChunk   int
	TotalChunks int
	StatErr     string
}

// ParseFileStatResp decodes and validates a stat response.
func ParseFileStatResp(body []byte) (core.FileStatRespPayload, bool) {
	var env struct {
		Type    string                  `json:"type"`
		Payload core.FileStatRespPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return core.FileStatRespPayload{}, false
	}
	if env.Type != core.TypeFileStatResp {
		return core.FileStatRespPayload{}, false
	}
	if !core.SanitizeFileStatResp(env.Payload) {
		return core.FileStatRespPayload{}, false
	}
	return env.Payload, true
}

// ingestFileStatRespBody routes one offset answer to its waiter. Responses
// with no waiter (late, duplicate, or unsolicited) are dropped: stat answers
// carry no state worth keeping.
func (s *Service) ingestFileStatRespBody(body []byte) {
	p, ok := ParseFileStatResp(body)
	if !ok {
		return
	}
	s.fileMu.Lock()
	if ch, ok := s.statWaiters[p.TransferID]; ok {
		select {
		case ch <- FileStatResult{NextChunk: p.NextChunk, TotalChunks: p.TotalChunks, StatErr: p.Error}:
		default:
		}
	}
	s.fileMu.Unlock()
}

// queryUploadStat asks the phone for the smallest missing chunk of a staged
// transfer. Unknown transfers and timeouts (old peers) both mean zero:
// restart from the beginning with the same transfer id.
func (s *Service) queryUploadStat(transferID string) int {
	s.fileMu.Lock()
	if s.statWaiters == nil {
		s.statWaiters = make(map[string]chan FileStatResult)
	}
	ch := make(chan FileStatResult, 1)
	s.statWaiters[transferID] = ch
	s.fileMu.Unlock()
	defer func() {
		s.fileMu.Lock()
		delete(s.statWaiters, transferID)
		s.fileMu.Unlock()
	}()
	if err := s.sendFeatureToPhone(core.TypeFileStatReq, &core.FileStatReqPayload{TransferID: transferID}); err != nil {
		return 0
	}
	select {
	case r := <-ch:
		if r.StatErr != "" || r.NextChunk < 0 {
			return 0
		}
		return r.NextChunk
	case <-time.After(fileStatTimeout):
		return 0
	}
}

// saveNativeSession retains a failed upload for retry. Cancelled transfers
// are user intent and never retained.
func (s *Service) saveNativeSession(clean, remotePath, policy string, info os.FileInfo, transferID string, totalChunks, chunkSize int) {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	if chunkSize <= 0 {
		chunkSize = core.LegacyFileChunkRaw
	}
	if s.uploadSessions == nil {
		s.uploadSessions = make(map[string]*NativeUploadSession)
	}
	s.uploadSessions[transferID] = &NativeUploadSession{
		TransferID: transferID, LocalPath: clean, RemotePath: remotePath,
		TotalSize: info.Size(), TotalChunks: totalChunks, ChunkSize: chunkSize, Policy: policy,
		SourceMtime: info.ModTime().Unix(), CreatedAt: time.Now(),
	}
	if tr, ok := s.transfers[transferID]; ok {
		tr.Resumable = true
	}
}

// sweepUploadSessions drops retry state past the window, returning cancels
// so the caller can tell the phone to discard staged bytes after releasing
// fileMu. Callers must hold fileMu.
func (s *Service) sweepUploadSessions(now time.Time) []fileCancel {
	var out []fileCancel
	for id, sess := range s.uploadSessions {
		if sess == nil || now.Sub(sess.CreatedAt) <= uploadResumeWindow {
			continue
		}
		delete(s.uploadSessions, id)
		if tr, ok := s.transfers[id]; ok {
			tr.Resumable = false
		}
		if sess != nil && sess.RemotePath != "" {
			out = append(out, fileCancel{transferID: id, path: sess.RemotePath})
		}
	}
	return out
}

// ResumeUpload retries a failed native upload, sending only the phone's
// missing tail under the original transfer id and policy. The local file
// must be unchanged (size check); a changed file fails closed so retry can
// never splice two different files together.
func (s *Service) ResumeUpload(transferID string) (string, error) {
	s.fileMu.Lock()
	sess, ok := s.uploadSessions[transferID]
	s.fileMu.Unlock()
	if !ok || sess == nil {
		return "", errors.New("upload no longer retryable — send it again")
	}
	info, err := os.Stat(sess.LocalPath)
	if err != nil {
		return "", fmt.Errorf("stat local file: %w", err)
	}
	if info.IsDir() || info.Size() != sess.TotalSize {
		s.dropNativeSession(transferID)
		return "", errors.New("file changed since upload started — send it again")
	}
	if !s.IsPaired() {
		return "", errors.New("phone is offline — reconnect first")
	}
	next := s.queryUploadStat(transferID)
	if next < 0 {
		next = 0
	}
	if next >= sess.TotalChunks {
		s.dropNativeSession(transferID)
		return "Already on the phone.", nil
	}
	if err := s.resumeNativeChunks(sess, next); err != nil {
		return "", err
	}
	s.dropNativeSession(transferID)
	return fmt.Sprintf("Resumed %s from chunk %d.", sess.RemotePath, next), nil
}

func (s *Service) dropNativeSession(transferID string) {
	s.fileMu.Lock()
	delete(s.uploadSessions, transferID)
	if tr, ok := s.transfers[transferID]; ok {
		tr.Resumable = false
	}
	s.fileMu.Unlock()
}

// resumeNativeChunks re-reads the local file, re-hashes the confirmed prefix
// without sending, then sends the missing tail with the original transfer id
// and policy. The final chunk ack-waits exactly like a fresh upload.
func (s *Service) resumeNativeChunks(sess *NativeUploadSession, next int) error {
	f, err := os.Open(sess.LocalPath)
	if err != nil {
		return fmt.Errorf("open local file: %w", err)
	}
	defer func() { _ = f.Close() }()

	hasher := sha256.New()
	chunkSize := sess.ChunkSize
	if chunkSize <= 0 {
		chunkSize = core.LegacyFileChunkRaw
	}
	buf := make([]byte, chunkSize)
	for idx := 0; idx < next; idx++ {
		if _, err := io.ReadFull(f, buf); err != nil {
			return fmt.Errorf("re-read chunk %d: %w", idx, err)
		}
		if _, err := hasher.Write(buf); err != nil {
			return fmt.Errorf("hash chunk %d: %w", idx, err)
		}
	}
	s.fileMu.Lock()
	if tr, ok := s.transfers[sess.TransferID]; ok {
		tr.Status = "running"
		tr.Error = ""
		tr.DoneSize = int64(next) * int64(chunkSize)
	} else {
		// The record was pruned while the session survived: recreate it so
		// progress is visible again.
		if s.transfers == nil {
			s.transfers = make(map[string]*FileTransfer)
		}
		s.transfers[sess.TransferID] = &FileTransfer{
			ID: sess.TransferID, Path: sess.RemotePath, Direction: "upload",
			TotalSize: sess.TotalSize, DoneSize: int64(next) * int64(chunkSize),
			Status: "running", Source: "local", Resumable: true,
		}
	}
	var resumeBatch string
	if tr, ok := s.transfers[sess.TransferID]; ok {
		resumeBatch = tr.BatchID
	}
	s.fileMu.Unlock()
	s.emitTransfersChanged()
	if resumeBatch != "" {
		s.emitBatchesChanged()
	}

	for idx := next; idx < sess.TotalChunks; idx++ {
		if s.isTransferCancelled(sess.TransferID) {
			return errors.New("transfer cancelled")
		}
		offset := int64(idx) * int64(chunkSize)
		n, readErr := io.ReadFull(f, buf)
		if sess.TotalSize == 0 {
			n = 0
		} else if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) && !errors.Is(readErr, io.EOF) {
			s.failTransfer(sess.TransferID, readErr.Error())
			return fmt.Errorf("read chunk %d: %w", idx, readErr)
		}
		chunkRaw := buf[:n]
		if _, err := hasher.Write(chunkRaw); err != nil {
			s.failTransfer(sess.TransferID, err.Error())
			return fmt.Errorf("hash chunk %d: %w", idx, err)
		}
		sha := ""
		if idx == sess.TotalChunks-1 {
			sha = hex.EncodeToString(hasher.Sum(nil))
		}
		var waitAck func(time.Duration) error
		if idx == sess.TotalChunks-1 && s.peerSupportsFileAck() {
			waitAck = s.registerAckWaiter(sess.TransferID)
		}
		if err := s.sendNativePayload(sess, idx, offset, chunkRaw, sha); err != nil {
			s.failTransfer(sess.TransferID, err.Error())
			return err
		}
		s.fileMu.Lock()
		if tr, ok := s.transfers[sess.TransferID]; ok {
			tr.DoneSize = offset + int64(n)
		}
		s.fileMu.Unlock()
		if shouldEmitProgress(idx-next, sess.TotalChunks-next) {
			s.emitTransfersChanged()
			if resumeBatch != "" {
				s.emitBatchesChanged()
			}
		}
		if waitAck != nil {
			if err := waitAck(fileAckTimeout); err != nil {
				s.failTransfer(sess.TransferID, err.Error())
				return fmt.Errorf("delivery not confirmed: %w", err)
			}
		}
	}
	s.fileMu.Lock()
	if tr, ok := s.transfers[sess.TransferID]; ok && tr.Status == "running" {
		tr.Status = "done"
		tr.DoneSize = sess.TotalSize
		tr.completedAt = time.Now()
	}
	s.fileMu.Unlock()
	s.emitTransfersChanged()
	s.refreshBatchCompletion(resumeBatch)
	s.appendLine("file resume done path=" + sess.RemotePath)
	return nil
}

// sendNativePayload builds and transmits one native chunk with bounded
// retries for transient transport blips. Pure sender: no state changes
// beyond the socket write.
func (s *Service) sendNativePayload(sess *NativeUploadSession, idx int, offset int64, chunkRaw []byte, sha string) error {
	b64 := ""
	if len(chunkRaw) > 0 {
		b64 = base64.StdEncoding.EncodeToString(chunkRaw)
	}
	payload := core.FileChunkPayload{
		TransferID: sess.TransferID, Path: sess.RemotePath, Offset: offset,
		TotalSize: sess.TotalSize, ChunkIndex: idx, TotalChunks: sess.TotalChunks,
		DataB64: b64, Sha256: sha, Policy: sess.Policy, SourceMtime: sess.SourceMtime,
	}
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*attempt) * 200 * time.Millisecond)
		}
		if err = s.sendFeatureToPhone(core.TypeFileChunk, &payload); err == nil {
			return nil
		}
		if !s.IsPaired() {
			break
		}
	}
	if err == nil {
		err = errors.New("send chunk failed")
	}
	return fmt.Errorf("send chunk %d: %w", idx, err)
}

// ResumeBrowserUpload starts a browser-slice retry: it queries the phone's
// missing offset, resets the session hash for an ordered replay, and returns
// the chunk index the tab must resume sending from. The tab re-hashes the
// confirmed prefix via RehashBrowserChunk (local bytes, no network) and
// sends the rest via SendBrowserChunk.
func (s *Service) ResumeBrowserUpload(transferID string) (int, error) {
	s.fileMu.Lock()
	sess, ok := s.browserUploads[transferID]
	s.fileMu.Unlock()
	if !ok || sess == nil {
		return 0, errors.New("upload no longer retryable — send it again")
	}
	if !s.IsPaired() {
		return 0, errors.New("phone is offline — reconnect first")
	}
	next := s.queryUploadStat(transferID)
	if next < 0 {
		next = 0
	}
	if next >= sess.TotalChunks {
		return sess.TotalChunks, nil
	}
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	sess.Hash = sha256.New()
	sess.NextChunk = 0
	if tr, ok := s.transfers[transferID]; ok {
		tr.Status = "running"
		tr.Error = ""
		// Browser slices stay on the legacy 1 MiB stride (tab memory).
		tr.DoneSize = int64(next) * core.LegacyFileChunkRaw
		if tr.DoneSize > tr.TotalSize {
			tr.DoneSize = tr.TotalSize
		}
		tr.Resumable = true
	}
	return next, nil
}

// RehashBrowserChunk feeds one confirmed-prefix slice to the session hash
// without sending it. Indexes must replay in order from zero; the following
// SendBrowserChunk sequence then continues the hash seamlessly.
func (s *Service) RehashBrowserChunk(transferID, b64chunk string) error {
	s.fileMu.Lock()
	sess, ok := s.browserUploads[transferID]
	s.fileMu.Unlock()
	if !ok || sess == nil {
		return errors.New("unknown upload session")
	}
	raw, err := decodeBrowserSlice(b64chunk, sess, sess.NextChunk)
	if err != nil {
		return err
	}
	if _, err := sess.Hash.Write(raw); err != nil {
		return fmt.Errorf("hash chunk %d: %w", sess.NextChunk, err)
	}
	s.fileMu.Lock()
	if sess2, ok := s.browserUploads[transferID]; ok && sess2 != nil {
		sess2.NextChunk++
		sess2.UpdatedAt = time.Now()
	}
	s.fileMu.Unlock()
	return nil
}

// decodeBrowserSlice validates one frontend slice against the session shape.
// Browser slices always use the legacy 1 MiB stride so multi-GB drops never
// sit fully in tab memory. Pure, no IO: size, order, and base64 shape all
// fail closed.
func decodeBrowserSlice(b64chunk string, sess *BrowserUploadSession, idx int) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64chunk))
	if err != nil {
		raw, err = base64.StdEncoding.WithPadding(base64.StdPadding).DecodeString(strings.TrimSpace(b64chunk))
		if err != nil {
			return nil, errors.New("invalid base64")
		}
	}
	var expected int
	if idx < sess.TotalChunks-1 {
		expected = core.LegacyFileChunkRaw
	} else if idx == sess.TotalChunks-1 {
		expected = int(sess.TotalSize - int64(idx)*core.LegacyFileChunkRaw)
	} else {
		return nil, errors.New("chunk past end")
	}
	if len(raw) != expected {
		return nil, fmt.Errorf("chunk %d size %d, want %d", idx, len(raw), expected)
	}
	return raw, nil
}
