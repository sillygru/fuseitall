// SPDX-License-Identifier: AGPL-3.0-only

package backend

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"fuseitall/core"
)

// clipChunkSession reassembles one inbound large image.
type clipChunkSession struct {
	manifest core.ClipManifestPayload
	chunks   map[int][]byte
	received int
	totalRaw int64
	updated  time.Time
}

// ClipChunkHub owns inbound chunk sessions (in-memory only, latest-wins per
// session id). Safe for concurrent use. Stale sessions expire after 5 min.
type ClipChunkHub struct {
	mu       sync.Mutex
	sessions map[string]*clipChunkSession
}

// NewClipChunkHub returns an empty hub.
func NewClipChunkHub() *ClipChunkHub {
	return &ClipChunkHub{sessions: make(map[string]*clipChunkSession)}
}

// Begin starts a session from a validated manifest. False when the id is
// already assembling (duplicate manifest is idempotent, not an error).
func (h *ClipChunkHub) Begin(m core.ClipManifestPayload) bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sweepLocked()
	if _, ok := h.sessions[m.SessionID]; ok {
		return false
	}
	h.sessions[m.SessionID] = &clipChunkSession{
		manifest: m,
		chunks:   make(map[int][]byte, m.TotalChunks),
		totalRaw: m.TotalRaw,
		updated:  time.Now(),
	}
	return true
}

// Add stores one validated chunk. It returns the reassembled clip-push when
// the session completes and verifies (hash + mime + magic), or nil while
// still assembling. Corrupt sessions are dropped fail-closed.
func (h *ClipChunkHub) Add(c core.ClipChunkPayload) *core.ClipPushPayload {
	if h == nil {
		return nil
	}
	raw, ok := core.DecodeClipChunkData(c.DataB64)
	if !ok {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	sess, ok := h.sessions[c.SessionID]
	if !ok {
		return nil
	}
	if c.TotalChunks != sess.manifest.TotalChunks || c.TotalRaw != sess.manifest.TotalRaw {
		delete(h.sessions, c.SessionID)
		return nil
	}
	if _, dup := sess.chunks[c.ChunkIndex]; dup {
		return nil
	}
	cp := append([]byte{}, raw...)
	sess.chunks[c.ChunkIndex] = cp
	sess.received++
	sess.updated = time.Now()
	if sess.received != sess.manifest.TotalChunks {
		return nil
	}
	// Reassemble in order.
	assembled := make([]byte, 0, sess.totalRaw)
	for i := 0; i < sess.manifest.TotalChunks; i++ {
		part, ok := sess.chunks[i]
		if !ok {
			delete(h.sessions, c.SessionID)
			return nil
		}
		assembled = append(assembled, part...)
	}
	delete(h.sessions, c.SessionID)
	if int64(len(assembled)) != sess.totalRaw {
		return nil
	}
	m := sess.manifest
	if got := core.ContentHashForBytes(assembled); !strings.EqualFold(got, m.ContentHash) {
		return nil
	}
	if _, ok := core.SanitizeClipImage(base64.StdEncoding.EncodeToString(assembled), m.Mime); !ok {
		// Validate magic without retaining the b64 string twice.
		if core.SniffImageMime(assembled) == "" {
			return nil
		}
	}
	b64 := base64.StdEncoding.EncodeToString(assembled)
	outMime, _ := core.SanitizeClipMime(m.Mime)
	push := &core.ClipPushPayload{
		Kind:        core.ClipKindImage,
		Mime:        outMime,
		ImageB64:    b64,
		Filename:    core.SanitizeClipFilename(m.Filename),
		ChangedAt:   m.ChangedAt,
		ChangedC:    m.ChangedC,
		Origin:      m.Origin,
		ContentHash: strings.ToLower(m.ContentHash),
		Sensitive:   m.Sensitive,
	}
	return push
}

func (h *ClipChunkHub) sweepLocked() {
	now := time.Now()
	for id, sess := range h.sessions {
		if now.Sub(sess.updated) > 5*time.Minute {
			delete(h.sessions, id)
		}
	}
	if len(h.sessions) > 8 {
		// Bound memory: drop oldest beyond 8 concurrent sessions.
		oldest := ""
		var oldestAt time.Time
		for id, sess := range h.sessions {
			if oldest == "" || sess.updated.Before(oldestAt) {
				oldest, oldestAt = id, sess.updated
			}
		}
		if oldest != "" {
			delete(h.sessions, oldest)
		}
	}
}

// freshClipSessionID mints a hex session id via crypto/rand. Fail-closed on
// RNG error per golang-security.
func freshClipSessionID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate clip session id: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}

// splitClipBytes slices raw into 1 MiB raw chunks with offsets. Pure.
func splitClipBytes(raw []byte) (chunks [][]byte) {
	for off := 0; off < len(raw); off += core.ClipChunkRaw {
		end := off + core.ClipChunkRaw
		if end > len(raw) {
			end = len(raw)
		}
		chunks = append(chunks, raw[off:end])
	}
	if len(chunks) == 0 {
		chunks = append(chunks, []byte{})
	}
	return chunks
}
