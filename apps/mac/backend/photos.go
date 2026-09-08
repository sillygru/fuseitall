// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fuseitall/core"
)

// PhotoEntryView is the Wails-bound row for one photo.
type PhotoEntryView struct {
	PhotoID     string `json:"photo_id"`
	TakenAt     int64  `json:"taken_at"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	Mime        string `json:"mime,omitempty"`
	Size        int64  `json:"size,omitempty"`
	Orientation int    `json:"orientation,omitempty"`
}

// PhotoListResult is the typed paged listing for the frontend.
type PhotoListResult struct {
	Entries    []PhotoEntryView `json:"entries"`
	NextCursor string           `json:"next_cursor,omitempty"`
	Error      string           `json:"error,omitempty"`
	ErrorCode  string           `json:"error_code,omitempty"`
	Permission string           `json:"permission,omitempty"`
}

// PhotoThumbResult is one fetched thumbnail.
type PhotoThumbResult struct {
	PhotoID string `json:"photo_id"`
	Mime    string `json:"mime,omitempty"`
	DataB64 string `json:"data_b64,omitempty"`
	Error   string `json:"error,omitempty"`
}

// PhotoDeleteItemView is per-item delete outcome.
type PhotoDeleteItemView struct {
	PhotoID   string `json:"photo_id"`
	Ok        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

// PhotoDeleteResult is the typed batch delete outcome.
type PhotoDeleteResult struct {
	Results    []PhotoDeleteItemView `json:"results"`
	Error      string                `json:"error,omitempty"`
	ErrorCode  string                `json:"error_code,omitempty"`
	Permission string                `json:"permission,omitempty"`
}

// PhotoTransferView is the Wails-bound progress row for photo downloads.
type PhotoTransferView struct {
	ID        string `json:"id"`
	PhotoID   string `json:"photo_id"`
	Status    string `json:"status"`
	Progress  int    `json:"progress"`
	TotalSize int64  `json:"total_size"`
	DoneSize  int64  `json:"done_size"`
	Error     string `json:"error,omitempty"`
}

// PhotoTransfer tracks one photo download. Guarded by Service.photoMu.
type PhotoTransfer struct {
	ID          string
	PhotoID     string
	TotalSize   int64
	DoneSize    int64
	Status      string
	Error       string
	tmpPath     string
	completedAt time.Time
}

// photoStagingRoot returns the isolated staging dir for photo downloads.
func photoStagingRoot() (string, error) {
	base := filepath.Join(os.TempDir(), "fuseitall-photos")
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", fmt.Errorf("mkdir photo staging: %w", err)
	}
	return base, nil
}

// ListPhonePhotos requests one paged listing and waits for photo-list-resp.
func (s *Service) ListPhonePhotos(cursor string, limit int) (PhotoListResult, error) {
	if len(cursor) > 256 {
		return PhotoListResult{}, errors.New("invalid cursor")
	}
	if limit == 0 {
		limit = core.DefaultPhotoListLimit
	}
	if limit < 1 || limit > core.MaxPhotosPerList {
		return PhotoListResult{}, errors.New("invalid limit")
	}
	if !s.IsPaired() {
		return PhotoListResult{}, errors.New("phone is offline — reconnect first")
	}
	if err := s.checkPeerCapability(core.CapabilityPhotos, 7); err != nil {
		var upd *core.UpdateRequiredError
		if errors.As(err, &upd) {
			return PhotoListResult{Error: upd.Message, ErrorCode: core.CodeUpdateRequired}, err
		}
		return PhotoListResult{}, err
	}
	reqID, err := freshTransferID()
	if err != nil {
		return PhotoListResult{}, err
	}
	reqID = reqID[:16]
	ch := make(chan PhotoListResult, 1)
	s.photoMu.Lock()
	if s.pendingPhotoLists == nil {
		s.pendingPhotoLists = make(map[string]chan PhotoListResult)
	}
	s.pendingPhotoLists[reqID] = ch
	s.photoMu.Unlock()
	defer func() {
		s.photoMu.Lock()
		delete(s.pendingPhotoLists, reqID)
		s.photoMu.Unlock()
	}()

	payload := core.PhotoListPayload{ReqID: reqID, Cursor: cursor, Limit: limit}
	if err := s.sendFeatureToPhone(core.TypePhotoList, &payload); err != nil {
		return PhotoListResult{}, fmt.Errorf("send photo-list: %w", err)
	}
	select {
	case res := <-ch:
		s.photoMu.Lock()
		s.lastPhotoList = res
		s.photoMu.Unlock()
		if res.Error != "" {
			return res, errors.New(res.Error)
		}
		return res, nil
	case <-time.After(8 * time.Second):
		if err := s.checkPeerCapability(core.CapabilityPhotos, 7); err != nil {
			var upd *core.UpdateRequiredError
			if errors.As(err, &upd) {
				return PhotoListResult{Error: upd.Message, ErrorCode: core.CodeUpdateRequired}, err
			}
			return PhotoListResult{}, err
		}
		return PhotoListResult{}, errors.New("photo listing timed out — phone did not respond")
	}
}

// photoThumbLRU is a bounded in-memory cache for photo thumbnails (RAM-only, zero SSD wear).
type photoThumbLRU struct {
	mu       sync.Mutex
	capacity int
	items    map[string]*thumbLRUNode
	head     *thumbLRUNode
	tail     *thumbLRUNode
}

type thumbLRUNode struct {
	key   string
	value PhotoThumbResult
	prev  *thumbLRUNode
	next  *thumbLRUNode
}

func newPhotoThumbLRU(capacity int) *photoThumbLRU {
	if capacity <= 0 {
		capacity = 200
	}
	return &photoThumbLRU{
		capacity: capacity,
		items:    make(map[string]*thumbLRUNode),
	}
}

func (c *photoThumbLRU) Get(key string) (PhotoThumbResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	node, ok := c.items[key]
	if !ok {
		return PhotoThumbResult{}, false
	}
	c.moveToHead(node)
	return node.value, true
}

func (c *photoThumbLRU) Put(key string, val PhotoThumbResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if node, ok := c.items[key]; ok {
		node.value = val
		c.moveToHead(node)
		return
	}
	node := &thumbLRUNode{key: key, value: val}
	c.items[key] = node
	c.addToHead(node)
	if len(c.items) > c.capacity {
		c.removeTail()
	}
}

func (c *photoThumbLRU) addToHead(node *thumbLRUNode) {
	node.next = c.head
	node.prev = nil
	if c.head != nil {
		c.head.prev = node
	}
	c.head = node
	if c.tail == nil {
		c.tail = node
	}
}

func (c *photoThumbLRU) removeNode(node *thumbLRUNode) {
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		c.head = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	} else {
		c.tail = node.prev
	}
}

func (c *photoThumbLRU) moveToHead(node *thumbLRUNode) {
	c.removeNode(node)
	c.addToHead(node)
}

func (c *photoThumbLRU) removeTail() {
	if c.tail == nil {
		return
	}
	delete(c.items, c.tail.key)
	c.removeNode(c.tail)
}

func (s *Service) getThumbCache() *photoThumbLRU {
	s.photoMu.Lock()
	defer s.photoMu.Unlock()
	if s.photoThumbCache == nil {
		s.photoThumbCache = newPhotoThumbLRU(200)
	}
	return s.photoThumbCache
}

// failPendingPhotoRequests unblocks all in-flight photo requests when peer disconnects.
func (s *Service) failPendingPhotoRequests(err error) {
	s.photoMu.Lock()
	defer s.photoMu.Unlock()
	for reqID, ch := range s.pendingThumbs {
		select {
		case ch <- PhotoThumbResult{Error: err.Error()}:
		default:
		}
		delete(s.pendingThumbs, reqID)
	}
	for reqID, ch := range s.pendingPhotoLists {
		select {
		case ch <- PhotoListResult{Error: err.Error()}:
		default:
		}
		delete(s.pendingPhotoLists, reqID)
	}
	for reqID, ch := range s.pendingPhotoDels {
		select {
		case ch <- PhotoDeleteResult{Error: err.Error()}:
		default:
		}
		delete(s.pendingPhotoDels, reqID)
	}
}

// RequestPhotoThumb fetches one thumbnail and waits for photo-thumb-resp.
// Checks the in-memory LRU cache first (RAM-only, zero SSD wear).
func (s *Service) RequestPhotoThumb(photoID string, thumbSize int) (PhotoThumbResult, error) {
	if _, ok := core.SanitizePhotoID(photoID); !ok {
		return PhotoThumbResult{}, errors.New("invalid photo id")
	}
	if thumbSize == 0 {
		thumbSize = 256
	}
	if thumbSize < core.MinPhotoThumbSize || thumbSize > core.MaxPhotoThumbSize {
		return PhotoThumbResult{}, errors.New("invalid thumb size")
	}

	cacheKey := fmt.Sprintf("%s_%d", photoID, thumbSize)
	if cached, ok := s.getThumbCache().Get(cacheKey); ok {
		return cached, nil
	}

	if !s.IsPaired() {
		return PhotoThumbResult{}, errors.New("phone is offline — reconnect first")
	}
	if err := s.checkPeerCapability(core.CapabilityPhotos, 7); err != nil {
		var upd *core.UpdateRequiredError
		if errors.As(err, &upd) {
			return PhotoThumbResult{PhotoID: photoID, Error: upd.Message}, err
		}
		return PhotoThumbResult{}, err
	}
	reqID, err := freshTransferID()
	if err != nil {
		return PhotoThumbResult{}, err
	}
	reqID = reqID[:16]
	ch := make(chan PhotoThumbResult, 1)
	s.photoMu.Lock()
	if s.pendingThumbs == nil {
		s.pendingThumbs = make(map[string]chan PhotoThumbResult)
	}
	s.pendingThumbs[reqID] = ch
	s.photoMu.Unlock()
	defer func() {
		s.photoMu.Lock()
		delete(s.pendingThumbs, reqID)
		s.photoMu.Unlock()
	}()

	payload := core.PhotoThumbReqPayload{ReqID: reqID, PhotoID: photoID, ThumbSize: thumbSize}
	if err := s.sendFeatureToPhone(core.TypePhotoThumbReq, &payload); err != nil {
		return PhotoThumbResult{}, fmt.Errorf("send photo-thumb-req: %w", err)
	}
	select {
	case res := <-ch:
		if res.Error != "" {
			return res, errors.New(res.Error)
		}
		if res.DataB64 != "" {
			s.getThumbCache().Put(cacheKey, res)
		}
		return res, nil
	case <-time.After(10 * time.Second):
		return PhotoThumbResult{}, errors.New("thumb timed out — phone did not respond")
	}
}

// DeletePhonePhotos deletes a batch and waits for photo-delete-resp.
func (s *Service) DeletePhonePhotos(photoIDs []string) (PhotoDeleteResult, error) {
	if len(photoIDs) == 0 || len(photoIDs) > core.MaxPhotoDeleteBatch {
		return PhotoDeleteResult{}, errors.New("invalid photo batch")
	}
	for _, id := range photoIDs {
		if _, ok := core.SanitizePhotoID(id); !ok {
			return PhotoDeleteResult{}, errors.New("invalid photo id")
		}
	}
	if !s.IsPaired() {
		return PhotoDeleteResult{}, errors.New("phone is offline — reconnect first")
	}
	if err := s.checkPeerCapability(core.CapabilityPhotos, 7); err != nil {
		return PhotoDeleteResult{Error: err.Error()}, err
	}
	reqID, err := freshTransferID()
	if err != nil {
		return PhotoDeleteResult{}, err
	}
	reqID = reqID[:16]
	ch := make(chan PhotoDeleteResult, 1)
	s.photoMu.Lock()
	if s.pendingPhotoDels == nil {
		s.pendingPhotoDels = make(map[string]chan PhotoDeleteResult)
	}
	s.pendingPhotoDels[reqID] = ch
	s.photoMu.Unlock()
	defer func() {
		s.photoMu.Lock()
		delete(s.pendingPhotoDels, reqID)
		s.photoMu.Unlock()
	}()

	payload := core.PhotoDeletePayload{ReqID: reqID, PhotoIDs: photoIDs}
	if err := s.sendFeatureToPhone(core.TypePhotoDelete, &payload); err != nil {
		return PhotoDeleteResult{}, fmt.Errorf("send photo-delete: %w", err)
	}
	select {
	case res := <-ch:
		if res.Error != "" && len(res.Results) == 0 {
			return res, errors.New(res.Error)
		}
		return res, nil
	case <-time.After(15 * time.Second):
		return PhotoDeleteResult{}, errors.New("photo delete timed out — phone did not respond")
	}
}

// RequestPhonePhoto starts a full-res download; progress via GetPhotoTransfers.
func (s *Service) RequestPhonePhoto(photoID, downloadDir string) (string, error) {
	if _, ok := core.SanitizePhotoID(photoID); !ok {
		return "", errors.New("invalid photo id")
	}
	if !s.IsPaired() {
		return "", errors.New("phone is offline — reconnect first")
	}
	if err := s.checkPeerCapability(core.CapabilityPhotos, 7); err != nil {
		return "", err
	}
	transferID, err := freshTransferID()
	if err != nil {
		return "", err
	}
	dir := downloadDir
	if dir == "" {
		home, herr := os.UserHomeDir()
		if herr != nil || home == "" {
			return "", errors.New("no download dir")
		}
		dir = filepath.Join(home, "Downloads")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir download dir: %w", err)
	}
	finalName := "photo-" + photoID + ".jpg"
	tmpPath := filepath.Join(dir, finalName)
	s.photoMu.Lock()
	if s.photoTransfers == nil {
		s.photoTransfers = make(map[string]*PhotoTransfer)
	}
	if s.photoWaiters == nil {
		s.photoWaiters = make(map[string]chan error)
	}
	s.photoTransfers[transferID] = &PhotoTransfer{
		ID: transferID, PhotoID: photoID, Status: "running", tmpPath: tmpPath,
	}
	s.photoMu.Unlock()

	payload := core.PhotoPullReqPayload{TransferID: transferID, PhotoID: photoID}
	if err := s.sendFeatureToPhone(core.TypePhotoPullReq, &payload); err != nil {
		s.failPhotoTransfer(transferID, err.Error())
		return "", fmt.Errorf("request photo: %w", err)
	}
	s.appendLine("photo pull sent")
	return transferID, nil
}

// GetPhotoTransfers returns photo download progress rows.
func (s *Service) GetPhotoTransfers() []PhotoTransferView {
	s.photoMu.Lock()
	defer s.photoMu.Unlock()
	views := make([]PhotoTransferView, 0, len(s.photoTransfers))
	for _, tr := range s.photoTransfers {
		progress := 0
		if tr.TotalSize > 0 {
			progress = int(tr.DoneSize * 100 / tr.TotalSize)
		}
		views = append(views, PhotoTransferView{
			ID: tr.ID, PhotoID: tr.PhotoID, Status: tr.Status,
			Progress: progress, TotalSize: tr.TotalSize, DoneSize: tr.DoneSize, Error: tr.Error,
		})
	}
	if views == nil {
		views = []PhotoTransferView{}
	}
	return views
}

// CancelPhotoTransfer marks a photo download cancelled.
func (s *Service) CancelPhotoTransfer(id string) {
	s.photoMu.Lock()
	defer s.photoMu.Unlock()
	if tr, ok := s.photoTransfers[id]; ok {
		tr.Status = "cancelled"
	}
}

// failPhotoTransfer marks a photo transfer failed. Call with any lock state;
// it takes photoMu.
func (s *Service) failPhotoTransfer(id, msg string) {
	s.photoMu.Lock()
	defer s.photoMu.Unlock()
	if tr, ok := s.photoTransfers[id]; ok {
		tr.Status = "error"
		tr.Error = msg
		tr.completedAt = time.Now()
	}
	if ch, ok := s.photoWaiters[id]; ok {
		select {
		case ch <- errors.New(msg):
		default:
		}
	}
}

// ingestPhotoBody dispatches accepted /photos pushes by type.
func (s *Service) ingestPhotoBody(body []byte) {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return
	}
	switch probe.Type {
	case core.TypePhotoListResp:
		s.ingestPhotoListRespBody(body)
	case core.TypePhotoThumbResp:
		s.ingestPhotoThumbRespBody(body)
	case core.TypePhotoChunk:
		s.ingestPhotoChunkBody(body)
	case core.TypePhotoDeleteResp:
		s.ingestPhotoDeleteRespBody(body)
	case core.TypePhotoPullReq, core.TypePhotoList, core.TypePhotoThumbReq, core.TypePhotoDelete:
		// Mac doesn't serve the library; log only.
	}
}

func (s *Service) ingestPhotoListRespBody(body []byte) {
	var env struct {
		Type    string                    `json:"type"`
		Payload core.PhotoListRespPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return
	}
	if env.Type != core.TypePhotoListResp {
		return
	}
	if !core.SanitizePhotoListResp(env.Payload) {
		return
	}
	entries := make([]PhotoEntryView, 0, len(env.Payload.Entries))
	for _, e := range env.Payload.Entries {
		entries = append(entries, PhotoEntryView{
			PhotoID: e.PhotoID, TakenAt: e.TakenAt, Width: e.Width,
			Height: e.Height, Mime: e.Mime, Size: e.Size, Orientation: e.Orientation,
		})
	}
	res := PhotoListResult{
		Entries: entries, NextCursor: env.Payload.NextCursor,
		Error: env.Payload.Error, ErrorCode: env.Payload.ErrorCode, Permission: env.Payload.Permission,
	}
	if res.Entries == nil {
		res.Entries = []PhotoEntryView{}
	}
	s.photoMu.Lock()
	if s.pendingPhotoLists == nil {
		s.pendingPhotoLists = make(map[string]chan PhotoListResult)
	}
	if ch, ok := s.pendingPhotoLists[env.Payload.ReqID]; ok {
		select {
		case ch <- res:
		default:
		}
		s.photoMu.Unlock()
		return
	}
	s.lastPhotoList = res
	s.photoMu.Unlock()
	s.appendLine("photo list resp received")
}

func (s *Service) ingestPhotoThumbRespBody(body []byte) {
	var env struct {
		Type    string                     `json:"type"`
		Payload core.PhotoThumbRespPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return
	}
	if env.Type != core.TypePhotoThumbResp {
		return
	}
	if !core.SanitizePhotoThumbResp(env.Payload) {
		return
	}
	res := PhotoThumbResult{
		PhotoID: env.Payload.PhotoID, Mime: env.Payload.Mime,
		DataB64: env.Payload.DataB64, Error: env.Payload.Error,
	}
	s.photoMu.Lock()
	if s.pendingThumbs == nil {
		s.pendingThumbs = make(map[string]chan PhotoThumbResult)
	}
	if ch, ok := s.pendingThumbs[env.Payload.ReqID]; ok {
		select {
		case ch <- res:
		default:
		}
	}
	s.photoMu.Unlock()
}

func (s *Service) ingestPhotoDeleteRespBody(body []byte) {
	var env struct {
		Type    string                      `json:"type"`
		Payload core.PhotoDeleteRespPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return
	}
	if env.Type != core.TypePhotoDeleteResp {
		return
	}
	if !core.SanitizePhotoDeleteResp(env.Payload) {
		return
	}
	items := make([]PhotoDeleteItemView, 0, len(env.Payload.Results))
	for _, r := range env.Payload.Results {
		items = append(items, PhotoDeleteItemView{
			PhotoID: r.PhotoID, Ok: r.Ok, Error: r.Error, ErrorCode: r.ErrorCode,
		})
	}
	res := PhotoDeleteResult{
		Results: items, Error: env.Payload.Error,
		ErrorCode: env.Payload.ErrorCode, Permission: env.Payload.Permission,
	}
	if res.Results == nil {
		res.Results = []PhotoDeleteItemView{}
	}
	s.photoMu.Lock()
	if s.pendingPhotoDels == nil {
		s.pendingPhotoDels = make(map[string]chan PhotoDeleteResult)
	}
	if ch, ok := s.pendingPhotoDels[env.Payload.ReqID]; ok {
		select {
		case ch <- res:
		default:
		}
	}
	s.photoMu.Unlock()
	s.appendLine("photo delete resp received")
}

func (s *Service) ingestPhotoChunkBody(body []byte) {
	var env struct {
		Type    string                `json:"type"`
		Payload core.PhotoChunkPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return
	}
	if env.Type != core.TypePhotoChunk {
		return
	}
	if !core.SanitizePhotoChunk(env.Payload) {
		s.appendLine("photo chunk rejected: sanitize failed")
		return
	}
	p := env.Payload

	s.photoMu.Lock()
	if s.photoTransfers == nil {
		s.photoTransfers = make(map[string]*PhotoTransfer)
	}
	tr, exists := s.photoTransfers[p.TransferID]
	if !exists {
		staging, _ := photoStagingRoot()
		if staging == "" {
			staging = os.TempDir()
		}
		tr = &PhotoTransfer{
			ID: p.TransferID, PhotoID: p.PhotoID, TotalSize: p.TotalSize,
			Status: "running", tmpPath: filepath.Join(staging, "photo-"+p.PhotoID+".jpg"),
		}
		s.photoTransfers[p.TransferID] = tr
	}
	if tr.Status == "cancelled" || tr.Status == "error" {
		s.photoMu.Unlock()
		return
	}
	s.photoMu.Unlock()

	var raw []byte
	if p.DataB64 != "" {
		var err error
		raw, err = base64.StdEncoding.DecodeString(strings.TrimSpace(p.DataB64))
		if err != nil {
			s.failPhotoTransfer(p.TransferID, "bad base64")
			return
		}
	}

	partPath := tr.tmpPath + ".part." + p.TransferID
	if err := os.MkdirAll(filepath.Dir(partPath), 0o700); err != nil {
		s.failPhotoTransfer(p.TransferID, err.Error())
		return
	}
	f, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		s.failPhotoTransfer(p.TransferID, err.Error())
		return
	}
	if _, err := f.Seek(p.Offset, io.SeekStart); err != nil {
		_ = f.Close()
		s.failPhotoTransfer(p.TransferID, err.Error())
		return
	}
	if len(raw) > 0 {
		if _, err := f.Write(raw); err != nil {
			_ = f.Close()
			s.failPhotoTransfer(p.TransferID, err.Error())
			return
		}
	}
	_ = f.Close()

	s.photoMu.Lock()
	tr.DoneSize = p.Offset + int64(len(raw))
	if p.ChunkIndex == p.TotalChunks-1 {
		if p.Sha256 != "" {
			if verr := verifySHA256(partPath, p.Sha256, p.TotalSize); verr != nil {
				tr.Status = "error"
				tr.Error = verr.Error()
				tr.completedAt = time.Now()
				if ch, ok := s.photoWaiters[p.TransferID]; ok {
					select {
					case ch <- verr:
					default:
					}
				}
				s.photoMu.Unlock()
				s.appendLine("photo download verify failed")
				return
			}
		}
		if fi, err := os.Stat(partPath); err == nil {
			if fi.Size() != p.TotalSize {
				tr.Status = "error"
				tr.Error = "size mismatch"
				tr.completedAt = time.Now()
				if ch, ok := s.photoWaiters[p.TransferID]; ok {
					select {
					case ch <- errors.New("size mismatch"):
					default:
					}
				}
				s.photoMu.Unlock()
				return
			}
		}
		finalPath := tr.tmpPath
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
			if ch, ok := s.photoWaiters[p.TransferID]; ok {
				select {
				case ch <- err:
				default:
				}
			}
			s.photoMu.Unlock()
			return
		}
		tr.Status = "done"
		tr.completedAt = time.Now()
		if ch, ok := s.photoWaiters[p.TransferID]; ok {
			select {
			case ch <- nil:
			default:
			}
		}
		s.photoMu.Unlock()
		s.appendLine("photo download done")
		s.emitPhotoTransfersChanged()
		return
	}
	tr.Status = "running"
	s.photoMu.Unlock()
	s.emitPhotoTransfersChanged()
}

// emitPhotoTransfersChanged notifies the frontend of photo progress.
func (s *Service) emitPhotoTransfersChanged() {
	emitWailsEvent("photo-transfers:changed", s.GetPhotoTransfers())
}

// Parse helpers exported for tests / ingestion symmetry.

func ParsePhotoList(body []byte) (core.PhotoListPayload, bool) {
	var env struct {
		Type    string               `json:"type"`
		Payload core.PhotoListPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return core.PhotoListPayload{}, false
	}
	if env.Type != core.TypePhotoList {
		return core.PhotoListPayload{}, false
	}
	if !core.SanitizePhotoList(env.Payload) {
		return core.PhotoListPayload{}, false
	}
	return env.Payload, true
}

func ParsePhotoListResp(body []byte) (core.PhotoListRespPayload, bool) {
	var env struct {
		Type    string                    `json:"type"`
		Payload core.PhotoListRespPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return core.PhotoListRespPayload{}, false
	}
	if env.Type != core.TypePhotoListResp {
		return core.PhotoListRespPayload{}, false
	}
	if !core.SanitizePhotoListResp(env.Payload) {
		return core.PhotoListRespPayload{}, false
	}
	return env.Payload, true
}

func ParsePhotoChunk(body []byte) (core.PhotoChunkPayload, bool) {
	var env struct {
		Type    string                 `json:"type"`
		Payload core.PhotoChunkPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return core.PhotoChunkPayload{}, false
	}
	if env.Type != core.TypePhotoChunk {
		return core.PhotoChunkPayload{}, false
	}
	if !core.SanitizePhotoChunk(env.Payload) {
		return core.PhotoChunkPayload{}, false
	}
	return env.Payload, true
}
