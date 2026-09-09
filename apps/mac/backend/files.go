// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"fuseitall/core"
)

// FileEntryView is the Wails-bound row for one file. Mirrors core.FileEntry
// but json-tagged for TS bindings.
type FileEntryView struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"is_dir"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time"`
	Mime    string `json:"mime,omitempty"`
}

// FileListResult is the typed listing for the frontend. Error is "" on success.
// ErrorCode/Permission are machine-readable (permission_denied + files) so
// the UI renders per-viewer empty states without string matching.
type FileListResult struct {
	Path       string          `json:"path"`
	Entries    []FileEntryView `json:"entries"`
	Error      string          `json:"error,omitempty"`
	ErrorCode  string          `json:"error_code,omitempty"`
	Permission string          `json:"permission,omitempty"`
}

// FileTransferView is the Wails-bound progress row.
type FileTransferView struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Direction string `json:"direction"` // upload | download
	Status    string `json:"status"`    // running | done | error | cancelled
	Progress  int    `json:"progress"`  // 0..100
	TotalSize int64  `json:"total_size"`
	DoneSize  int64  `json:"done_size"`
	Error     string `json:"error,omitempty"`
}

// FileTransfer tracks one chunked transfer. Guarded by Service.fileMu.
type FileTransfer struct {
	ID        string
	Path      string
	Direction string
	TotalSize int64
	DoneSize  int64
	Status    string
	Error     string
	// Download temp file
	tmpPath string
	file    *os.File
	// upload
	sha256      string
	completedAt time.Time
}

// stagingRoot returns a safe staging directory for downloads.
// Uses os.TempDir with subdir, 0700.
func stagingRoot() (string, error) {
	base := filepath.Join(os.TempDir(), "fuseitall-files")
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", fmt.Errorf("mkdir staging: %w", err)
	}
	return base, nil
}

func freshTransferID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate transfer id: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}

// ListPhoneFiles requests a directory listing from the phone and waits for
// the file-list-resp push. It returns the listing or an error after 8s.
// Path is sandboxed rel path ("" = root).
func (s *Service) ListPhoneFiles(path string) (FileListResult, error) {
	san, ok := core.SanitizeFilePath(path)
	if !ok {
		return FileListResult{}, errors.New("invalid path")
	}
	if !s.IsPaired() {
		// Allow listing even when TTL expired if we have remembered device?
		// For now require paired; frontend will show reconnect.
		return FileListResult{}, errors.New("phone is offline — reconnect first")
	}
	if err := s.checkPeerCapability(core.CapabilityFiles, 5); err != nil {
		var upd *core.UpdateRequiredError
		if errors.As(err, &upd) {
			return FileListResult{Error: upd.Message, ErrorCode: core.CodeUpdateRequired}, err
		}
		return FileListResult{}, err
	}
	reqID, err := freshTransferID()
	if err != nil {
		return FileListResult{}, err
	}
	// Shorten to 16 hex chars for req_id readability (core allows 1..64)
	reqID = reqID[:16]
	ch := make(chan FileListResult, 1)
	s.fileMu.Lock()
	if s.pendingLists == nil {
		s.pendingLists = make(map[string]chan FileListResult)
	}
	if s.transfers == nil {
		s.transfers = make(map[string]*FileTransfer)
	}
	s.pendingLists[reqID] = ch
	s.fileMu.Unlock()
	defer func() {
		s.fileMu.Lock()
		delete(s.pendingLists, reqID)
		s.fileMu.Unlock()
	}()

	payload := core.FileListPayload{Path: san, ReqID: reqID}
	if err := s.sendFeatureToPhone(core.TypeFileList, &payload); err != nil {
		return FileListResult{}, fmt.Errorf("send file-list: %w", err)
	}
	select {
	case res := <-ch:
		s.fileMu.Lock()
		s.lastList = res
		s.fileMu.Unlock()
		if res.Error != "" {
			return res, errors.New(res.Error)
		}
		return res, nil
	case <-time.After(8 * time.Second):
		return FileListResult{}, errors.New("listing timed out — phone did not respond")
	}
}

// GetLastFileList returns the last successful listing (offline cache).
func (s *Service) GetLastFileList() FileListResult {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	if s.lastList.Entries == nil {
		s.lastList.Entries = []FileEntryView{}
	}
	return s.lastList
}

// MkdirPhone creates a directory on the phone.
func (s *Service) MkdirPhone(path string) (string, error) {
	if _, ok := core.SanitizeFilePath(path); !ok {
		return "", errors.New("invalid path")
	}
	if path == "" {
		return "", errors.New("path must not be empty")
	}
	if !s.IsPaired() {
		return "", errors.New("phone is offline — reconnect first")
	}
	payload := core.FileMkdirPayload{Path: path}
	if err := s.sendFeatureToPhone(core.TypeFileMkdir, &payload); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}
	s.appendLine("file mkdir sent path=" + path)
	return "Folder created.", nil
}

// DeletePhone deletes a file or empty directory on the phone.
func (s *Service) DeletePhone(path string) (string, error) {
	if _, ok := core.SanitizeFilePath(path); !ok {
		return "", errors.New("invalid path")
	}
	if path == "" {
		return "", errors.New("path must not be empty")
	}
	if !s.IsPaired() {
		return "", errors.New("phone is offline — reconnect first")
	}
	payload := core.FileDeletePayload{Path: path}
	if err := s.sendFeatureToPhone(core.TypeFileDelete, &payload); err != nil {
		return "", fmt.Errorf("delete: %w", err)
	}
	s.appendLine("file delete sent path=" + path)
	return "Deleted.", nil
}

// RenamePhone renames a file or directory on the phone within the same directory.
func (s *Service) RenamePhone(from, to string) (string, error) {
	if _, ok := core.SanitizeFilePath(from); !ok {
		return "", errors.New("invalid source path")
	}
	if _, ok := core.SanitizeFilePath(to); !ok {
		return "", errors.New("invalid destination path")
	}
	if from == "" || to == "" {
		return "", errors.New("path must not be empty")
	}
	if from == to {
		return "", errors.New("source and destination are the same")
	}
	// Same-directory rename only (friendliness, no cross-folder move confusion).
	fromDir := ""
	if idx := strings.LastIndex(from, "/"); idx >= 0 {
		fromDir = from[:idx]
	}
	toDir := ""
	if idx := strings.LastIndex(to, "/"); idx >= 0 {
		toDir = to[:idx]
	}
	if fromDir != toDir {
		return "", errors.New("rename must stay in the same folder")
	}
	toName := filepath.Base(to)
	if _, ok := core.SanitizeFileName(toName); !ok {
		return "", errors.New("invalid new name")
	}
	if !s.IsPaired() {
		return "", errors.New("phone is offline — reconnect first")
	}
	payload := core.FileRenamePayload{From: from, To: to}
	if err := s.sendFeatureToPhone(core.TypeFileRename, &payload); err != nil {
		return "", fmt.Errorf("rename: %w", err)
	}
	s.appendLine("file rename sent from=" + from + " to=" + to)
	return "Renamed.", nil
}

// UploadLocalFiles uploads one or more local Mac files and folders into remoteDir on the phone.
// Each localPath may be a file or directory; directories are walked recursively.
// Drag-n-drop calls this with the dropped file paths.
func (s *Service) UploadLocalFiles(localPaths []string, remoteDir string) (string, error) {
	if len(localPaths) == 0 {
		return "", errors.New("no files to upload")
	}
	remoteSan, ok := core.SanitizeFilePath(remoteDir)
	if !ok {
		return "", errors.New("invalid remote directory")
	}
	if !s.IsPaired() {
		return "", errors.New("phone is offline — reconnect first")
	}
	for _, lp := range localPaths {
		clean := filepath.Clean(lp)
		info, err := os.Stat(clean)
		if err != nil {
			return "", fmt.Errorf("stat local file: %w", err)
		}
		if info.IsDir() {
			if err := s.uploadOneFolder(clean, remoteSan); err != nil {
				return "", err
			}
		} else {
			if err := s.uploadOneFile(clean, remoteSan); err != nil {
				return "", err
			}
		}
	}
	return fmt.Sprintf("Uploaded %d item(s).", len(localPaths)), nil
}

// uploadOneFolder walks a directory and uploads all files recursively.
func (s *Service) uploadOneFolder(localDir, remoteDir string) error {
	base := filepath.Base(localDir)
	if _, ok := core.SanitizeFileName(base); !ok {
		base = "folder"
	}
	targetRemote := base
	if remoteDir != "" {
		targetRemote = remoteDir + "/" + base
	}
	// Ensure remote folder exists (mkdir is idempotent on Android).
	if _, ok := core.SanitizeFilePath(targetRemote); !ok {
		return errors.New("invalid remote path")
	}
	payload := core.FileMkdirPayload{Path: targetRemote}
	if err := s.sendFeatureToPhone(core.TypeFileMkdir, &payload); err != nil {
		// Best-effort: continue even if mkdir fails (may already exist).
		s.appendLine("mkdir for folder failed path=" + targetRemote + " err=" + err.Error())
	}
	err := filepath.WalkDir(localDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == localDir {
			return nil
		}
		rel, err := filepath.Rel(localDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		remotePath := targetRemote + "/" + rel
		if _, ok := core.SanitizeFilePath(remotePath); !ok {
			s.appendLine("skip invalid remote path=" + remotePath)
			return nil
		}
		if d.IsDir() {
			pl := core.FileMkdirPayload{Path: remotePath}
			_ = s.sendFeatureToPhone(core.TypeFileMkdir, &pl)
			return nil
		}
		dir := filepath.Dir(remotePath)
		if dir == "." {
			dir = ""
		}
		return s.uploadOneFile(path, dir)
	})
	if err != nil {
		return fmt.Errorf("walk folder: %w", err)
	}
	return nil
}

func (s *Service) uploadOneFile(localPath, remoteDir string) error {
	clean := filepath.Clean(localPath)
	if clean == "" || clean == "." {
		return errors.New("invalid local path")
	}
	info, err := os.Stat(clean)
	if err != nil {
		return fmt.Errorf("stat local file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("is directory: %s (use folder upload)", filepath.Base(clean))
	}
	if info.Size() > core.MaxFileTotalSize {
		return fmt.Errorf("file too large (max 2 GiB): %s", filepath.Base(clean))
	}
	name := filepath.Base(clean)
	if _, ok := core.SanitizeFileName(name); !ok {
		// Fallback: sanitize via file name sanitizer, or use generic name
		name = "file"
	}
	remotePath := name
	if remoteDir != "" {
		remotePath = remoteDir + "/" + name
	}
	if _, ok := core.SanitizeFilePath(remotePath); !ok {
		return errors.New("invalid remote path")
	}
	f, err := os.Open(clean)
	if err != nil {
		return fmt.Errorf("open local file: %w", err)
	}
	defer func() { _ = f.Close() }()

	totalSize := info.Size()
	totalChunks := int((totalSize + core.MaxFileChunkRaw - 1) / core.MaxFileChunkRaw)
	if totalSize == 0 {
		totalChunks = 1
	}
	transferID, err := freshTransferID()
	if err != nil {
		return err
	}
	// Compute sha256 for verification (stream already open; compute separately)
	hash, err := fileSHA256(clean)
	if err != nil {
		return fmt.Errorf("hash file: %w", err)
	}

	// Register transfer for progress UI
	s.fileMu.Lock()
	if s.transfers == nil {
		s.transfers = make(map[string]*FileTransfer)
	}
	if s.pendingLists == nil {
		s.pendingLists = make(map[string]chan FileListResult)
	}
	s.transfers[transferID] = &FileTransfer{
		ID: transferID, Path: remotePath, Direction: "upload",
		TotalSize: totalSize, Status: "running",
		sha256: hash,
	}
	s.fileMu.Unlock()
	defer func() {
		s.fileMu.Lock()
		if tr, ok := s.transfers[transferID]; ok && tr.Status == "running" {
			tr.Status = "done"
			tr.DoneSize = totalSize
			tr.completedAt = time.Now()
		}
		s.fileMu.Unlock()
	}()

	buf := make([]byte, core.MaxFileChunkRaw)
	for idx := 0; idx < totalChunks; idx++ {
		offset := int64(idx) * core.MaxFileChunkRaw
		n, readErr := io.ReadFull(f, buf)
		if totalSize == 0 {
			n = 0
		} else if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) && !errors.Is(readErr, io.EOF) {
			s.failTransfer(transferID, readErr.Error())
			return fmt.Errorf("read chunk %d: %w", idx, readErr)
		}
		chunkRaw := buf[:n]
		b64 := ""
		if len(chunkRaw) > 0 {
			b64 = base64.StdEncoding.EncodeToString(chunkRaw)
		}
		sha := ""
		if idx == totalChunks-1 {
			sha = hash
		}
		payload := core.FileChunkPayload{
			TransferID:  transferID,
			Path:        remotePath,
			Offset:      offset,
			TotalSize:   totalSize,
			ChunkIndex:  idx,
			TotalChunks: totalChunks,
			DataB64:     b64,
			Sha256:      sha,
		}
		if err := s.sendFeatureToPhone(core.TypeFileChunk, &payload); err != nil {
			s.failTransfer(transferID, err.Error())
			return fmt.Errorf("send chunk %d: %w", idx, err)
		}
		s.fileMu.Lock()
		if tr, ok := s.transfers[transferID]; ok {
			tr.DoneSize = offset + int64(n)
		}
		s.fileMu.Unlock()
	}
	s.appendLine("file upload done path=" + remotePath)
	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s *Service) failTransfer(id, msg string) {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	if tr, ok := s.transfers[id]; ok {
		tr.Status = "error"
		tr.Error = msg
		tr.completedAt = time.Now()
	}
	if ch, ok := s.transferWaiters[id]; ok {
		select {
		case ch <- errors.New(msg):
		default:
		}
	}
	s.fileMu.Unlock()
	s.emitTransfersChanged()
}

// RequestPhoneFile asks the phone to send a file back chunk-by-chunk.
// downloadDir is a local Mac directory (absolute) to save into; if empty,
// uses system Downloads. Returns transfer id for live progress events.
func (s *Service) RequestPhoneFile(remotePath, downloadDir string) (string, error) {
	if _, ok := core.SanitizeFilePath(remotePath); !ok {
		return "", errors.New("invalid remote path")
	}
	if remotePath == "" {
		return "", errors.New("path must not be empty")
	}
	if !s.IsPaired() {
		return "", errors.New("phone is offline — reconnect first")
	}
	transferID, err := freshTransferID()
	if err != nil {
		return "", err
	}
	// Resolve local target dir safely.
	targetBase := downloadDir
	if strings.TrimSpace(targetBase) == "" {
		home, _ := os.UserHomeDir()
		if home != "" {
			targetBase = filepath.Join(home, "Downloads")
		} else {
			targetBase, _ = stagingRoot()
		}
	}
	targetBase = filepath.Clean(targetBase)
	if err := os.MkdirAll(targetBase, 0o700); err != nil {
		return "", fmt.Errorf("mkdir download dir: %w", err)
	}
	// Register pending download so ingestFileChunk knows where to write.
	baseName := filepath.Base(remotePath)
	s.fileMu.Lock()
	if s.transfers == nil {
		s.transfers = make(map[string]*FileTransfer)
	}
	if s.pendingLists == nil {
		s.pendingLists = make(map[string]chan FileListResult)
	}
	s.transfers[transferID] = &FileTransfer{
		ID: transferID, Path: remotePath, Direction: "download",
		Status: "running", tmpPath: targetBase + "/" + baseName,
	}
	s.fileMu.Unlock()

	payload := core.FilePullReqPayload{Path: remotePath, TransferID: transferID}
	if err := s.sendFeatureToPhone(core.TypeFilePullReq, &payload); err != nil {
		s.failTransfer(transferID, err.Error())
		return "", fmt.Errorf("request file: %w", err)
	}
	s.appendLine("file pull req sent path=" + remotePath)
	return transferID, nil
}

// GetTransfers returns snapshot of active/recent transfers for the UI.
// Completed/errored transfers older than 5 seconds are pruned automatically.
func (s *Service) GetTransfers() []FileTransferView {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	if s.transfers == nil {
		return []FileTransferView{}
	}
	now := time.Now()
	for id, tr := range s.transfers {
		if tr.Status != "running" && !tr.completedAt.IsZero() && now.Sub(tr.completedAt) > 5*time.Second {
			delete(s.transfers, id)
		}
	}
	out := make([]FileTransferView, 0, len(s.transfers))
	for _, tr := range s.transfers {
		prog := 0
		if tr.TotalSize > 0 {
			prog = int(tr.DoneSize * 100 / tr.TotalSize)
		} else if tr.Status == "done" {
			prog = 100
		}
		out = append(out, FileTransferView{
			ID: tr.ID, Path: tr.Path, Direction: tr.Direction,
			Status: tr.Status, Progress: prog,
			TotalSize: tr.TotalSize, DoneSize: tr.DoneSize, Error: tr.Error,
		})
	}
	return out
}

// CancelTransfer marks a transfer cancelled (best-effort, no wire cancel yet).
func (s *Service) CancelTransfer(id string) (string, error) {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	tr, ok := s.transfers[id]
	if !ok {
		return "", errors.New("unknown transfer")
	}
	if tr.Status != "running" {
		return "", errors.New("transfer not running")
	}
	tr.Status = "cancelled"
	tr.completedAt = time.Now()
	if tr.file != nil {
		_ = tr.file.Close()
		tr.file = nil
	}
	if ch, ok := s.transferWaiters[id]; ok {
		select {
		case ch <- errors.New("transfer cancelled"):
		default:
		}
	}
	return "Cancelled.", nil
}

// RevealInFinder opens the staging dir in Finder (fallback for drag-out).
func (s *Service) RevealInFinder(transferID string) (string, error) {
	s.fileMu.Lock()
	tr, ok := s.transfers[transferID]
	s.fileMu.Unlock()
	if !ok {
		return "", errors.New("unknown transfer")
	}
	dir := filepath.Dir(tr.tmpPath)
	if dir == "." || dir == "" {
		dir, _ = stagingRoot()
	}
	// Use open via helper; ignore errors for UI.
	_ = revealPath(dir)
	return dir, nil
}

// DownloadFileToExactPath downloads a phone file directly into exactDestPath on the Mac.
// Used by native drag-out (NSFilePromiseProvider) so Finder receives the file directly
// in the dropped directory (e.g. external drives or custom folders).
func (s *Service) DownloadFileToExactPath(remotePath, exactDestPath string) error {
	if _, ok := core.SanitizeFilePath(remotePath); !ok || remotePath == "" {
		return errors.New("invalid remote path")
	}
	if !s.IsPaired() {
		return errors.New("phone is offline — reconnect first")
	}
	cleanDest := filepath.Clean(exactDestPath)
	if cleanDest == "" || cleanDest == "." {
		return errors.New("invalid destination path")
	}
	destDir := filepath.Dir(cleanDest)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("mkdir destination dir: %w", err)
	}

	transferID, err := freshTransferID()
	if err != nil {
		return fmt.Errorf("generate transfer id: %w", err)
	}

	doneCh := make(chan error, 1)

	s.fileMu.Lock()
	if s.transfers == nil {
		s.transfers = make(map[string]*FileTransfer)
	}
	if s.pendingLists == nil {
		s.pendingLists = make(map[string]chan FileListResult)
	}
	if s.transferWaiters == nil {
		s.transferWaiters = make(map[string]chan error)
	}
	s.transferWaiters[transferID] = doneCh
	s.transfers[transferID] = &FileTransfer{
		ID:        transferID,
		Path:      remotePath,
		Direction: "download",
		Status:    "running",
		tmpPath:   cleanDest,
	}
	s.fileMu.Unlock()

	defer func() {
		s.fileMu.Lock()
		delete(s.transferWaiters, transferID)
		s.fileMu.Unlock()
	}()

	payload := core.FilePullReqPayload{Path: remotePath, TransferID: transferID}
	if err := s.sendFeatureToPhone(core.TypeFilePullReq, &payload); err != nil {
		s.failTransfer(transferID, err.Error())
		return fmt.Errorf("request file: %w", err)
	}
	s.appendLine("file pull req sent for drag path=" + remotePath + " dest=" + cleanDest)

	select {
	case err := <-doneCh:
		return err
	case <-time.After(5 * time.Minute):
		s.failTransfer(transferID, "download timed out")
		return errors.New("download timed out")
	}
}

// PrepareDownloadForDrag returns an existing staged path if already downloaded.
// It no longer triggers unprompted background downloads into ~/Downloads.
func (s *Service) PrepareDownloadForDrag(remotePath string) (string, error) {
	if _, ok := core.SanitizeFilePath(remotePath); !ok || remotePath == "" {
		return "", errors.New("invalid path")
	}
	s.fileMu.Lock()
	for _, tr := range s.transfers {
		if tr.Path == remotePath && tr.Status == "done" && tr.Direction == "download" {
			if _, err := os.Stat(tr.tmpPath); err == nil {
				p := tr.tmpPath
				s.fileMu.Unlock()
				return p, nil
			}
		}
	}
	s.fileMu.Unlock()
	return "", errors.New("file not staged")
}

// PickDownloadDir is a placeholder for native folder picker. Wails v3 dialog is invoked from frontend via window API;
// backend keeps this for compat and simply returns empty meaning "use Downloads".
func (s *Service) PickDownloadDir() (string, error) {
	home, _ := os.UserHomeDir()
	if home != "" {
		return filepath.Join(home, "Downloads"), nil
	}
	return "", nil
}

// UploadBrowserFileWithRelPath uploads a file with relative path (for folder drag via webkitRelativePath).
func (s *Service) UploadBrowserFileWithRelPath(b64, relPath, remoteDir string) (string, error) {
	if strings.TrimSpace(b64) == "" {
		return "", errors.New("empty file")
	}
	if relPath != "" {
		if _, ok := core.SanitizeFilePath(relPath); !ok {
			return "", errors.New("invalid relative path")
		}
		dir := filepath.Dir(relPath)
		if dir != "." && dir != "" {
			// Ensure parent dirs exist on phone.
			base := remoteDir
			if base != "" {
				base = base + "/" + dir
			} else {
				base = dir
			}
			if _, ok := core.SanitizeFilePath(base); ok && base != "" {
				// Create parents recursively (mkdir loop).
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
			filename := filepath.Base(relPath)
			return s.UploadBrowserFile(b64, filename, base)
		}
		return s.UploadBrowserFile(b64, filepath.Base(relPath), remoteDir)
	}
	// Fallback to simple name handling.
	safe := "file"
	if relPath != "" {
		safe = filepath.Base(relPath)
	}
	return s.UploadBrowserFile(b64, safe, remoteDir)
}

// UploadBrowserFile uploads a single file supplied as base64 from the browser
// (drag-n-drop fallback when Finder paths are not available). It chunks the
// decoded bytes exactly like UploadLocalFiles.
func (s *Service) UploadBrowserFile(b64, filename, remoteDir string) (string, error) {
	if strings.TrimSpace(b64) == "" {
		return "", errors.New("empty file")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		raw, err = base64.StdEncoding.WithPadding(base64.StdPadding).DecodeString(strings.TrimSpace(b64))
		if err != nil {
			return "", errors.New("invalid base64")
		}
	}
	if len(raw) > core.MaxFileTotalSize {
		return "", errors.New("file too large")
	}
	if _, ok := core.SanitizeFileName(filename); !ok {
		return "", errors.New("invalid filename")
	}
	remoteSan, ok := core.SanitizeFilePath(remoteDir)
	if !ok {
		return "", errors.New("invalid remote directory")
	}
	if !s.IsPaired() {
		return "", errors.New("phone is offline — reconnect first")
	}
	remotePath := filename
	if remoteSan != "" {
		remotePath = remoteSan + "/" + filename
	}
	if _, ok := core.SanitizeFilePath(remotePath); !ok {
		return "", errors.New("invalid remote path")
	}
	totalSize := int64(len(raw))
	totalChunks := int((totalSize + core.MaxFileChunkRaw - 1) / core.MaxFileChunkRaw)
	if totalSize == 0 {
		totalChunks = 1
	}
	transferID, err := freshTransferID()
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(raw)
	hash := hex.EncodeToString(h[:])
	s.fileMu.Lock()
	if s.transfers == nil {
		s.transfers = make(map[string]*FileTransfer)
	}
	if s.pendingLists == nil {
		s.pendingLists = make(map[string]chan FileListResult)
	}
	s.transfers[transferID] = &FileTransfer{
		ID: transferID, Path: remotePath, Direction: "upload",
		TotalSize: totalSize, Status: "running", sha256: hash,
	}
	s.fileMu.Unlock()
	defer func() {
		s.fileMu.Lock()
		if tr, ok := s.transfers[transferID]; ok && tr.Status == "running" {
			tr.Status = "done"
			tr.DoneSize = totalSize
			tr.completedAt = time.Now()
		}
		s.fileMu.Unlock()
	}()
	for idx := 0; idx < totalChunks; idx++ {
		offset := int64(idx) * core.MaxFileChunkRaw
		end := offset + core.MaxFileChunkRaw
		if end > totalSize {
			end = totalSize
		}
		chunk := raw[offset:end]
		b64chunk := ""
		if len(chunk) > 0 {
			b64chunk = base64.StdEncoding.EncodeToString(chunk)
		}
		sha := ""
		if idx == totalChunks-1 {
			sha = hash
		}
		payload := core.FileChunkPayload{
			TransferID:  transferID,
			Path:        remotePath,
			Offset:      offset,
			TotalSize:   totalSize,
			ChunkIndex:  idx,
			TotalChunks: totalChunks,
			DataB64:     b64chunk,
			Sha256:      sha,
		}
		if err := s.sendFeatureToPhone(core.TypeFileChunk, &payload); err != nil {
			s.failTransfer(transferID, err.Error())
			return "", fmt.Errorf("send chunk %d: %w", idx, err)
		}
		s.fileMu.Lock()
		if tr, ok := s.transfers[transferID]; ok {
			tr.DoneSize = end
		}
		s.fileMu.Unlock()
	}
	s.appendLine("file upload (browser) done path=" + remotePath)
	return "Uploaded " + filename, nil
}

// Ingest helpers called from WrapHandler on accepted /files posts.

func (s *Service) ingestFileBody(body []byte) {
	// Dispatch by type for ingestion. Each case validates and updates state.
	var env struct {
		Type         string          `json:"type"`
		Sender       core.SenderInfo `json:"sender"`
		Capabilities []string        `json:"capabilities"`
		Payload      json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return
	}
	// Accepted bodies already passed the token gate: refresh the cached peer
	// version so a phone that updated mid-session unblocks the file gate.
	s.learnPeer(env.Sender.Platform, env.Sender.AppBuild, env.Sender.AppVersion, env.Capabilities)
	switch env.Type {
	case core.TypeFileList:
		// Phone browses Mac? Not used in v1; ignore.
	case core.TypeFileListResp:
		s.ingestFileListRespBody(body)
	case core.TypeFileChunk:
		s.ingestFileChunkBody(body)
	case core.TypeFilePullReq:
		s.ingestFilePullReqBody(body)
	case core.TypeFileMkdir, core.TypeFileDelete, core.TypeFileRename:
		// Ack already sent; just log.
	}
}

func (s *Service) ingestFileListRespBody(body []byte) {
	var env struct {
		Type    string                   `json:"type"`
		Payload core.FileListRespPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return
	}
	if env.Type != core.TypeFileListResp {
		return
	}
	if !core.SanitizeFileListResp(env.Payload) {
		return
	}
	res := FileListResult{
		Path:       "",
		Error:      env.Payload.Error,
		ErrorCode:  env.Payload.ErrorCode,
		Permission: env.Payload.Permission,
	}
	// Path not in resp; use req_id lookup? For now store entries with path inferred.
	entries := make([]FileEntryView, 0, len(env.Payload.Entries))
	for _, e := range env.Payload.Entries {
		entries = append(entries, FileEntryView{
			Name: e.Name, Path: e.Path, IsDir: e.IsDir, Size: e.Size, ModTime: e.ModTime, Mime: e.Mime,
		})
	}
	res.Entries = entries
	if res.Entries == nil {
		res.Entries = []FileEntryView{}
	}
	// Try to resolve pending req_id.
	s.fileMu.Lock()
	if s.pendingLists == nil {
		s.pendingLists = make(map[string]chan FileListResult)
	}
	if s.transfers == nil {
		s.transfers = make(map[string]*FileTransfer)
	}
	ch, ok := s.pendingLists[env.Payload.ReqID]
	if ok {
		// Also capture path from entries[0]?? Keep as empty, frontend uses breadcrumb.
		select {
		case ch <- res:
		default:
		}
		s.fileMu.Unlock()
		return
	}
	// No waiter: store as lastList for offline cache.
	s.lastList = res
	s.fileMu.Unlock()
	s.appendLine("file list resp received")
}

func (s *Service) ingestFileChunkBody(body []byte) {
	var env struct {
		Type    string               `json:"type"`
		Payload core.FileChunkPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return
	}
	if env.Type != core.TypeFileChunk {
		return
	}
	if !core.SanitizeFileChunk(env.Payload) {
		s.appendLine("file chunk rejected: sanitize failed")
		return
	}
	p := env.Payload

	// Check for cancel.
	s.fileMu.Lock()
	if s.transfers == nil {
		s.transfers = make(map[string]*FileTransfer)
	}
	if s.pendingLists == nil {
		s.pendingLists = make(map[string]chan FileListResult)
	}
	tr, exists := s.transfers[p.TransferID]
	if !exists {
		// New download transfer initiated by phone push or our pull.
		// Create download entry.
		baseName := filepath.Base(p.Path)
		staging, _ := stagingRoot()
		if staging == "" {
			staging = os.TempDir()
		}
		// Use requested transfer's tmpPath if we created it via RequestPhoneFile?
		// For unsolicited pushes, use staging.
		tr = &FileTransfer{
			ID: p.TransferID, Path: p.Path, Direction: "download",
			TotalSize: p.TotalSize, Status: "running",
			tmpPath: filepath.Join(staging, baseName),
		}
		s.transfers[p.TransferID] = tr
	}
	if tr.Status == "cancelled" || tr.Status == "error" {
		s.fileMu.Unlock()
		return
	}
	s.fileMu.Unlock()

	// Decode data
	var raw []byte
	if p.DataB64 != "" {
		var err error
		raw, err = base64.StdEncoding.DecodeString(strings.TrimSpace(p.DataB64))
		if err != nil {
			raw, err = base64.StdEncoding.WithPadding(base64.StdPadding).DecodeString(strings.TrimSpace(p.DataB64))
			if err != nil {
				s.failTransfer(p.TransferID, "bad base64")
				return
			}
		}
	}

	// Write chunk at offset to temp file .part.<id>
	partPath := tr.tmpPath + ".part." + p.TransferID
	// Use os.Root-like scoping: ensure parent exists and is within staging or Downloads.
	// For download validation, ensure tmpPath's dir exists.
	if err := os.MkdirAll(filepath.Dir(partPath), 0o700); err != nil {
		s.failTransfer(p.TransferID, err.Error())
		return
	}
	// Open or create part file.
	f, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		s.failTransfer(p.TransferID, err.Error())
		return
	}
	if _, err := f.Seek(p.Offset, io.SeekStart); err != nil {
		_ = f.Close()
		s.failTransfer(p.TransferID, err.Error())
		return
	}
	if len(raw) > 0 {
		if _, err := f.Write(raw); err != nil {
			_ = f.Close()
			s.failTransfer(p.TransferID, err.Error())
			return
		}
	}
	_ = f.Close()

	s.fileMu.Lock()
	tr.DoneSize = p.Offset + int64(len(raw))
	if p.ChunkIndex == p.TotalChunks-1 {
		// Final chunk: verify sha256 if present, then atomically rename.
		if p.Sha256 != "" {
			if verr := verifySHA256(partPath, p.Sha256, p.TotalSize); verr != nil {
				tr.Status = "error"
				tr.Error = verr.Error()
				tr.completedAt = time.Now()
				if ch, ok := s.transferWaiters[p.TransferID]; ok {
					select {
					case ch <- verr:
					default:
					}
				}
				s.fileMu.Unlock()
				s.appendLine("file download verify failed")
				return
			}
		}
		// Ensure final size matches.
		if fi, err := os.Stat(partPath); err == nil {
			if fi.Size() != p.TotalSize {
				tr.Status = "error"
				tr.Error = "size mismatch"
				tr.completedAt = time.Now()
				if ch, ok := s.transferWaiters[p.TransferID]; ok {
					select {
					case ch <- errors.New("size mismatch"):
					default:
					}
				}
				s.fileMu.Unlock()
				return
			}
		}
		// Atomic rename, handling existing target.
		finalPath := tr.tmpPath
		// If exists with non-zero size, add suffix.
		if fi, err := os.Stat(finalPath); err == nil && fi.Size() > 0 {
			ext := filepath.Ext(finalPath)
			base := strings.TrimSuffix(finalPath, ext)
			finalPath = fmt.Sprintf("%s-%s%s", base, p.TransferID[:6], ext)
			tr.tmpPath = finalPath
		}
		if err := os.Rename(partPath, finalPath); err != nil {
			tr.Status = "error"
			tr.Error = err.Error()
			tr.completedAt = time.Now()
			if ch, ok := s.transferWaiters[p.TransferID]; ok {
				select {
				case ch <- err:
				default:
				}
			}
			s.fileMu.Unlock()
			return
		}
		tr.Status = "done"
		tr.completedAt = time.Now()
		if ch, ok := s.transferWaiters[p.TransferID]; ok {
			select {
			case ch <- nil:
			default:
			}
		}
		s.fileMu.Unlock()
		s.appendLine("file download done path=" + finalPath)
		return
	}
	tr.Status = "running"
	s.fileMu.Unlock()
}

func verifySHA256(path, wantHex string, totalSize int64) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, wantHex) {
		return fmt.Errorf("sha256 mismatch")
	}
	return nil
}

func (s *Service) ingestFilePullReqBody(body []byte) {
	var env struct {
		Type    string                  `json:"type"`
		Payload core.FilePullReqPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return
	}
	if env.Type != core.TypeFilePullReq {
		return
	}
	if !core.SanitizeFilePullReq(env.Payload) {
		return
	}
	// For v1, Mac does not serve files to phone via pull-req (phone pulls).
	// Stub: log and ignore. Full impl would stream local file back in chunks.
	s.appendLine("file pull req received path=" + env.Payload.Path)
}

// Parse helpers exported for tests / ingestion symmetry.

func ParseFileList(body []byte) (core.FileListPayload, bool) {
	var env struct {
		Type    string              `json:"type"`
		Payload core.FileListPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return core.FileListPayload{}, false
	}
	if env.Type != core.TypeFileList {
		return core.FileListPayload{}, false
	}
	if !core.SanitizeFileList(env.Payload) {
		return core.FileListPayload{}, false
	}
	return env.Payload, true
}

func ParseFileListResp(body []byte) (core.FileListRespPayload, bool) {
	var env struct {
		Type    string                  `json:"type"`
		Payload core.FileListRespPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return core.FileListRespPayload{}, false
	}
	if env.Type != core.TypeFileListResp {
		return core.FileListRespPayload{}, false
	}
	if !core.SanitizeFileListResp(env.Payload) {
		return core.FileListRespPayload{}, false
	}
	return env.Payload, true
}

func ParseFileChunk(body []byte) (core.FileChunkPayload, bool) {
	var env struct {
		Type    string               `json:"type"`
		Payload core.FileChunkPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return core.FileChunkPayload{}, false
	}
	if env.Type != core.TypeFileChunk {
		return core.FileChunkPayload{}, false
	}
	if !core.SanitizeFileChunk(env.Payload) {
		return core.FileChunkPayload{}, false
	}
	return env.Payload, true
}

// revealPath tries to open Finder at path (best-effort).
func revealPath(path string) error {
	return exec.Command("open", path).Run()
}
