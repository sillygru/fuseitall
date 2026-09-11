// SPDX-License-Identifier: AGPL-3.0-only

// Clipboard sync primitives: HLC stamps, content hashes, sensitive-type
// gating, and chunked large-image transfer. Every type rides the Envelope
// contract; decoders ignore unknown fields. Bodies are never logged verbatim.
package core

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// FreshNonce mints a hex nonce via crypto/rand. Fail-closed on entropy
// failure per golang-security.
func FreshNonce() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate clip nonce: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}

const (
	// TypeClipManifest announces a large image before its chunks.
	// Capability: CapabilityClipboard.
	TypeClipManifest = "clip-image-manifest"
	// TypeClipChunk carries one slice of a large image.
	// Capability: CapabilityClipboard.
	TypeClipChunk = "clip-image-chunk"
)

const (
	// ClipChunkRaw is the raw stride for clipboard image chunks (1 MiB):
	// ~1.4 MiB base64 plus envelope stays well under MaxBodyBytes and the
	// Android Binder ~1 MiB per-call ceiling when chunked at the seam.
	ClipChunkRaw = 1 << 20
	// MaxClipChunkB64Len caps one chunk body on the wire.
	MaxClipChunkB64Len = 1800000
	// MaxClipTotalRaw caps a reassembled clipboard image (25 MiB): covers
	// retina screenshots while bounding memory. Inline single-push stays at
	// MaxClipImageRaw (5 MiB); anything larger must use the chunk lane.
	MaxClipTotalRaw = 25 << 20
	// MaxClipChunks caps chunk count for a session.
	MaxClipChunks = 32
	// MaxClipSessionIDLen caps session ids (hex nonces).
	MaxClipSessionIDLen = 64
)

// ClipManifestPayload announces a chunked image transfer. SessionID groups
// chunks; Nonce is the ack nonce for the manifest itself (stable across
// retries of the same logical copy so receivers dedupe).
type ClipManifestPayload struct {
	Nonce       string `json:"nonce"`
	SessionID   string `json:"session_id"`
	Mime        string `json:"mime,omitempty"`
	Filename    string `json:"filename,omitempty"`
	TotalRaw    int64  `json:"total_raw"`
	TotalChunks int    `json:"total_chunks"`
	ChunkSize   int    `json:"chunk_size"`
	ChangedAt   int64  `json:"changed_at,omitempty"`
	ChangedC    int64  `json:"changed_c,omitempty"`
	Origin      string `json:"origin,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
	Sensitive   bool   `json:"sensitive,omitempty"`
}

// ClipChunkPayload carries one slice. DataB64 decodes to raw bytes at
// Offset; the last chunk carries ContentHash for end-to-end verification
// (mirrors FileChunkPayload Sha256-on-last).
type ClipChunkPayload struct {
	Nonce       string `json:"nonce"`
	SessionID   string `json:"session_id"`
	ChunkIndex  int    `json:"chunk_index"`
	TotalChunks int    `json:"total_chunks"`
	Offset      int64  `json:"offset"`
	TotalRaw    int64  `json:"total_raw"`
	DataB64     string `json:"data_b64,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
}

// ClipStamp is an HLC stamp: L is wall millis (or secs promoted), C breaks
// same-L ties. Packed compare is lexicographic (L, C).
type ClipStamp struct {
	L int64
	C int64
}

// ClipStampNext advances a local HLC clock: L=max(stored, wall); same L
// bumps C else resets. Pure.
func ClipStampNext(stored ClipStamp, wall int64) ClipStamp {
	if wall < 0 {
		wall = 0
	}
	if wall > stored.L {
		return ClipStamp{L: wall, C: 0}
	}
	if wall == stored.L {
		return ClipStamp{L: stored.L, C: stored.C + 1}
	}
	return ClipStamp{L: stored.L, C: stored.C + 1}
}

// ClipStampOnReceive absorbs a remote stamp: L=max(local, remote, wall) with
// C bumped past both on ties so the result exceeds each input. Pure.
func ClipStampOnReceive(local, remote ClipStamp, wall int64) ClipStamp {
	l := local.L
	if remote.L > l {
		l = remote.L
	}
	if wall > l {
		l = wall
	}
	var c int64
	if l == local.L && l == remote.L {
		c = local.C
		if remote.C > c {
			c = remote.C
		}
		c++
	} else if l == local.L {
		c = local.C + 1
	} else if l == remote.L {
		c = remote.C + 1
	}
	return ClipStamp{L: l, C: c}
}

// CompareClipStamps orders (L, C) lexicographically: -1/0/+1. Pure.
func CompareClipStamps(a, b ClipStamp) int {
	if a.L != b.L {
		if a.L < b.L {
			return -1
		}
		return 1
	}
	if a.C != b.C {
		if a.C < b.C {
			return -1
		}
		return 1
	}
	return 0
}

// RemoteClipWinsEx reports whether a remote stamp supersedes local: strictly
// greater (L, C) wins; ties keep local (idempotent redelivery must not flap);
// zero remote L never wins. Pure.
func RemoteClipWinsEx(localL, localC, remoteL, remoteC int64) bool {
	if remoteL <= 0 {
		return false
	}
	if remoteL != localL {
		return remoteL > localL
	}
	return remoteC > localC
}

// ContentHashForBytes returns lowercase sha256 hex of raw bytes. Pure.
func ContentHashForBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ContentHashForText returns the hash of text utf8 bytes. Pure.
func ContentHashForText(s string) string {
	return ContentHashForBytes([]byte(s))
}

// SanitizeContentHash reports whether s is a 64-char lowercase hex sha256
// (empty means absent, allowed for backward compat). Pure.
func SanitizeContentHash(s string) bool {
	if s == "" {
		return true
	}
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return false
	}
	return true
}

// SanitizeClipSessionID validates a chunk session id: 1..64 chars, hex or
// alphanum/_/-. Pure.
func SanitizeClipSessionID(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || len(trimmed) > MaxClipSessionIDLen {
		return "", false
	}
	for _, r := range trimmed {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') || r == '-' || r == '_' {
			continue
		}
		return "", false
	}
	return trimmed, true
}

// SanitizeClipManifest validates a manifest payload. Pure.
func SanitizeClipManifest(p ClipManifestPayload) bool {
	if p.Nonce == "" {
		return false
	}
	if _, ok := SanitizeClipSessionID(p.SessionID); !ok {
		return false
	}
	if _, ok := SanitizeClipMime(p.Mime); !ok {
		return false
	}
	if p.Filename != "" && SanitizeClipFilename(p.Filename) == "" {
		return false
	}
	if p.TotalRaw <= MaxClipImageRaw || p.TotalRaw > MaxClipTotalRaw {
		return false
	}
	if p.TotalChunks < 2 || p.TotalChunks > MaxClipChunks {
		return false
	}
	if p.ChunkSize != ClipChunkRaw {
		return false
	}
	want := int((p.TotalRaw + int64(p.ChunkSize) - 1) / int64(p.ChunkSize))
	if want != p.TotalChunks {
		return false
	}
	if p.ChangedAt < 0 || p.ChangedC < 0 {
		return false
	}
	if !SanitizeContentHash(p.ContentHash) || p.ContentHash == "" {
		return false
	}
	o := NormalizeOrigin(p.Origin)
	if o == "" {
		return false
	}
	return true
}

// SanitizeClipChunk validates one chunk header plus body shape. Pure: checks
// ids, indices, offsets, b64 shape and per-chunk raw cap.
func SanitizeClipChunk(p ClipChunkPayload) bool {
	if p.Nonce == "" {
		return false
	}
	if _, ok := SanitizeClipSessionID(p.SessionID); !ok {
		return false
	}
	if p.TotalChunks < 2 || p.TotalChunks > MaxClipChunks {
		return false
	}
	if p.ChunkIndex < 0 || p.ChunkIndex >= p.TotalChunks {
		return false
	}
	if p.TotalRaw <= MaxClipImageRaw || p.TotalRaw > MaxClipTotalRaw {
		return false
	}
	wantOffset := int64(p.ChunkIndex) * int64(ClipChunkRaw)
	if p.Offset != wantOffset {
		return false
	}
	if p.Offset < 0 || p.Offset >= p.TotalRaw {
		return false
	}
	if len(p.DataB64) > MaxClipChunkB64Len {
		return false
	}
	if p.DataB64 == "" {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(p.DataB64))
	if err != nil {
		return false
	}
	if len(raw) == 0 || len(raw) > ClipChunkRaw {
		return false
	}
	// Last chunk may carry the full-file hash; when present it must be shaped.
	if p.ContentHash != "" && !SanitizeContentHash(p.ContentHash) {
		return false
	}
	// Non-last chunks must not claim a full hash (avoids confusion).
	if p.ChunkIndex != p.TotalChunks-1 && p.ContentHash != "" {
		return false
	}
	return true
}

// DecodeClipChunkData decodes one chunk body for the hot receive path
// (single decode). Pure.
func DecodeClipChunkData(dataB64 string) ([]byte, bool) {
	if dataB64 == "" || len(dataB64) > MaxClipChunkB64Len {
		return nil, false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(dataB64))
	if err != nil {
		return nil, false
	}
	if len(raw) == 0 || len(raw) > ClipChunkRaw {
		return nil, false
	}
	return raw, true
}

// TotalClipChunksForSize reports chunk count for totalRaw at ClipChunkRaw.
// Pure.
func TotalClipChunksForSize(totalRaw int64) int {
	if totalRaw <= 0 {
		return 1
	}
	n := int((totalRaw + int64(ClipChunkRaw) - 1) / int64(ClipChunkRaw))
	if n < 1 {
		return 1
	}
	return n
}

// ConcealedPasteboardTypes are macOS NSPasteboard types that mark secrets:
// readers must skip data fetch when any is present unless the user opted in
// to sensitive sync (manual Send always bypasses).
var ConcealedPasteboardTypes = map[string]bool{
	"org.nspasteboard.concealed-type":   true,
	"org.nspasteboard.transient-type":   true,
	"org.nspasteboard.auto-generated":   true,
	"com.agilebits.onepassword":         true,
}

// HasConcealedPasteboardType reports whether any advertised pasteboard type
// marks the content sensitive. Pure.
func HasConcealedPasteboardType(types []string) bool {
	for _, t := range types {
		if ConcealedPasteboardTypes[strings.TrimSpace(t)] {
			return true
		}
	}
	return false
}

// ClipboardAllowSensitive reports whether the settings blob opts in to
// auto-syncing sensitive content. Nil/absent means false (default skip).
// Pure.
func ClipboardAllowSensitive(p SettingsSyncPayload) bool {
	if p.ClipboardAllowSensitive == nil {
		return false
	}
	return *p.ClipboardAllowSensitive
}
