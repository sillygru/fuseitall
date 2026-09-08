// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Package core photo payloads: library browsing with separate permission
// reporting. Every type rides the Envelope contract (packages/proto/photos.json);
// decoders ignore unknown fields. Photo IDs are opaque MediaStore row IDs:
// legacy pure digits (image) or namespaced img:<row> / vid:<row>
// (1..128, no slash, printable). Build 8 adds video: entries carry
// media_type photo|video (absent = photo) and duration_ms. Thumbs are
// fetched via separate photo-thumb-req/resp (video thumbs are JPEG frame
// grabs) so one bad thumb never fails a listing page.
// Full-res uses dedicated photo-chunk (isolated from file-chunk) with
// transfer_id+absolute-offset idempotency and sha256 on full-file last
// chunk. photo-pull-req accepts optional offset/length for range streaming
// (0 = legacy full pull); range chunks reuse absolute chunk_index/
// total_chunks so each chunk validates standalone under SanitizePhotoChunk.
package core

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
)

const (
	TypePhotoList       = "photo-list"
	TypePhotoListResp   = "photo-list-resp"
	TypePhotoThumbReq   = "photo-thumb-req"
	TypePhotoThumbResp  = "photo-thumb-resp"
	TypePhotoPullReq    = "photo-pull-req"
	TypePhotoChunk      = "photo-chunk"
	TypePhotoDelete     = "photo-delete"
	TypePhotoDeleteResp = "photo-delete-resp"

	CapabilityPhotos = "photos"

	PermissionPhotos = "photos"
	PermissionFiles  = "files"
)

const (
	MaxPhotoIDLen         = 128
	MaxPhotosPerList      = 200
	MaxPhotoThumbB64Len   = 2000000
	MaxPhotoDeleteBatch   = 200
	MaxPhotoChunkB64Len   = 1850000
	MinPhotoThumbSize     = 64
	MaxPhotoThumbSize     = 1024
	DefaultPhotoListLimit = 100
	// MaxVideoDurationMs bounds duration_ms (24h). 0 = unknown.
	MaxVideoDurationMs = 24 * 3600 * 1000
	// MaxPhotoRangeLen caps one range pull so a single range stays well
	// under MaxBodyBytes once chunked (32 MiB = 32 chunks of 1 MiB raw).
	MaxPhotoRangeLen = 32 << 20
)

const (
	// MediaTypePhoto is a still image (also the default when media_type is
	// absent for pre-0.8.0 peers).
	MediaTypePhoto = "photo"
	// MediaTypeVideo is a video; entry carries duration_ms and a video mime.
	MediaTypeVideo = "video"
)

const (
	ErrorCodePermissionDenied = "permission_denied"
	ErrorCodeNotFound         = "not_found"
	ErrorCodeInvalidArg       = "invalid_arg"
	ErrorCodeInternal         = "internal"
)

// PhotoEntry is one row in a paged photo listing. PhotoID is opaque
// MediaStore row ID (legacy digits, or img:<row> / vid:<row>); TakenAt is
// unix millis (DATE_TAKEN or DATE_MODIFIED fallback). Mime is image or
// video mime, Size is bytes. MediaType is photo|video ("" = photo for
// pre-0.8.0 peers); DurationMs is video millis, 0 = unknown.
type PhotoEntry struct {
	PhotoID     string `json:"photo_id"`
	TakenAt     int64  `json:"taken_at"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	Mime        string `json:"mime,omitempty"`
	Size        int64  `json:"size,omitempty"`
	Orientation int    `json:"orientation,omitempty"`
	MediaType   string `json:"media_type,omitempty"`
	DurationMs  int64  `json:"duration_ms,omitempty"`
}

// IsVideo reports whether the entry is a video. Pure.
func (e PhotoEntry) IsVideo() bool {
	return e.MediaType == MediaTypeVideo
}

// PhotoListPayload requests a paged listing. Cursor empty = first page.
// Limit 1..200 (default 100). ReqID correlates request -> resp.
type PhotoListPayload struct {
	Nonce  string `json:"nonce"`
	ReqID  string `json:"req_id"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// PhotoListRespPayload is the reply to a PhotoListPayload.
type PhotoListRespPayload struct {
	Nonce      string       `json:"nonce"`
	ReqID      string       `json:"req_id"`
	Entries    []PhotoEntry `json:"entries,omitempty"`
	NextCursor string       `json:"next_cursor,omitempty"`
	Error      string       `json:"error,omitempty"`
	ErrorCode  string       `json:"error_code,omitempty"`
	Permission string       `json:"permission,omitempty"`
}

// PhotoThumbReqPayload requests one thumbnail.
type PhotoThumbReqPayload struct {
	Nonce     string `json:"nonce"`
	ReqID     string `json:"req_id"`
	PhotoID   string `json:"photo_id"`
	ThumbSize int    `json:"thumb_size,omitempty"`
}

// PhotoThumbRespPayload is the reply to a thumb request.
type PhotoThumbRespPayload struct {
	Nonce      string `json:"nonce"`
	ReqID      string `json:"req_id"`
	PhotoID    string `json:"photo_id"`
	Mime       string `json:"mime,omitempty"`
	DataB64    string `json:"data_b64,omitempty"`
	Error      string `json:"error,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	Permission string `json:"permission,omitempty"`
}

// PhotoPullReqPayload requests a full-res photo be streamed via photo-chunk.
// Offset/Length request a byte range for video streaming (0 = legacy full
// pull). Range chunks reuse absolute offsets and chunk_index/total_chunks
// over the full file so each chunk validates standalone.
type PhotoPullReqPayload struct {
	Nonce      string `json:"nonce"`
	TransferID string `json:"transfer_id,omitempty"`
	PhotoID    string `json:"photo_id"`
	Offset     int64  `json:"offset,omitempty"`
	Length     int64  `json:"length,omitempty"`
}

// PhotoChunkPayload carries one chunk of a full-res photo. TransferID groups
// chunks of one photo; Offset must equal ChunkIndex*chunkSize. DataB64 is
// base64 of raw bytes (0..1 MiB raw). Sha256 is optional hex sha256 of the
// full file, sent on last chunk. Mime is set on first chunk for type hint.
type PhotoChunkPayload struct {
	Nonce       string `json:"nonce"`
	TransferID  string `json:"transfer_id"`
	PhotoID     string `json:"photo_id"`
	Offset      int64  `json:"offset"`
	TotalSize   int64  `json:"total_size"`
	ChunkIndex  int    `json:"chunk_index"`
	TotalChunks int    `json:"total_chunks"`
	DataB64     string `json:"data_b64,omitempty"`
	Sha256      string `json:"sha256,omitempty"`
	Mime        string `json:"mime,omitempty"`
}

// PhotoDeletePayload requests deletion of a batch of photos.
type PhotoDeletePayload struct {
	Nonce    string   `json:"nonce"`
	ReqID    string   `json:"req_id"`
	PhotoIDs []string `json:"photo_ids"`
}

// PhotoDeleteResult is per-item delete outcome for partial success.
type PhotoDeleteResult struct {
	PhotoID   string `json:"photo_id"`
	Ok        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

// PhotoDeleteRespPayload is the reply to a delete batch.
type PhotoDeleteRespPayload struct {
	Nonce      string              `json:"nonce"`
	ReqID      string              `json:"req_id"`
	Results    []PhotoDeleteResult `json:"results,omitempty"`
	Error      string              `json:"error,omitempty"`
	ErrorCode  string              `json:"error_code,omitempty"`
	Permission string              `json:"permission,omitempty"`
}

// SanitizePhotoID validates an opaque MediaStore row ID. Pure.
func SanitizePhotoID(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || len(trimmed) > MaxPhotoIDLen {
		return "", false
	}
	if strings.Contains(trimmed, "/") || strings.Contains(trimmed, "\\") || strings.Contains(trimmed, "\x00") {
		return "", false
	}
	for _, r := range trimmed {
		if r == 0 || r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	if trimmed == "." || trimmed == ".." {
		return "", false
	}
	return trimmed, true
}

// ParsePhotoID splits a sanitized photo ID into kind and MediaStore row ID.
// Legacy pure-digit IDs are images. Namespaced img:<row> / vid:<row> select
// the collection; the row must be 1..128 chars, no slash. Pure: returns
// ("", "", false) for anything else (callers already ran SanitizePhotoID).
func ParsePhotoID(s string) (kind, row string, ok bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || len(trimmed) > MaxPhotoIDLen {
		return "", "", false
	}
	if rest, found := strings.CutPrefix(trimmed, "img:"); found {
		if rest == "" || len(rest) > MaxPhotoIDLen || strings.ContainsAny(rest, "/\\\x00:") {
			return "", "", false
		}
		if _, ok := SanitizePhotoID(rest); !ok {
			return "", "", false
		}
		return MediaTypePhoto, rest, true
	}
	if rest, found := strings.CutPrefix(trimmed, "vid:"); found {
		if rest == "" || len(rest) > MaxPhotoIDLen || strings.ContainsAny(rest, "/\\\x00:") {
			return "", "", false
		}
		if _, ok := SanitizePhotoID(rest); !ok {
			return "", "", false
		}
		return MediaTypeVideo, rest, true
	}
	if _, ok := SanitizePhotoID(trimmed); !ok {
		return "", "", false
	}
	return MediaTypePhoto, trimmed, true
}

// SanitizeMediaType validates a media_type value ("" = legacy photo). Pure.
func SanitizeMediaType(s string) bool {
	return s == "" || s == MediaTypePhoto || s == MediaTypeVideo
}

// SanitizeErrorCode validates a machine error code. Pure.
func SanitizeErrorCode(s string) bool {
	if s == "" {
		return true
	}
	if len(s) > 64 {
		return false
	}
	switch strings.TrimSpace(s) {
	case ErrorCodePermissionDenied, ErrorCodeNotFound, ErrorCodeInvalidArg, ErrorCodeInternal:
		return true
	default:
		return false
	}
}

// SanitizePermission validates a permission domain. Pure.
func SanitizePermission(s string) bool {
	if s == "" {
		return true
	}
	trimmed := strings.TrimSpace(s)
	return trimmed == PermissionPhotos || trimmed == PermissionFiles
}

// SanitizePhotoList validates a photo-list request. Pure.
func SanitizePhotoList(p PhotoListPayload) bool {
	if p.Nonce == "" || strings.TrimSpace(p.ReqID) == "" {
		return false
	}
	if len(p.ReqID) > 64 {
		return false
	}
	if len(p.Cursor) > 256 {
		return false
	}
	if p.Limit != 0 && (p.Limit < 1 || p.Limit > MaxPhotosPerList) {
		return false
	}
	return true
}

// SanitizePhotoListResp validates a photo-list response. Pure.
func SanitizePhotoListResp(p PhotoListRespPayload) bool {
	if p.Nonce == "" || strings.TrimSpace(p.ReqID) == "" {
		return false
	}
	if len(p.ReqID) > 64 {
		return false
	}
	if len(p.Entries) > MaxPhotosPerList {
		return false
	}
	for _, e := range p.Entries {
		if _, ok := SanitizePhotoID(e.PhotoID); !ok {
			return false
		}
		if e.TakenAt < 0 {
			return false
		}
		if e.Size < 0 || e.Size > MaxFileTotalSize {
			return false
		}
		if e.Width < 0 || e.Height < 0 {
			return false
		}
		if e.Mime != "" && len(e.Mime) > 64 {
			return false
		}
		if !SanitizeMediaType(e.MediaType) {
			return false
		}
		if e.DurationMs < 0 || e.DurationMs > MaxVideoDurationMs {
			return false
		}
	}
	if len(p.NextCursor) > 256 {
		return false
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

// SanitizePhotoThumbReq validates a thumb request. Pure.
func SanitizePhotoThumbReq(p PhotoThumbReqPayload) bool {
	if p.Nonce == "" || strings.TrimSpace(p.ReqID) == "" {
		return false
	}
	if len(p.ReqID) > 64 {
		return false
	}
	if _, ok := SanitizePhotoID(p.PhotoID); !ok {
		return false
	}
	if p.ThumbSize != 0 && (p.ThumbSize < MinPhotoThumbSize || p.ThumbSize > MaxPhotoThumbSize) {
		return false
	}
	return true
}

// SanitizePhotoThumbResp validates a thumb response. Pure.
func SanitizePhotoThumbResp(p PhotoThumbRespPayload) bool {
	if p.Nonce == "" || strings.TrimSpace(p.ReqID) == "" {
		return false
	}
	if len(p.ReqID) > 64 {
		return false
	}
	if _, ok := SanitizePhotoID(p.PhotoID); !ok {
		return false
	}
	if len(p.DataB64) > MaxPhotoThumbB64Len {
		return false
	}
	if p.DataB64 != "" {
		if _, err := base64.StdEncoding.DecodeString(strings.TrimSpace(p.DataB64)); err != nil {
			return false
		}
	}
	if p.Mime != "" && len(p.Mime) > 64 {
		return false
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

// SanitizePhotoPullReq validates a pull request. Offset/Length select a byte
// range for video streaming; both zero means a legacy full pull. Pure.
func SanitizePhotoPullReq(p PhotoPullReqPayload) bool {
	if p.Nonce == "" {
		return false
	}
	if _, ok := SanitizePhotoID(p.PhotoID); !ok {
		return false
	}
	if p.TransferID != "" {
		if _, ok := SanitizeTransferID(p.TransferID); !ok {
			return false
		}
	}
	if p.Offset < 0 || p.Offset > MaxFileTotalSize {
		return false
	}
	if p.Length < 0 || p.Length > MaxFileTotalSize {
		return false
	}
	if p.Length > 0 {
		if p.Length > MaxPhotoRangeLen {
			return false
		}
		if p.Offset > MaxFileTotalSize-p.Length {
			return false
		}
	}
	return true
}

// SanitizePhotoChunk validates a photo chunk payload. Pure.
func SanitizePhotoChunk(p PhotoChunkPayload) bool {
	if p.Nonce == "" {
		return false
	}
	if _, ok := SanitizeTransferID(p.TransferID); !ok {
		return false
	}
	if _, ok := SanitizePhotoID(p.PhotoID); !ok {
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
	expectedChunks := int((p.TotalSize + MaxFileChunkRaw - 1) / MaxFileChunkRaw)
	if p.TotalSize == 0 {
		expectedChunks = 1
	}
	if p.TotalChunks != expectedChunks {
		return false
	}
	if p.ChunkIndex < p.TotalChunks-1 {
		if p.Offset != int64(p.ChunkIndex)*MaxFileChunkRaw {
			return false
		}
		if len(p.DataB64) == 0 {
			return false
		}
	} else {
		if p.Offset != int64(p.ChunkIndex)*MaxFileChunkRaw {
			return false
		}
	}
	if len(p.DataB64) > MaxPhotoChunkB64Len {
		return false
	}
	if p.DataB64 != "" {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(p.DataB64))
		if err != nil {
			return false
		}
		if len(raw) > MaxFileChunkRaw {
			return false
		}
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
		if p.ChunkIndex != p.TotalChunks-1 {
			return false
		}
	}
	if p.Mime != "" && len(p.Mime) > 64 {
		return false
	}
	return true
}

// SanitizePhotoDelete validates a delete request. Pure.
func SanitizePhotoDelete(p PhotoDeletePayload) bool {
	if p.Nonce == "" || strings.TrimSpace(p.ReqID) == "" {
		return false
	}
	if len(p.ReqID) > 64 {
		return false
	}
	if len(p.PhotoIDs) == 0 || len(p.PhotoIDs) > MaxPhotoDeleteBatch {
		return false
	}
	seen := make(map[string]bool, len(p.PhotoIDs))
	for _, id := range p.PhotoIDs {
		if _, ok := SanitizePhotoID(id); !ok {
			return false
		}
		if seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

// SanitizePhotoDeleteResp validates a delete response. Pure.
func SanitizePhotoDeleteResp(p PhotoDeleteRespPayload) bool {
	if p.Nonce == "" || strings.TrimSpace(p.ReqID) == "" {
		return false
	}
	if len(p.ReqID) > 64 {
		return false
	}
	if len(p.Results) > MaxPhotoDeleteBatch {
		return false
	}
	for _, r := range p.Results {
		if _, ok := SanitizePhotoID(r.PhotoID); !ok {
			return false
		}
		if len(r.Error) > 512 {
			return false
		}
		if !SanitizeErrorCode(r.ErrorCode) {
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
