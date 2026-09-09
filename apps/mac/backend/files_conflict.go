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
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fuseitall/core"
)

// File-upload sentinels: stable low-cardinality templates for errors.Is.
// Details (basenames, counts) stay out of the grouping message; callers
// branch on the sentinel, never on message substrings.
var (
	ErrNoFilesToUpload  = errors.New("no files to upload")
	ErrTooManyFiles     = errors.New("too many files in one batch")
	ErrPhoneOffline     = errors.New("phone is offline — reconnect first")
	ErrUnknownBatch     = errors.New("unknown upload batch")
	ErrTransferCanceled = errors.New("transfer cancelled")
	ErrInvalidLocal     = errors.New("invalid local path")
	ErrInvalidRemote    = errors.New("invalid remote path")
)

// browserUploadIdleTimeout bounds a stalled browser slice session. Idle
// sessions are swept (transfer marked error) so an abandoned tab cannot leak
// server state forever. Active resume retries refresh UpdatedAt per chunk.
const browserUploadIdleTimeout = 30 * time.Minute

// BrowserUploadSession tracks one frontend-sliced browser upload. The phone
// sees an ordinary chunk sequence under one transfer_id; slicing only changes
// where bytes are buffered (1 MiB in the tab, never the whole file).
type BrowserUploadSession struct {
	ID          string
	RemotePath  string
	TotalSize   int64
	TotalChunks int
	Policy      string
	SourceMtime int64
	Hash        hash.Hash
	NextChunk   int
	UpdatedAt   time.Time
}

// mkdirRemoteParents creates remoteDir/subDir parents on the phone (mkdir is
// idempotent) and returns the deepest base. Pure sender: failures are
// best-effort since the target may already exist.
func (s *Service) mkdirRemoteParents(remoteDir, subDir string) string {
	base := remoteDir
	if subDir != "" {
		if base != "" {
			base = base + "/" + subDir
		} else {
			base = subDir
		}
	}
	if _, ok := core.SanitizeFilePath(base); ok && base != "" {
		parts := strings.Split(base, "/")
		cur := ""
		for _, p := range parts {
			if cur == "" {
				cur = p
			} else {
				cur = cur + "/" + p
			}
			pl := core.FileMkdirPayload{Path: cur}
			_ = s.sendFeatureToPhone(core.TypeFileMkdir, &pl)
		}
	}
	return base
}

// LocalFileInfo is the Wails-bound stat row for one dropped path. Mtime is
// unix seconds truncated to match file-list mod_time granularity.
type LocalFileInfo struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
	IsDir bool   `json:"is_dir"`
}

// StatLocalFiles stats dropped Finder paths so the UI can build the conflict
// list before sending any bytes. It never touches the network.
func (s *Service) StatLocalFiles(localPaths []string) ([]LocalFileInfo, error) {
	if len(localPaths) == 0 {
		return nil, ErrNoFilesToUpload
	}
	out := make([]LocalFileInfo, 0, len(localPaths))
	for _, lp := range localPaths {
		if strings.TrimSpace(lp) == "" {
			return nil, fmt.Errorf("%w: empty drop — re-select the file in Finder", ErrInvalidLocal)
		}
		clean := filepath.Clean(lp)
		if clean == "" || clean == "." {
			return nil, fmt.Errorf("%w: empty drop — re-select the file in Finder", ErrInvalidLocal)
		}
		info, err := os.Stat(clean)
		if err != nil {
			return nil, fmt.Errorf("stat local file: %w", err)
		}
		out = append(out, LocalFileInfo{
			Path:  clean,
			Name:  filepath.Base(clean),
			Size:  info.Size(),
			Mtime: info.ModTime().Unix(),
			IsDir: info.IsDir(),
		})
	}
	return out, nil
}

// DecidedUpload is one frontend-resolved file: an exact local path to an
// exact remote path with its wire conflict policy. The frontend owns all
// conflict decisions (skip never reaches here — skipped files are simply
// absent; keep-both is pre-resolved to a fresh path); the backend only
// validates and streams. JSON-tagged for the Wails binding.
type DecidedUpload struct {
	LocalPath  string `json:"local_path"`
	RemotePath string `json:"remote_path"`
	Policy     string `json:"policy"`
}

// UnmarshalJSON accepts both snake_case (local_path, remote_path) and
// camelCase (localPath, remotePath) so Wails IPC deserialization works
// regardless of whether the caller normalizes the keys.
func (d *DecidedUpload) UnmarshalJSON(b []byte) error {
	if d == nil {
		return errors.New("nil DecidedUpload")
	}
	type Alias DecidedUpload
	aux := struct {
		*Alias
		LocalPathCamel  string `json:"localPath"`
		RemotePathCamel string `json:"remotePath"`
	}{
		Alias: (*Alias)(d),
	}
	if err := json.Unmarshal(b, &aux); err != nil {
		return fmt.Errorf("unmarshal decided upload: %w", err)
	}
	if d.LocalPath == "" && aux.LocalPathCamel != "" {
		d.LocalPath = aux.LocalPathCamel
	}
	if d.RemotePath == "" && aux.RemotePathCamel != "" {
		d.RemotePath = aux.RemotePathCamel
	}
	return nil
}

// UploadDecidedFiles streams frontend-resolved files in one batch so drops
// of many files share a single 8-wide pipelined run instead of one
// ack-confirmed round-trip per file. Each entry carries its own wire policy
// (overwrite, if_newer, or legacy keep-both); skip/stop never ride (the
// frontend filters them before calling). Directories are rejected fail-closed
// — folder drops keep the UploadLocalFilesWithPolicyInBatch path, which owns
// remote mkdir ordering. Unknown batches fail closed like the other entry
// points. Returns fail-closed like runUploadTasks: all tasks still stream,
// the first error is reported.
func (s *Service) UploadDecidedFiles(decided []DecidedUpload, batchID string) (string, error) {
	if len(decided) == 0 {
		return "", ErrNoFilesToUpload
	}
	if len(decided) > 10000 {
		return "", ErrTooManyFiles
	}
	if !s.IsPaired() {
		return "", ErrPhoneOffline
	}
	if batchID == "" {
		if id, err := s.BeginUploadBatch(len(decided), 0); err == nil {
			batchID = id
		}
	} else {
		s.fileMu.Lock()
		_, ok := s.batches[batchID]
		s.fileMu.Unlock()
		if !ok {
			return "", ErrUnknownBatch
		}
	}
	tasks := make([]uploadTask, 0, len(decided))
	stride := s.negotiatedChunkSize()
	wantsAck := s.peerSupportsFileAck()
	for _, d := range decided {
		if s.isBatchCancelled(batchID) {
			return "", ErrTransferCanceled
		}
		if strings.TrimSpace(d.LocalPath) == "" {
			return "", fmt.Errorf("%w: empty drop — re-select the file in Finder", ErrInvalidLocal)
		}
		clean := filepath.Clean(d.LocalPath)
		if clean == "" || clean == "." {
			return "", fmt.Errorf("%w: empty drop — re-select the file in Finder", ErrInvalidLocal)
		}
		remotePath, ok := core.SanitizeFilePath(d.RemotePath)
		if !ok || remotePath == "" {
			return "", fmt.Errorf("%w: empty destination — pick a phone folder", ErrInvalidRemote)
		}
		policy := normalizeWirePolicy(d.Policy)
		info, err := os.Stat(clean)
		if err != nil {
			return "", fmt.Errorf("stat local file: %w", err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("is directory: %s (use folder upload)", filepath.Base(clean))
		}
		if info.Size() > core.MaxFileTotalSize {
			return "", fmt.Errorf("file too large (max 8 GiB): %s", filepath.Base(clean))
		}
		tasks = append(tasks, uploadTask{localPath: clean, remotePath: remotePath, policy: policy, info: info, chunkSize: stride, wantsAck: wantsAck})
	}
	if err := s.runUploadTasks(tasks, batchID); err != nil {
		return "", err
	}
	return fmt.Sprintf("Uploaded %d file(s).", len(tasks)), nil
}

// UploadLocalFilesWithPolicy uploads Finder paths into remoteDir honoring a
// conflict policy (overwrite, if_newer, or legacy keep-both). Skip/stop are
// sender-side only: the UI simply does not call for skipped files and aborts
// the batch on stop. Keep-both resolves to a fresh path per file via
// UploadLocalFileToRemotePath before calling. The whole call is one batch.
func (s *Service) UploadLocalFilesWithPolicy(localPaths []string, remoteDir, policy string) (string, error) {
	return s.UploadLocalFilesWithPolicyInBatch(localPaths, remoteDir, policy, "")
}

// UploadLocalFilesWithPolicyInBatch attaches the call to a frontend batch.
func (s *Service) UploadLocalFilesWithPolicyInBatch(localPaths []string, remoteDir, policy, batchID string) (string, error) {
	if len(localPaths) == 0 {
		return "", ErrNoFilesToUpload
	}
	remoteSan, ok := core.SanitizeFilePath(remoteDir)
	if !ok {
		return "", ErrInvalidRemote
	}
	if !s.IsPaired() {
		return "", ErrPhoneOffline
	}
	policy = normalizeWirePolicy(policy)
	if batchID == "" {
		files, bytes := estimateBatchTotals(localPaths)
		if id, err := s.BeginUploadBatch(files, bytes); err == nil {
			batchID = id
		}
	} else {
		s.fileMu.Lock()
		_, ok := s.batches[batchID]
		s.fileMu.Unlock()
		if !ok {
			return "", ErrUnknownBatch
		}
	}
	// Bounded parallel path with discovery overlapped with sending: the
	// first byte leaves after the first file's stat+open, not after the
	// whole folder walk plus mkdirs.
	if err := s.uploadPathsStreaming(localPaths, remoteSan, policy, batchID); err != nil {
		return "", err
	}
	return fmt.Sprintf("Uploaded %d item(s).", len(localPaths)), nil
}

// UploadLocalFileToRemotePath uploads one Finder file to an exact remote
// path (used for keep-both renames resolved by the UI via KeepBothName).
func (s *Service) UploadLocalFileToRemotePath(localPath, remotePath, policy string) (string, error) {
	return s.UploadLocalFileToRemotePathInBatch(localPath, remotePath, policy, "")
}

// UploadLocalFileToRemotePathInBatch attaches a keep-both rename to a batch.
func (s *Service) UploadLocalFileToRemotePathInBatch(localPath, remotePath, policy, batchID string) (string, error) {
	if strings.TrimSpace(localPath) == "" {
		return "", fmt.Errorf("%w: empty drop — re-select the file in Finder", ErrInvalidLocal)
	}
	clean := filepath.Clean(localPath)
	if clean == "" || clean == "." {
		return "", fmt.Errorf("%w: empty drop — re-select the file in Finder", ErrInvalidLocal)
	}
	if _, ok := core.SanitizeFilePath(remotePath); !ok || remotePath == "" {
		return "", fmt.Errorf("%w: empty destination — pick a phone folder", ErrInvalidRemote)
	}
	if !s.IsPaired() {
		return "", ErrPhoneOffline
	}
	info, err := os.Stat(clean)
	if err != nil {
		return "", fmt.Errorf("stat local file: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("is directory: %s (use folder upload)", filepath.Base(clean))
	}
	if info.Size() > core.MaxFileTotalSize {
		return "", fmt.Errorf("file too large (max 8 GiB): %s", filepath.Base(clean))
	}
	if batchID == "" {
		if id, err := s.BeginUploadBatch(1, info.Size()); err == nil {
			batchID = id
		}
	}
	if err := s.streamLocalFileInBatch(clean, info, remotePath, policy, batchID); err != nil {
		return "", err
	}
	return "Uploaded " + filepath.Base(remotePath), nil
}

// normalizeWirePolicy maps UI choices to wire values. Only overwrite and
// if_newer ride the wire; everything else (skip, keep_both, stop, legacy)
// sends as legacy keep-both so old peers suffix a copy instead of losing data.
func normalizeWirePolicy(policy string) string {
	switch policy {
	case core.FilePolicyOverwrite, core.FilePolicyIfNewer:
		return policy
	default:
		return ""
	}
}

// UploadBrowserFileToRemotePath uploads browser bytes to an exact remote
// path (keep-both rename target). It decodes exactly like UploadBrowserFile.
func (s *Service) UploadBrowserFileToRemotePath(b64, remotePath, policy string, sourceMtime int64) (string, error) {
	if _, ok := core.SanitizeFilePath(remotePath); !ok || remotePath == "" {
		return "", errors.New("invalid remote path")
	}
	dir := filepath.Dir(remotePath)
	if dir == "." {
		dir = ""
	}
	return s.UploadBrowserFileWithPolicy(b64, filepath.Base(remotePath), dir, policy, sourceMtime)
}

// BrowserUploadBegin is the Wails-bound receipt for a started slice session.
type BrowserUploadBegin struct {
	TransferID string `json:"transfer_id"`
	RemotePath string `json:"remote_path"`
}

// BeginBrowserUpload starts a frontend-sliced browser upload: the tab sends
// 1 MiB slices via SendBrowserChunk instead of one whole-file base64 blob,
// so multi-GB drops never sit fully in webview or server memory. It returns
// the transfer id (also the progress id for GetTransfers) and the resolved
// remote path. The phone sees an ordinary file-chunk sequence.
func (s *Service) BeginBrowserUpload(filename, relPath, remoteDir string, totalSize int64, policy string, sourceMtime int64) (BrowserUploadBegin, error) {
	if totalSize < 0 || totalSize > core.MaxFileTotalSize {
		return BrowserUploadBegin{}, errors.New("file too large")
	}
	remoteSan, ok := core.SanitizeFilePath(remoteDir)
	if !ok {
		return BrowserUploadBegin{}, errors.New("invalid remote directory")
	}
	base := remoteSan
	name := filename
	if relPath != "" {
		if _, ok := core.SanitizeFilePath(relPath); !ok {
			return BrowserUploadBegin{}, errors.New("invalid relative path")
		}
		if dir := filepath.Dir(relPath); dir != "." && dir != "" {
			base = s.mkdirRemoteParents(remoteSan, dir)
			name = filepath.Base(relPath)
		} else {
			name = filepath.Base(relPath)
		}
	}
	if _, ok := core.SanitizeFileName(name); !ok {
		return BrowserUploadBegin{}, errors.New("invalid filename")
	}
	remotePath := name
	if base != "" {
		remotePath = base + "/" + name
	}
	id, rp, err := s.beginBrowserSession(remotePath, totalSize, policy, sourceMtime)
	if err != nil {
		return BrowserUploadBegin{}, err
	}
	return BrowserUploadBegin{TransferID: id, RemotePath: rp}, nil
}

// BeginBrowserUploadToPath starts a sliced upload to an exact remote path
// (keep-both rename target resolved by the UI).
func (s *Service) BeginBrowserUploadToPath(remotePath string, totalSize int64, policy string, sourceMtime int64) (string, error) {
	if _, ok := core.SanitizeFilePath(remotePath); !ok || remotePath == "" {
		return "", errors.New("invalid remote path")
	}
	if totalSize < 0 || totalSize > core.MaxFileTotalSize {
		return "", errors.New("file too large")
	}
	id, _, err := s.beginBrowserSessionInBatch(remotePath, totalSize, policy, sourceMtime, "")
	return id, err
}

// BeginBrowserUploadInBatch starts a sliced upload attached to a batch.
func (s *Service) BeginBrowserUploadInBatch(filename, relPath, remoteDir string, totalSize int64, policy string, sourceMtime int64, batchID string) (BrowserUploadBegin, error) {
	if batchID != "" {
		s.fileMu.Lock()
		_, ok := s.batches[batchID]
		s.fileMu.Unlock()
		if !ok {
			return BrowserUploadBegin{}, errors.New("unknown upload batch")
		}
	}
	if totalSize < 0 || totalSize > core.MaxFileTotalSize {
		return BrowserUploadBegin{}, errors.New("file too large")
	}
	remoteSan, ok := core.SanitizeFilePath(remoteDir)
	if !ok {
		return BrowserUploadBegin{}, errors.New("invalid remote directory")
	}
	base := remoteSan
	name := filename
	if relPath != "" {
		if _, ok := core.SanitizeFilePath(relPath); !ok {
			return BrowserUploadBegin{}, errors.New("invalid relative path")
		}
		if dir := filepath.Dir(relPath); dir != "." && dir != "" {
			base = s.mkdirRemoteParents(remoteSan, dir)
			name = filepath.Base(relPath)
		} else {
			name = filepath.Base(relPath)
		}
	}
	if _, ok := core.SanitizeFileName(name); !ok {
		return BrowserUploadBegin{}, errors.New("invalid filename")
	}
	remotePath := name
	if base != "" {
		remotePath = base + "/" + name
	}
	id, rp, err := s.beginBrowserSessionInBatch(remotePath, totalSize, policy, sourceMtime, batchID)
	if err != nil {
		return BrowserUploadBegin{}, err
	}
	return BrowserUploadBegin{TransferID: id, RemotePath: rp}, nil
}

// BeginBrowserUploadToPathInBatch starts a keep-both upload in a batch.
func (s *Service) BeginBrowserUploadToPathInBatch(remotePath string, totalSize int64, policy string, sourceMtime int64, batchID string) (string, error) {
	if _, ok := core.SanitizeFilePath(remotePath); !ok || remotePath == "" {
		return "", errors.New("invalid remote path")
	}
	if totalSize < 0 || totalSize > core.MaxFileTotalSize {
		return "", errors.New("file too large")
	}
	if batchID != "" {
		s.fileMu.Lock()
		_, ok := s.batches[batchID]
		s.fileMu.Unlock()
		if !ok {
			return "", errors.New("unknown upload batch")
		}
	}
	id, _, err := s.beginBrowserSessionInBatch(remotePath, totalSize, policy, sourceMtime, batchID)
	return id, err
}

func (s *Service) beginBrowserSession(remotePath string, totalSize int64, policy string, sourceMtime int64) (string, string, error) {
	return s.beginBrowserSessionInBatch(remotePath, totalSize, policy, sourceMtime, "")
}

func (s *Service) beginBrowserSessionInBatch(remotePath string, totalSize int64, policy string, sourceMtime int64, batchID string) (string, string, error) {
	if _, ok := core.SanitizeFilePath(remotePath); !ok || remotePath == "" {
		return "", "", errors.New("invalid remote path")
	}
	if !s.IsPaired() {
		return "", "", errors.New("phone is offline — reconnect first")
	}
	transferID, err := freshTransferID()
	if err != nil {
		return "", "", err
	}
	// Browser slices stay on the legacy 1 MiB stride: the tab holds one
	// slice in memory at a time, and 1 MiB keeps tab pressure low.
	totalChunks := core.TotalChunksForSize(totalSize, core.LegacyFileChunkRaw)
	s.fileMu.Lock()
	if s.transfers == nil {
		s.transfers = make(map[string]*FileTransfer)
	}
	if s.browserUploads == nil {
		s.browserUploads = make(map[string]*BrowserUploadSession)
	}
	s.transfers[transferID] = &FileTransfer{
		ID: transferID, Path: remotePath, Direction: "upload",
		TotalSize: totalSize, Status: "running", Source: "browser",
		BatchID: batchID,
	}
	s.browserUploads[transferID] = &BrowserUploadSession{
		ID: transferID, RemotePath: remotePath, TotalSize: totalSize,
		TotalChunks: totalChunks, Policy: normalizeWirePolicy(policy),
		SourceMtime: sourceMtime, Hash: sha256.New(), UpdatedAt: time.Now(),
	}
	s.fileMu.Unlock()
	s.emitTransfersChanged()
	if batchID != "" {
		s.emitBatchesChanged()
	}
	return transferID, remotePath, nil
}

// SendBrowserChunk forwards one frontend slice as file-chunk number NextChunk.
// Chunks must arrive in order; out-of-order delivery fails closed so a
// misbehaving tab cannot interleave bytes. It reports done when the final
// chunk is sent (commit confirmation arrives separately via file-ack).
func (s *Service) SendBrowserChunk(transferID, b64chunk string) (bool, error) {
	s.fileMu.Lock()
	sess, ok := s.browserUploads[transferID]
	tr, trok := s.transfers[transferID]
	if !ok || sess == nil {
		s.fileMu.Unlock()
		return false, errors.New("unknown upload session")
	}
	if trok && tr.Status == "cancelled" {
		delete(s.browserUploads, transferID)
		s.fileMu.Unlock()
		return false, errors.New("transfer cancelled")
	}
	idx := sess.NextChunk
	offset := int64(idx) * core.LegacyFileChunkRaw
	s.fileMu.Unlock()

	raw, err := decodeBrowserSlice(b64chunk, sess, idx)
	if err != nil {
		return false, err
	}
	if _, err := sess.Hash.Write(raw); err != nil {
		return false, fmt.Errorf("hash chunk %d: %w", idx, err)
	}
	b64 := ""
	if len(raw) > 0 {
		b64 = base64.StdEncoding.EncodeToString(raw)
	}
	sha := ""
	last := idx == sess.TotalChunks-1
	if last {
		sha = fmt.Sprintf("%x", sess.Hash.Sum(nil))
	}
	payload := core.FileChunkPayload{
		Nonce: transferID, TransferID: transferID, Path: sess.RemotePath,
		Offset: offset, TotalSize: sess.TotalSize, ChunkIndex: idx,
		TotalChunks: sess.TotalChunks, DataB64: b64, Sha256: sha,
		Policy: sess.Policy, SourceMtime: sess.SourceMtime,
	}
	if !core.SanitizeFileChunk(payload) {
		return false, fmt.Errorf("chunk %d failed validation", idx)
	}
	var waitAck func(time.Duration) error
	if last && s.peerSupportsFileAck() {
		waitAck = s.registerAckWaiter(transferID)
	}
	if err := s.sendFileChunkWithRetry(core.TypeFileChunk, &payload); err != nil {
		s.failTransfer(transferID, err.Error())
		s.fileMu.Lock()
		if tr, ok := s.transfers[transferID]; ok {
			tr.Resumable = true
		}
		if sess2, ok := s.browserUploads[transferID]; ok && sess2 != nil {
			sess2.UpdatedAt = time.Now()
		}
		s.fileMu.Unlock()
		return false, fmt.Errorf("send chunk %d: %w", idx, err)
	}
	var batchID string
	s.fileMu.Lock()
	if tr, ok := s.transfers[transferID]; ok {
		batchID = tr.BatchID
		tr.DoneSize = offset + int64(len(raw))
		tr.sha256 = sha
		if last && waitAck == nil && tr.Status == "running" {
			// Legacy peer: no file-ack coming, the send is final.
			tr.Status = "done"
			tr.completedAt = time.Now()
		}
	}
	if sess2, ok := s.browserUploads[transferID]; ok && sess2 != nil {
		sess2.NextChunk++
		sess2.UpdatedAt = time.Now()
		if last {
			delete(s.browserUploads, transferID)
		}
	}
	s.fileMu.Unlock()
	s.emitTransfersChanged()
	if batchID != "" {
		if last && waitAck == nil {
			s.refreshBatchCompletion(batchID)
		} else {
			s.emitBatchesChanged()
		}
	}
	if waitAck != nil {
		if err := waitAck(fileAckTimeout); err != nil {
			s.failTransfer(transferID, err.Error())
			s.fileMu.Lock()
			if tr, ok := s.transfers[transferID]; ok {
				tr.Resumable = true
			}
			if sess2, ok := s.browserUploads[transferID]; ok && sess2 != nil {
				sess2.UpdatedAt = time.Now()
			}
			s.fileMu.Unlock()
			return false, fmt.Errorf("delivery not confirmed: %w", err)
		}
		s.fileMu.Lock()
		if tr, ok := s.transfers[transferID]; ok {
			tr.Resumable = false
		}
		s.fileMu.Unlock()
		s.refreshBatchCompletion(batchID)
	}
	return last, nil
}

// AbortBrowserUpload drops a sliced session without sending further chunks.
// It also tells the phone to discard staged bytes; the transfer record is
// marked cancelled for the progress UI and is never resumable.
func (s *Service) AbortBrowserUpload(transferID string) (string, error) {
	s.fileMu.Lock()
	if _, ok := s.browserUploads[transferID]; ok {
		delete(s.browserUploads, transferID)
	}
	tr, ok := s.transfers[transferID]
	var remotePath, batchID string
	if ok {
		remotePath = tr.Path
		batchID = tr.BatchID
		tr.Resumable = false
	}
	if ok && tr.Status == "running" {
		tr.Status = "cancelled"
		tr.completedAt = time.Now()
	}
	s.fileMu.Unlock()
	if ok {
		payload := core.FileCancelPayload{TransferID: transferID, Path: remotePath}
		_ = s.sendFeatureToPhone(core.TypeFileCancel, &payload)
	}
	s.emitTransfersChanged()
	s.refreshBatchCompletion(batchID)
	if !ok {
		return "", errors.New("unknown upload session")
	}
	return "Cancelled.", nil
}

// sweepBrowserUploads marks sessions idle past the timeout as errored and
// drops them, returning cancels so the caller can tell the phone to discard
// staged bytes after releasing fileMu. Callers must hold fileMu.
func (s *Service) sweepBrowserUploads(now time.Time) []fileCancel {
	var out []fileCancel
	for id, sess := range s.browserUploads {
		if sess == nil || now.Sub(sess.UpdatedAt) <= browserUploadIdleTimeout {
			continue
		}
		delete(s.browserUploads, id)
		if tr, ok := s.transfers[id]; ok && tr.Status == "running" {
			tr.Status = "error"
			tr.Error = "upload stalled"
			tr.Resumable = false
			tr.completedAt = now
		}
		if sess != nil && sess.RemotePath != "" {
			out = append(out, fileCancel{transferID: id, path: sess.RemotePath})
		}
	}
	return out
}

// fileCancel is a deferred discard request for a swept session.
type fileCancel struct {
	transferID string
	path       string
}
