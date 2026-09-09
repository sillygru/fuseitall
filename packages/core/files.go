// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Package core file payloads: browsing and chunked transfer. Every type rides
// the Envelope contract (packages/proto/files.json); decoders ignore unknown
// fields. Paths are sandboxed rel paths: no absolute, no .., printable UTF-8
// per component (denylist: /, \, NUL, control), bounded. Chunks are 1 MiB raw
// max to stay under MaxBodyBytes.
package core

import (
	"encoding/base64"
	"encoding/hex"
	"path/filepath"
	"strings"
)

const (
	TypeFileList     = "file-list"
	TypeFileListResp = "file-list-resp"
	TypeFileMkdir    = "file-mkdir"
	TypeFileDelete   = "file-delete"
	TypeFileRename   = "file-rename"
	TypeFileChunk    = "file-chunk"
	TypeFilePullReq  = "file-pull-req"
	TypeFileAck      = "file-ack"
	TypeFileCancel   = "file-cancel"
	TypeFileStatReq  = "file-stat-req"
	TypeFileStatResp = "file-stat-resp"

	CapabilityFiles = "files"
	// CapabilityFilesAck is advertised by receivers that commit uploads with
	// sha256 verification and confirm via file-ack. Senders treat its absence
	// as legacy fire-and-forget (done after the last chunk send).
	CapabilityFilesAck = "files-ack"
)

// Upload conflict policies for file-chunk. Only Overwrite and IfNewer ride
// the wire; Skip, KeepBoth, and Stop are sender-side only (skip/stop send
// nothing, keep-both resolves to a fresh path before sending).
const (
	FilePolicyOverwrite = "overwrite"
	FilePolicyIfNewer   = "if_newer"
	FilePolicySkip      = "skip"
	FilePolicyKeepBoth  = "keep_both"
	FilePolicyStop      = "stop"
)

const (
	MaxFilePathLen    = 1024
	MaxFileNameLen    = 255
	MaxFilesPerList   = 500
	MaxFileChunkRaw   = 1 << 20 // 1 MiB raw per chunk
	MaxFileChunkB64Len = 1850000 // ~ 4*ceil(1MiB/3) + margin
	MaxFileTotalSize  = 8 << 30 // 8 GiB soft cap
	MaxFileTransferIDLen = 64
	MinFileTransferIDLen = 16
)

// FileEntry is one row in a directory listing. Path is sandboxed rel path
// from the listing root (empty root means device root). Mime is optional.
type FileEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path,omitempty"`
	IsDir     bool   `json:"is_dir"`
	Size      int64  `json:"size,omitempty"`
	ModTime   int64  `json:"mod_time,omitempty"`
	Mime      string `json:"mime,omitempty"`
}

// FileListPayload requests a directory listing. Path is sandboxed rel path
// (empty = root). ReqID correlates request -> file-list-resp.
type FileListPayload struct {
	Nonce string `json:"nonce"`
	ReqID string `json:"req_id"`
	Path  string `json:"path,omitempty"`
}

// FileListRespPayload is the reply to a FileListPayload. Entries is bounded
// to MaxFilesPerList; Error is set when listing failed (not found, not dir).
// ErrorCode and Permission provide machine-readable permission separation
// (permission_denied + permission=files vs photos) so adapters can render
// per-viewer empty states without string matching.
type FileListRespPayload struct {
	Nonce      string      `json:"nonce"`
	ReqID      string      `json:"req_id"`
	Entries    []FileEntry `json:"entries,omitempty"`
	Error      string      `json:"error,omitempty"`
	ErrorCode  string      `json:"error_code,omitempty"`
	Permission string      `json:"permission,omitempty"`
}

// FileMkdirPayload creates a directory.
type FileMkdirPayload struct {
	Nonce string `json:"nonce"`
	Path  string `json:"path"`
}

// FileDeletePayload deletes a file or empty directory.
type FileDeletePayload struct {
	Nonce string `json:"nonce"`
	Path  string `json:"path"`
}

// FileRenamePayload renames a file or directory within the sandbox.
// From and To must both be valid rel paths, To's parent must exist.
type FileRenamePayload struct {
	Nonce string `json:"nonce"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// FileChunkPayload carries one chunk of a file. TransferID groups chunks of
// one file; Offset must equal ChunkIndex*chunkSize (validated by receiver by
// ordering). DataB64 is base64 of raw bytes (0..1 MiB raw). Sha256 is optional
// hex sha256 of the full file, sent on last chunk for end-to-end verification.
// Policy is the sender conflict intent: "" (legacy keep-both), "overwrite",
// or "if_newer". SourceMtime is the sender mtime in unix seconds, used only
// with if_newer; it is truncated to seconds to match file-list mod_time.
type FileChunkPayload struct {
	Nonce       string `json:"nonce"`
	TransferID  string `json:"transfer_id"`
	Path        string `json:"path"`
	Offset      int64  `json:"offset"`
	TotalSize   int64  `json:"total_size"`
	ChunkIndex  int    `json:"chunk_index"`
	TotalChunks int    `json:"total_chunks"`
	DataB64     string `json:"data_b64,omitempty"`
	Sha256      string `json:"sha256,omitempty"`
	Policy      string `json:"policy,omitempty"`
	SourceMtime int64  `json:"source_mtime,omitempty"`
}

// FilePullReqPayload requests a file be sent back chunk-by-chunk (phone -> Mac
// or vice versa).
type FilePullReqPayload struct {
	Nonce      string `json:"nonce"`
	TransferID string `json:"transfer_id,omitempty"`
	Path       string `json:"path"`
}

// FileAckPayload confirms one upload transfer after the final chunk commits.
// It rides the /files lane receiver -> sender. OK with empty Error means the
// bytes are on disk and verified; !OK carries a short machine-ish reason.
type FileAckPayload struct {
	Nonce      string `json:"nonce"`
	TransferID string `json:"transfer_id"`
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
}

// SanitizeFilePath validates a sandboxed rel path. Empty means root (allowed).
// Denylist: rejects NUL, \, absolute, traversal (..), empty components,
// control bytes (<0x20, 0x7f), per-component >255, total >1024.
// Allows printable UTF-8 including spaces, parentheses, etc. Pure.
func SanitizeFilePath(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", true
	}
	if len(trimmed) > MaxFilePathLen {
		return "", false
	}
	if strings.Contains(trimmed, "\\") || strings.Contains(trimmed, "\x00") {
		return "", false
	}
	if filepath.IsAbs(trimmed) {
		return "", false
	}
	for _, r := range trimmed {
		if r == 0 || r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	cleaned := filepath.Clean(trimmed)
	if cleaned == "." {
		return "", true
	}
	if !filepath.IsLocal(cleaned) {
		return "", false
	}
	if strings.HasPrefix(cleaned, "..") {
		return "", false
	}
	parts := strings.Split(cleaned, "/")
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			return "", false
		}
		if len(p) > MaxFileNameLen {
			return "", false
		}
		if strings.Contains(p, "\\") || strings.Contains(p, "\x00") {
			return "", false
		}
		for _, r := range p {
			if r == 0 || r < 0x20 || r == 0x7f {
				return "", false
			}
		}
	}
	return cleaned, true
}

// SanitizeFileName validates a single basename (no slash). Denylist mirror.
// Pure.
func SanitizeFileName(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || len(trimmed) > MaxFileNameLen {
		return "", false
	}
	if strings.Contains(trimmed, "/") || strings.Contains(trimmed, "\\") || strings.Contains(trimmed, "\x00") {
		return "", false
	}
	if !filepath.IsLocal(trimmed) {
		return "", false
	}
	if trimmed == "." || trimmed == ".." {
		return "", false
	}
	for _, r := range trimmed {
		if r == 0 || r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	return trimmed, true
}

// SanitizeTransferID validates a hex transfer id (16..64 hex chars). Pure.
func SanitizeTransferID(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", false
	}
	l := len(trimmed)
	if l < MinFileTransferIDLen || l > MaxFileTransferIDLen {
		return "", false
	}
	if l%2 != 0 {
		return "", false
	}
	if _, err := hex.DecodeString(trimmed); err != nil {
		return "", false
	}
	return strings.ToLower(trimmed), true
}

// SanitizeFileChunk validates a chunk payload. Pure: checks paths, sizes,
// offsets, chunk indices, b64 shape and raw cap, sha256 hex when present.
func SanitizeFileChunk(p FileChunkPayload) bool {
	if p.Nonce == "" {
		return false
	}
	if _, ok := SanitizeTransferID(p.TransferID); !ok {
		return false
	}
	if _, ok := SanitizeFilePath(p.Path); !ok {
		return false
	}
	if p.Path == "" {
		return false
	}
	if p.TotalSize < 0 || p.TotalSize > MaxFileTotalSize {
		return false
	}
	if p.Offset < 0 || p.Offset > p.TotalSize {
		return false
	}
	if p.ChunkIndex < 0 || p.TotalChunks < 1 {
		return false
	}
	if p.ChunkIndex >= p.TotalChunks {
		return false
	}
	// TotalChunks must be consistent with TotalSize and chunk size.
	expectedChunks := int((p.TotalSize + MaxFileChunkRaw - 1) / MaxFileChunkRaw)
	if p.TotalSize == 0 {
		expectedChunks = 1
	}
	if p.TotalChunks != expectedChunks {
		return false
	}
	// Last chunk may be smaller; non-last must be exactly MaxFileChunkRaw
	// except when total_size < chunk size.
	if p.ChunkIndex < p.TotalChunks-1 {
		// Non-last: offset must be chunk_index * MaxFileChunkRaw
		if p.Offset != int64(p.ChunkIndex)*MaxFileChunkRaw {
			return false
		}
		if len(p.DataB64) == 0 {
			return false
		}
	} else {
		// Last: offset must be chunk_index * chunkSize
		if p.Offset != int64(p.ChunkIndex)*MaxFileChunkRaw {
			return false
		}
		// Data may be 0..MaxFileChunkRaw
	}
	if len(p.DataB64) > MaxFileChunkB64Len {
		return false
	}
	if p.DataB64 != "" {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(p.DataB64))
		if err != nil {
			raw, err = base64.StdEncoding.WithPadding(base64.StdPadding).DecodeString(strings.TrimSpace(p.DataB64))
			if err != nil {
				return false
			}
		}
		if len(raw) > MaxFileChunkRaw {
			return false
		}
		// Verify raw length matches expectation for this chunk.
		var expectedRaw int
		if p.ChunkIndex < p.TotalChunks-1 {
			expectedRaw = MaxFileChunkRaw
		} else {
			expectedRaw = int(p.TotalSize - p.Offset)
		}
		if len(raw) != expectedRaw {
			return false
		}
	} else {
		// Empty data only valid for zero-byte file (total_size 0, 1 chunk).
		if !(p.TotalSize == 0 && p.TotalChunks == 1 && p.Offset == 0) {
			return false
		}
	}
	if p.Sha256 != "" {
		if len(p.Sha256) != 64 {
			return false
		}
		if _, err := hex.DecodeString(p.Sha256); err != nil {
			return false
		}
		// Sha256 only on last chunk.
		if p.ChunkIndex != p.TotalChunks-1 {
			return false
		}
	}
	if !SanitizeFilePolicy(p.Policy) {
		return false
	}
	if p.SourceMtime < 0 {
		return false
	}
	// Policy and source_mtime ride every chunk consistently; receivers use
	// the last chunk's values. No per-chunk consistency check needed beyond
	// the shared transfer_id+offset idempotency.
	return true
}

// SanitizeFilePolicy validates a wire conflict policy. Empty means legacy
// keep-both (receiver suffixes a copy). Only overwrite and if_newer ride
// the wire; skip/keep_both/stop are sender-side only and must never be sent.
func SanitizeFilePolicy(p string) bool {
	switch p {
	case "", FilePolicyOverwrite, FilePolicyIfNewer:
		return true
	default:
		return false
	}
}

// IsSourceNewer reports whether the sender copy should replace the target
// under the if_newer policy. Comparison is mtime seconds first (matching the
// file-list mod_time granularity), with size as tiebreak: equal mtime plus
// different size counts as newer (content differs), equal mtime plus equal
// size counts as same (skip, avoiding a redundant re-upload; the last-chunk
// sha256 still guards integrity when an upload does run). Pure.
func IsSourceNewer(sourceMtime, targetMtime, sourceSize, targetSize int64) bool {
	if sourceMtime != targetMtime {
		return sourceMtime > targetMtime
	}
	return sourceSize != targetSize
}

// KeepBothName derives a non-colliding sibling for remotePath given the
// existing names in the target directory. It mirrors Finder numbering:
// "photo.png" -> "photo (2).png" -> "photo (3).png". Extension handling
// splits on the last dot; dotfiles keep their leading dot. Pure.
func KeepBothName(remotePath string, existing map[string]struct{}) string {
	if _, taken := existing[remotePath]; !taken {
		return remotePath
	}
	ext := ""
	base := remotePath
	if idx := strings.LastIndex(remotePath, "/"); idx >= 0 {
		dir := remotePath[:idx]
		file := remotePath[idx+1:]
		dot := strings.LastIndex(file, ".")
		if dot > 0 {
			ext = file[dot:]
			base = dir + "/" + file[:dot]
		} else {
			base = remotePath
		}
	} else {
		if dot := strings.LastIndex(remotePath, "."); dot > 0 {
			ext = remotePath[dot:]
			base = remotePath[:dot]
		}
	}
	for i := 2; ; i++ {
		candidate := base + " (" + itoa(i) + ")" + ext
		if _, taken := existing[candidate]; !taken {
			return candidate
		}
	}
}

// itoa is a tiny int formatter avoiding strconv import churn in this file.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// SanitizeFileList validates a file-list request. Pure.
func SanitizeFileList(p FileListPayload) bool {
	if p.Nonce == "" || strings.TrimSpace(p.ReqID) == "" {
		return false
	}
	if len(p.ReqID) > 64 {
		return false
	}
	if _, ok := SanitizeFilePath(p.Path); !ok {
		return false
	}
	return true
}

// SanitizeFileListResp validates a file-list response. Pure.
func SanitizeFileListResp(p FileListRespPayload) bool {
	if p.Nonce == "" || strings.TrimSpace(p.ReqID) == "" {
		return false
	}
	if len(p.ReqID) > 64 {
		return false
	}
	if len(p.Entries) > MaxFilesPerList {
		return false
	}
	for _, e := range p.Entries {
		if e.Name == "" || len(e.Name) > MaxFileNameLen {
			return false
		}
		if _, ok := SanitizeFileName(e.Name); !ok {
			return false
		}
		if e.Path != "" {
			if _, ok := SanitizeFilePath(e.Path); !ok {
				return false
			}
		}
		if e.Size < 0 || e.Size > MaxFileTotalSize {
			return false
		}
		if e.ModTime < 0 {
			return false
		}
	}
	if len(p.Error) > 512 {
		return false
	}
	if !SanitizeErrorCode(p.ErrorCode) {
		return false
	}
	if !SanitizePermission(p.Permission) {
		return false
	}
	return true
}

// SanitizeFileMkdir validates a mkdir payload. Pure.
func SanitizeFileMkdir(p FileMkdirPayload) bool {
	if p.Nonce == "" {
		return false
	}
	if _, ok := SanitizeFilePath(p.Path); !ok {
		return false
	}
	if p.Path == "" {
		return false
	}
	return true
}

// SanitizeFileDelete validates a delete payload. Pure.
func SanitizeFileDelete(p FileDeletePayload) bool {
	if p.Nonce == "" {
		return false
	}
	if _, ok := SanitizeFilePath(p.Path); !ok {
		return false
	}
	if p.Path == "" {
		return false
	}
	return true
}

// SanitizeFileRename validates a rename payload. Pure.
func SanitizeFileRename(p FileRenamePayload) bool {
	if p.Nonce == "" {
		return false
	}
	if _, ok := SanitizeFilePath(p.From); !ok {
		return false
	}
	if p.From == "" {
		return false
	}
	if _, ok := SanitizeFilePath(p.To); !ok {
		return false
	}
	if p.To == "" {
		return false
	}
	if p.From == p.To {
		return false
	}
	return true
}

// SanitizeFilePullReq validates a pull request. Pure.
func SanitizeFilePullReq(p FilePullReqPayload) bool {
	if p.Nonce == "" {
		return false
	}
	if _, ok := SanitizeFilePath(p.Path); !ok {
		return false
	}
	if p.Path == "" {
		return false
	}
	if p.TransferID != "" {
		if _, ok := SanitizeTransferID(p.TransferID); !ok {
			return false
		}
	}
	return true
}

// SanitizeFileAck validates a delivery confirmation. Pure: a well-formed ack
// names its transfer; !OK should carry a short reason (unchecked here beyond
// the length cap so receivers stay liberal).
func SanitizeFileAck(p FileAckPayload) bool {
	if p.Nonce == "" {
		return false
	}
	if _, ok := SanitizeTransferID(p.TransferID); !ok {
		return false
	}
	if len(p.Error) > 512 {
		return false
	}
	return true
}

// FileCancelPayload aborts one transfer; the receiver discards staged bytes.
// Path is an optional locator hint (sandboxed rel path); empty is valid and
// simply discards nothing the receiver cannot locate.
type FileCancelPayload struct {
	Nonce      string `json:"nonce"`
	TransferID string `json:"transfer_id"`
	Path       string `json:"path,omitempty"`
}

// SanitizeFileCancel validates a cancel. Pure.
func SanitizeFileCancel(p FileCancelPayload) bool {
	if p.Nonce == "" {
		return false
	}
	if _, ok := SanitizeTransferID(p.TransferID); !ok {
		return false
	}
	if p.Path != "" {
		if _, ok := SanitizeFilePath(p.Path); !ok {
			return false
		}
	}
	return true
}

// FileStatReqPayload asks the receiver which chunks of a staged upload it
// already holds, so a retry sends only the missing tail.
type FileStatReqPayload struct {
	Nonce      string `json:"nonce"`
	TransferID string `json:"transfer_id"`
}

// FileStatRespPayload answers with the smallest missing chunk index.
// NextChunk == TotalChunks means nothing is missing. Error names an unknown
// or expired transfer; the sender then restarts from zero.
type FileStatRespPayload struct {
	Nonce       string `json:"nonce"`
	TransferID  string `json:"transfer_id"`
	NextChunk   int    `json:"next_chunk"`
	TotalChunks int    `json:"total_chunks"`
	Error       string `json:"error,omitempty"`
}

// SanitizeFileStatReq validates a stat request. Pure.
func SanitizeFileStatReq(p FileStatReqPayload) bool {
	if p.Nonce == "" {
		return false
	}
	if _, ok := SanitizeTransferID(p.TransferID); !ok {
		return false
	}
	return true
}

// SanitizeFileStatResp validates a stat response. Pure.
func SanitizeFileStatResp(p FileStatRespPayload) bool {
	if p.Nonce == "" {
		return false
	}
	if _, ok := SanitizeTransferID(p.TransferID); !ok {
		return false
	}
	if p.NextChunk < 0 || p.TotalChunks < 1 || p.NextChunk > p.TotalChunks {
		return false
	}
	if len(p.Error) > 512 {
		return false
	}
	return true
}
