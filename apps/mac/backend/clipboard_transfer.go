// SPDX-License-Identifier: AGPL-3.0-only

package backend

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"fuseitall/core"
)

// ensureClipNonce stamps a fresh nonce when absent (WS path mints here;
// HTTP SendFeature stamps instead). Stable across a single attempt; retries
// mint anew while content-hash dedupe keeps them idempotent.
func ensureClipNonce(p *core.ClipPushPayload) error {
	if p.Nonce != "" {
		return nil
	}
	nonce, err := core.FreshNonce()
	if err != nil {
		return err
	}
	p.Nonce = nonce
	return nil
}

// isSensitivePasteboard reports whether the current pasteboard advertises a
// concealed type. Manual Send flags but never gates on this; auto watchers
// skip unless opted in.
func isSensitivePasteboard() bool {
	return core.HasConcealedPasteboardType(readPasteboardTypes())
}

// decodeClipB64ForChunk decodes + validates an image against the chunk-lane
// cap (mime whitelist + magic + ≤MaxClipTotalRaw). Pure aside from no I/O.
func decodeClipB64ForChunk(b64, mime string) ([]byte, bool) {
	if len(b64) == 0 {
		return nil, false
	}
	m, ok := core.SanitizeClipMime(mime)
	if !ok {
		return nil, false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil || len(raw) == 0 || len(raw) > core.MaxClipTotalRaw {
		return nil, false
	}
	sniffed := core.SniffImageMime(raw)
	if sniffed == "" {
		return nil, false
	}
	if m == "image/heic" || m == "image/heif" {
		if sniffed != "image/heic" && sniffed != "image/heif" {
			return nil, false
		}
	} else if sniffed != m {
		return nil, false
	}
	return raw, true
}

// sendLargeClipboardImage sends raw (>5 MiB, ≤25 MiB) via manifest + chunks.
// Chunk sends are synchronous and fail closed (no 25-chunk pending queue:
// the user retries manual Send). HLC + hash ride the manifest.
func (s *Service) sendLargeClipboardImage(raw []byte, mime, filename string, sensitive bool) (string, error) {
	if len(raw) <= core.MaxClipImageRaw || len(raw) > core.MaxClipTotalRaw {
		return "", fmt.Errorf("image size %d not chunkable (inline ≤%d, chunked ≤%d)", len(raw), core.MaxClipImageRaw, core.MaxClipTotalRaw)
	}
	m, _ := core.SanitizeClipMime(mime)
	fn := core.SanitizeClipFilename(filename)
	hash := core.ContentHashForBytes(raw)
	cur := s.clips.Get()
	now := time.Now().Unix()
	stamp := core.ClipStampNext(core.ClipStamp{L: cur.ChangedUnix, C: cur.ChangedC}, now)
	sessionID, err := freshClipSessionID()
	if err != nil {
		return "", err
	}
	totalChunks := core.TotalClipChunksForSize(int64(len(raw)))
	manifest := core.ClipManifestPayload{
		SessionID: sessionID, Mime: m, Filename: fn,
		TotalRaw: int64(len(raw)), TotalChunks: totalChunks, ChunkSize: core.ClipChunkRaw,
		ChangedAt: stamp.L, ChangedC: stamp.C, Origin: core.OriginMac,
		ContentHash: hash, Sensitive: sensitive,
	}
	if nonce, err := core.FreshNonce(); err != nil {
		return "", err
	} else {
		manifest.Nonce = nonce
	}
	if !core.SanitizeClipManifest(manifest) {
		return "", fmt.Errorf("large image manifest invalid")
	}
	if err := s.sendFeatureToPhone(core.TypeClipManifest, &manifest); err != nil {
		return "", fmt.Errorf("send image manifest: %w", err)
	}
	parts := splitClipBytes(raw)
	for i, part := range parts {
		chunk := core.ClipChunkPayload{
			SessionID: sessionID, ChunkIndex: i, TotalChunks: totalChunks,
			Offset: int64(i) * int64(core.ClipChunkRaw), TotalRaw: int64(len(raw)),
			DataB64: base64.StdEncoding.EncodeToString(part),
		}
		if i == len(parts)-1 {
			chunk.ContentHash = hash
		}
		if nonce, err := core.FreshNonce(); err != nil {
			return "", err
		} else {
			chunk.Nonce = nonce
		}
		if !core.SanitizeClipChunk(chunk) {
			return "", fmt.Errorf("large image chunk %d invalid", i)
		}
		if err := s.sendFeatureToPhone(core.TypeClipChunk, &chunk); err != nil {
			return "", fmt.Errorf("send image chunk %d/%d: %w", i+1, totalChunks, err)
		}
	}
	s.appendLine(fmt.Sprintf("clipboard large image sent to phone (%d bytes, %d chunks)", len(raw), totalChunks))
	return fmt.Sprintf("Large image sent to phone (%d chunks).", totalChunks), nil
}

// applyRemoteToPasteboard arms suppression, writes, emits, and disarms on
// failure. Shared by inline pushes and reassembled chunk sessions.
func (s *Service) applyRemoteToPasteboard(p core.ClipPushPayload) {
	kind := core.NormalizeClipKind(p.Kind)
	if kind == core.ClipKindImage {
		if s.clipWatcher != nil {
			s.clipWatcher.NoteRemoteImage(p.ImageB64, p.Mime)
		}
		s.emitClipboardChanged(s.clips.Get())
		if p.Sensitive {
			s.appendLine("clipboard sensitive image synced from phone")
		} else {
			s.appendLine("clipboard image synced from phone")
		}
		if !writePasteboardImageWithFilename(p.ImageB64, p.Mime, p.Filename) {
			s.appendLine("clipboard image write failed")
			if s.clipWatcher != nil {
				s.clipWatcher.ClearSuppress()
			}
		}
		return
	}
	if s.clipWatcher != nil {
		s.clipWatcher.NoteRemoteCopy(p.Text)
	}
	s.emitClipboardChanged(s.clips.Get())
	if p.Sensitive {
		s.appendLine("clipboard sensitive text synced from phone")
	} else {
		s.appendLine("clipboard synced from phone")
	}
	if !writePasteboard(p.Text) {
		s.appendLine("clipboard write failed")
		if s.clipWatcher != nil {
			s.clipWatcher.ClearSuppress()
		}
	}
}

// clipConflictLine renders a feed-only conflict/stale drop (lengths only,
// never bodies). Pure.
func clipConflictLine(p core.ClipPushPayload) string {
	kind := core.NormalizeClipKind(p.Kind)
	if kind == core.ClipKindImage {
		return "clipboard conflict: kept local, dropped remote image origin=" + core.NormalizeOrigin(p.Origin)
	}
	return "clipboard conflict: kept local, dropped remote text len=" + itoa(len(p.Text)) + " origin=" + core.NormalizeOrigin(p.Origin)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// ingestClipRouted routes one accepted /clip envelope to its handler by
// type (inline push vs chunk manifest vs chunk). Unknown types drop loud.
func (s *Service) ingestClipRouted(body []byte) {
	var sniff struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &sniff); err != nil {
		return
	}
	switch sniff.Type {
	case core.TypeClipManifest:
		s.ingestClipManifestBody(body)
	case core.TypeClipChunk:
		s.ingestClipChunkBody(body)
	default:
		s.ingestClipBody(body)
	}
}

// ingestClipManifestBody starts a chunked-image session from an accepted
// manifest. Unknown/duplicate manifests are idempotent drops.
func (s *Service) ingestClipManifestBody(body []byte) {
	m, ok := ParseClipManifest(body)
	if !ok {
		return
	}
	mode := s.settings.Get().ClipboardMode
	if mode == "" {
		mode = core.ClipboardBoth
	}
	if !core.ClipboardModeAllowsReceive(mode, m.Origin) {
		s.appendLine("clipboard dropped: mode " + mode)
		return
	}
	if s.clipChunks == nil {
		s.clipChunks = NewClipChunkHub()
	}
	s.clipChunks.Begin(m)
	s.appendLine("clipboard large image receiving")
}

// ingestClipChunkBody stores one chunk; on completion the reassembled push
// flows through the same HLC/hash/mode/pasteboard path as inline pushes.
func (s *Service) ingestClipChunkBody(body []byte) {
	c, ok := ParseClipChunk(body)
	if !ok {
		return
	}
	if s.clipChunks == nil {
		return
	}
	push := s.clipChunks.Add(c)
	if push == nil {
		return
	}
	if push.Nonce == "" {
		if nonce, err := core.FreshNonce(); err == nil {
			push.Nonce = nonce
		}
	}
	mode := s.settings.Get().ClipboardMode
	if mode == "" {
		mode = core.ClipboardBoth
	}
	if !core.ClipboardModeAllowsReceive(mode, push.Origin) {
		s.appendLine("clipboard dropped: mode " + mode)
		return
	}
	if !s.clips.ApplyRemote(*push) {
		s.appendLine(clipConflictLine(*push))
		return
	}
	s.applyRemoteToPasteboard(*push)
}
