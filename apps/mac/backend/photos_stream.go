// SPDX-License-Identifier: AGPL-3.0-only

package backend

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fuseitall/core"
)

// Video streaming: the frontend <video> element cannot consume base64 data
// URLs for gigabyte files, so the Mac serves the sparse part file over a
// loopback HTTP server with byte-range support (http.ServeContent). Reads
// past downloaded bytes block briefly while the missing window is pulled
// from the phone, so scrubbing never serves zero-filled holes: backpressure
// surfaces as buffering, never corruption.
//
// Security: 127.0.0.1 only (never 0.0.0.0), per-stream crypto/rand token
// compared in constant time, lifetime bound to the transfer entry. Loopback
// http is a secure context for video elements; if the webview refuses it,
// the preview falls back to full download (loud error, never silent).

const (
	// photoStreamPrefetch is the first window pulled when a stream opens so
	// the first frame arrives without waiting for a seek.
	photoStreamPrefetch = 4 << 20 // 4 MiB
	// photoStreamWindow caps one on-demand pull behind a read miss.
	photoStreamWindow = 4 << 20 // 4 MiB
	// photoStreamWait bounds one range wait behind an HTTP read.
	photoStreamWait = 20 * time.Second
)

// photoStreamServer is a loopback-only range server for video streams.
type photoStreamServer struct {
	svc    *Service
	ln     net.Listener
	port   int
	mu     sync.Mutex
	tokens map[string]string // transferID -> token
}

// streamServer returns the lazy loopback server, binding 127.0.0.1:0 once.
func (s *Service) streamServer() (*photoStreamServer, error) {
	s.photoStreamMu.Lock()
	defer s.photoStreamMu.Unlock()
	if s.photoStream != nil {
		return s.photoStream, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("bind photo stream loopback: %w", err)
	}
	srv := &photoStreamServer{
		svc:    s,
		ln:     ln,
		tokens: make(map[string]string),
	}
	if addr, ok := ln.Addr().(*net.TCPAddr); ok {
		srv.port = addr.Port
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v/", srv.serveVideo)
	go func() {
		if serr := http.Serve(ln, mux); serr != nil && !errors.Is(serr, net.ErrClosed) {
			slog.Warn("photo stream server ended", "err", serr)
		}
	}()
	s.photoStream = srv
	return srv, nil
}

// PhotoStreamStart is the Wails-bound result for StartPhotoStream.
type PhotoStreamStart struct {
	TransferID string `json:"transferId"`
	URL        string `json:"url"`
}

// StartPhotoStream opens a video stream: creates a range transfer, fires a
// prefix pull for fast first frame, and returns the transfer ID plus the
// loopback URL for the frontend <video> element. Build-8 peer required.
func (s *Service) StartPhotoStream(photoID, mime string) (PhotoStreamStart, error) {
	if _, ok := core.SanitizePhotoID(photoID); !ok {
		return PhotoStreamStart{}, errors.New("invalid photo id")
	}
	if !s.IsPaired() {
		return PhotoStreamStart{}, errors.New("phone is offline — reconnect first")
	}
	if err := s.checkPeerCapability(core.CapabilityPhotos, 8); err != nil {
		return PhotoStreamStart{}, err
	}
	transferID, err := freshTransferID()
	if err != nil {
		return PhotoStreamStart{}, err
	}
	staging, err := photoStagingRoot()
	if err != nil {
		return PhotoStreamStart{}, err
	}
	token, err := freshTransferID()
	if err != nil {
		return PhotoStreamStart{}, err
	}
	ext := photoExtForMime(mime, photoID)
	s.photoMu.Lock()
	if s.photoTransfers == nil {
		s.photoTransfers = make(map[string]*PhotoTransfer)
	}
	if s.photoWaiters == nil {
		s.photoWaiters = make(map[string]chan error)
	}
	tr := newPhotoTransfer(transferID, photoID, filepath.Join(staging, "stream-"+photoFileStem(photoID)+ext))
	tr.IsRange = true
	tr.Mime = mime
	s.photoTransfers[transferID] = tr
	s.photoMu.Unlock()

	srv, err := s.streamServer()
	if err != nil {
		s.failPhotoTransfer(transferID, err.Error())
		return PhotoStreamStart{}, err
	}
	srv.mu.Lock()
	srv.tokens[transferID] = token
	srv.mu.Unlock()

	// Fire-and-forget prefix: the player starts while the rest streams.
	if err := s.sendPhotoRange(transferID, photoID, 0, photoStreamPrefetch); err != nil {
		s.failPhotoTransfer(transferID, err.Error())
		return PhotoStreamStart{}, err
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/v/%s?token=%s", srv.port, transferID, token)
	return PhotoStreamStart{TransferID: transferID, URL: url}, nil
}

// GetPhotoStreamURL reissues the loopback URL for a live stream transfer.
func (s *Service) GetPhotoStreamURL(transferID string) (string, error) {
	if _, ok := core.SanitizeTransferID(transferID); !ok {
		return "", errors.New("invalid transfer id")
	}
	s.photoMu.Lock()
	_, ok := s.photoTransfers[transferID]
	s.photoMu.Unlock()
	if !ok {
		return "", errors.New("stream ended")
	}
	srv, err := s.streamServer()
	if err != nil {
		return "", err
	}
	srv.mu.Lock()
	token, ok := srv.tokens[transferID]
	if !ok {
		token, err = freshTransferID()
		if err != nil {
			srv.mu.Unlock()
			return "", err
		}
		srv.tokens[transferID] = token
	}
	port := srv.port
	srv.mu.Unlock()
	return fmt.Sprintf("http://127.0.0.1:%d/v/%s?token=%s", port, transferID, token), nil
}

// sendPhotoRange issues one range pull, deduping identical in-flight windows
// (scrub storms) while always serving distinct seeks. Caller holds no locks.
func (s *Service) sendPhotoRange(transferID, photoID string, offset, length int64) error {
	if length <= 0 {
		return errors.New("invalid range")
	}
	if length > core.MaxPhotoRangeLen {
		length = core.MaxPhotoRangeLen
	}
	now := time.Now()
	s.photoMu.Lock()
	if tr, ok := s.photoTransfers[transferID]; ok {
		if tr.ReqLen == length && tr.ReqOff == offset && now.Sub(tr.lastRangeAt) < 3*time.Second {
			s.photoMu.Unlock()
			return nil
		}
		tr.ReqOff, tr.ReqLen, tr.lastRangeAt = offset, length, now
	}
	s.photoMu.Unlock()
	payload := core.PhotoPullReqPayload{TransferID: transferID, PhotoID: photoID, Offset: offset, Length: length}
	if err := s.sendFeatureToPhone(core.TypePhotoPullReq, &payload); err != nil {
		return fmt.Errorf("request photo range: %w", err)
	}
	return nil
}

// serveVideo authenticates the stream token, waits for the requested bytes,
// and delegates to ServeContent for byte-range semantics.
func (srv *photoStreamServer) serveVideo(w http.ResponseWriter, r *http.Request) {
	transferID := strings.TrimPrefix(r.URL.Path, "/v/")
	if i := strings.IndexByte(transferID, '/'); i >= 0 {
		transferID = transferID[:i]
	}
	srv.mu.Lock()
	want, ok := srv.tokens[transferID]
	srv.mu.Unlock()
	got := r.URL.Query().Get("token")
	if !ok || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	s := srv.svc
	s.photoMu.Lock()
	tr, ok := s.photoTransfers[transferID]
	if !ok {
		s.photoMu.Unlock()
		http.Error(w, "stream ended", http.StatusGone)
		return
	}
	photoID, mime := tr.PhotoID, tr.Mime
	s.photoMu.Unlock()

	// Wait for the total size so clamps and Content-Length are honest.
	total, err := s.waitStreamTotal(transferID, 15*time.Second)
	if err != nil {
		http.Error(w, "video info timed out — phone did not respond", http.StatusGatewayTimeout)
		return
	}
	// Ensure the requested window is present before ServeContent maps it.
	// Multipart ranges are rejected: players use single ranges.
	if ranges := r.Header.Get("Range"); ranges != "" && strings.Count(ranges, ",") > 0 {
		http.Error(w, "multipart ranges unsupported", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	start, length := parseSingleRange(r.Header.Get("Range"), total)
	if err := s.ensureStreamWindow(transferID, photoID, start, length, total); err != nil {
		if err.Error() == "range past end of video" {
			http.Error(w, "range past end", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		http.Error(w, "video data timed out — phone did not respond", http.StatusGatewayTimeout)
		return
	}
	s.photoMu.Lock()
	tr, ok = s.photoTransfers[transferID]
	if !ok || tr.Status == "cancelled" || tr.Status == "error" {
		terr := ""
		if ok {
			terr = tr.Error
		}
		s.photoMu.Unlock()
		if terr == "" {
			terr = "stream ended"
		}
		http.Error(w, terr, http.StatusGone)
		return
	}
	part := photoPartPath(tr)
	s.photoMu.Unlock()

	f, err := os.Open(part)
	if err != nil {
		http.Error(w, "stream not ready", http.StatusServiceUnavailable)
		return
	}
	defer func() { _ = f.Close() }()
	rs := &blockingStreamReader{svc: s, transferID: transferID, f: f, size: total, ctx: r.Context()}
	if mime != "" {
		w.Header().Set("Content-Type", mime)
	}
	name := "video" + photoExtForMime(mime, photoID)
	http.ServeContent(w, r, name, time.Now(), rs)
}

// waitStreamTotal blocks until the transfer learns its total size.
// Push-driven: chunk ingestion broadcasts on the transfer notify channel,
// so this waits with zero polling; timeout is a single one-shot timer.
func (s *Service) waitStreamTotal(transferID string, timeout time.Duration) (int64, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		s.photoMu.Lock()
		tr, ok := s.photoTransfers[transferID]
		if !ok {
			s.photoMu.Unlock()
			return 0, errors.New("stream ended")
		}
		if tr.Status == "cancelled" || tr.Status == "error" {
			terr := tr.Error
			if terr == "" {
				terr = "stream ended"
			}
			s.photoMu.Unlock()
			return 0, errors.New(terr)
		}
		total := tr.TotalSize
		notify := tr.notify
		s.photoMu.Unlock()
		if total > 0 {
			return total, nil
		}
		select {
		case <-notify:
		case <-timer.C:
			return 0, errors.New("video info timed out")
		}
	}
}

// ensureStreamWindow pulls (if needed) and waits for [start, start+length).
func (s *Service) ensureStreamWindow(transferID, photoID string, start, length, total int64) error {
	if total > 0 && start >= total {
		return errors.New("range past end of video")
	}
	if total > 0 && start+length > total {
		length = total - start
	}
	if length <= 0 {
		return nil
	}
	for attempt := 0; attempt < 3; attempt++ {
		s.photoMu.Lock()
		tr, ok := s.photoTransfers[transferID]
		covered := ok && rangeCovered(tr.Ranges, start, length)
		s.photoMu.Unlock()
		if !ok {
			return errors.New("stream ended")
		}
		if covered {
			return nil
		}
		win := length
		if win > photoStreamWindow {
			win = photoStreamWindow
		}
		if err := s.sendPhotoRange(transferID, photoID, start, win); err != nil {
			return err
		}
		if _, _, err := s.waitPhotoRange(transferID, start, win, photoStreamWait); err != nil {
			return err
		}
	}
	return errors.New("video data timed out — phone did not respond")
}

// blockingStreamReader is an io.ReadSeeker over the sparse part file that
// blocks (honoring request cancel) instead of serving zero-filled holes, so
// ServeContent backpressures the player as buffering rather than glitching.
type blockingStreamReader struct {
	svc        *Service
	transferID string
	f          *os.File
	size       int64
	off        int64
	ctx        context.Context
}

func (b *blockingStreamReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	n, err := b.ReadAt(p, b.off)
	b.off += int64(n)
	return n, err
}

func (b *blockingStreamReader) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = b.off + offset
	case io.SeekEnd:
		abs = b.size + offset
	default:
		return 0, errors.New("invalid whence")
	}
	if abs < 0 {
		return 0, errors.New("negative position")
	}
	b.off = abs
	return abs, nil
}

func (b *blockingStreamReader) ReadAt(p []byte, off int64) (int, error) {
	if off >= b.size {
		return 0, io.EOF
	}
	if max := b.size - off; int64(len(p)) > max {
		p = p[:max]
	}
	deadline := time.Now().Add(photoStreamWait)
	for {
		s := b.svc
		s.photoMu.Lock()
		tr, ok := s.photoTransfers[b.transferID]
		var covered bool
		var status, terr, photoID string
		if ok {
			covered = rangeCovered(tr.Ranges, off, int64(len(p)))
			status, terr, photoID = tr.Status, tr.Error, tr.PhotoID
		}
		s.photoMu.Unlock()
		if !ok || status == "cancelled" || status == "error" {
			if terr == "" {
				terr = "stream ended"
			}
			return 0, errors.New(terr)
		}
		if covered {
			break
		}
		// Pull the missing window (deduped), then wait for it.
		win := int64(len(p))
		if win > photoStreamWindow {
			win = photoStreamWindow
		}
		if err := s.sendPhotoRange(b.transferID, photoID, off, win); err != nil {
			return 0, err
		}
		if err := s.waitWindow(b.ctx, b.transferID, off, win, deadline); err != nil {
			return 0, err
		}
	}
	n, err := b.f.ReadAt(p, off)
	if err != nil && !errors.Is(err, io.EOF) {
		return n, fmt.Errorf("read stream file: %w", err)
	}
	return n, err
}

// waitWindow blocks push-driven until [off, off+ln) is covered, the transfer
// fails, the request is cancelled, or deadline passes. Chunk ingestion
// broadcasts on the transfer notify channel: zero polling, one deadline timer.
func (s *Service) waitWindow(ctx context.Context, transferID string, off, ln int64, deadline time.Time) error {
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	for {
		s.photoMu.Lock()
		tr, ok := s.photoTransfers[transferID]
		covered := ok && rangeCovered(tr.Ranges, off, ln)
		dead := ok && (tr.Status == "cancelled" || tr.Status == "error")
		terr := ""
		var notify chan struct{}
		if ok {
			terr = tr.Error
			notify = tr.notify
		}
		s.photoMu.Unlock()
		if !ok || dead {
			if terr == "" {
				terr = "stream ended"
			}
			return errors.New(terr)
		}
		if covered {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("stream ended")
		case <-timer.C:
			return errors.New("video data timed out — phone did not respond")
		case <-notify:
		}
	}
}

// parseSingleRange parses one `bytes=start-end` header against total.
// Absent header means the whole file; open end clamps to total.
func parseSingleRange(header string, total int64) (int64, int64) {
	if total <= 0 {
		return 0, 0
	}
	h := strings.TrimSpace(header)
	if h == "" {
		return 0, total
	}
	if !strings.HasPrefix(h, "bytes=") {
		return 0, total
	}
	spec := strings.TrimPrefix(h, "bytes=")
	if strings.HasSuffix(spec, "-") && !strings.Contains(spec[:len(spec)-1], "-") {
		var start int64
		if _, err := fmt.Sscanf(spec, "%d-", &start); err == nil && start >= 0 && start < total {
			return start, total - start
		}
		return 0, total
	}
	var start, end int64
	if _, err := fmt.Sscanf(spec, "%d-%d", &start, &end); err == nil {
		if start < 0 {
			start = 0
		}
		if end >= total {
			end = total - 1
		}
		if end >= start {
			return start, end - start + 1
		}
	}
	return 0, total
}
