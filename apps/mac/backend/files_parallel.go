// SPDX-License-Identifier: AGPL-3.0-only

package backend

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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

// maxParallelFileUploads bounds concurrent file streams inside one batch.
// Chunks of different files interleave over the single WebSocket (each frame
// is one file-chunk envelope, serialized by the WS write mutex); the receiver
// reassembles by transfer_id+offset, so interleaving is safe. Eight streams
// saturate a typical LAN for many-small-file drops (songs folders) without
// exhausting phone-side staging: the phone processes upload chunks one by
// one and stages to disk, so in-flight memory stays near one envelope.
const maxParallelFileUploads = 8

// uploadProgressEmitEvery coalesces per-chunk progress pushes: the send loops
// emit Wails events every Nth chunk plus the final chunk (the deferred
// completion emit always runs). Push-only, no timers or polling — the UI
// still learns every completion, just with fewer intermediate frames.
const uploadProgressEmitEvery = 4

// largeChunkBuild is the first build whose receivers accept 4 MiB chunks
// (dual-stride validation in core). Senders stay on 1 MiB for older peers.
const largeChunkBuild = 11

// peerSupportsLargeChunks reports whether the connected phone accepts 4 MiB
// file chunks. Both the additive capability and the build gate must agree;
// unknown peers (build 0, no caps yet) stay on the legacy stride so the first
// contact after a reconnect can never poison a transfer. Fail closed.
func (s *Service) peerSupportsLargeChunks() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.peerBuild > 0 && s.peerBuild < largeChunkBuild {
		return false
	}
	if s.peerBuild == 0 {
		return false
	}
	return core.IsCapabilitySupported(s.peerCapabilities, core.CapabilityFilesLargeChunk)
}

// negotiatedChunkSize returns the raw chunk stride for a new native upload:
// 4 MiB when the peer handles large chunks, else the legacy 1 MiB.
func (s *Service) negotiatedChunkSize() int {
	return core.FileChunkSizeForPeer(s.peerSupportsLargeChunks())
}

// shouldEmitProgress reports whether chunk idx of totalChunks warrants a
// progress push under the coalescing rule. Pure.
func shouldEmitProgress(idx, totalChunks int) bool {
	if totalChunks <= 1 {
		return true
	}
	return idx == 0 || idx == totalChunks-1 || idx%uploadProgressEmitEvery == 0
}

// uploadTask is one file stream inside a parallel batch: an exact local path
// to an exact remote path with the batch's conflict policy. Info carries the
// walk-time stat so the sender skips its path lookup and verifies against
// the open handle instead (one fd syscall). Gate orders the first chunk
// behind the parent mkdir on the shared WS (nil = no ordering needed).
// ChunkSize/WantsAck snapshot the peer negotiation once per batch instead
// of locking per file (zero ChunkSize = resolve live); strides are
// per-transfer consistent so a mid-batch peer change stays valid.
type uploadTask struct {
	localPath  string
	remotePath string
	policy     string
	info       os.FileInfo
	gate       *mkdirGate
	chunkSize  int
	wantsAck   bool
}

// mkdirGate records remote dirs whose mkdir frame the walker already wrote.
// Workers wait on their file's parent dir before the first chunk so a chunk
// can never overtake its parent mkdir on the shared WS write mutex. The
// walker marks before feeding tasks, so waits resolve immediately in
// practice; a missing gate fails open (the phone recursive-creates parents
// on write) rather than stalling the batch.
type mkdirGate struct {
	mu    sync.Mutex
	gates map[string]chan struct{}
}

func newMkdirGate() *mkdirGate {
	return &mkdirGate{gates: make(map[string]chan struct{})}
}

// markSent records that dir's mkdir frame was written (success or fail) and
// wakes waiters. Idempotent; empty dir is a no-op (the target root exists,
// the frontend lists it before uploading).
func (g *mkdirGate) markSent(dir string) {
	if g == nil || dir == "" {
		return
	}
	g.mu.Lock()
	ch, ok := g.gates[dir]
	if !ok {
		ch = make(chan struct{})
		g.gates[dir] = ch
	}
	select {
	case <-ch:
	default:
		close(ch)
	}
	g.mu.Unlock()
}

// wait blocks until dir's mkdir frame was written. Empty dir returns
// immediately. A nil gate returns immediately (slice-built tasks carry no
// ordering requirement).
func (g *mkdirGate) wait(dir string) {
	if g == nil || dir == "" {
		return
	}
	g.mu.Lock()
	ch, ok := g.gates[dir]
	if !ok {
		ch = make(chan struct{})
		g.gates[dir] = ch
	}
	g.mu.Unlock()
	<-ch
}

// parentRemoteDir returns the remote parent of a sandboxed rel path (""
// for top-level entries, which need no mkdir wait).
func parentRemoteDir(remotePath string) string {
	if i := strings.LastIndex(remotePath, "/"); i >= 0 {
		return remotePath[:i]
	}
	return ""
}

// expandUploadTasksStreaming feeds one task per file as the walk discovers
// it instead of building the whole list first: the first byte leaves after
// the first file's stat+open, not after thousands. Tasks stream over out in
// discovery order; out is closed by the caller when this returns. A walk
// failure aborts discovery — already-fed tasks still stream (partial success
// is the runner's established semantic; the error is reported).
func (s *Service) expandUploadTasksStreaming(localPaths []string, remoteDir, policy, batchID string, gates *mkdirGate, out chan<- uploadTask) error {
	policy = normalizeWirePolicy(policy)
	stride := s.negotiatedChunkSize()
	wantsAck := s.peerSupportsFileAck()
	for _, lp := range localPaths {
		if s.isBatchCancelled(batchID) {
			return fmt.Errorf("transfer cancelled")
		}
		clean := filepath.Clean(lp)
		info, err := os.Stat(clean)
		if err != nil {
			return fmt.Errorf("stat local file: %w", err)
		}
		if info.IsDir() {
			if err := s.expandOneFolderStreaming(clean, remoteDir, policy, batchID, gates, out); err != nil {
				return err
			}
			continue
		}
		if info.Size() > core.MaxFileTotalSize {
			return fmt.Errorf("file too large (max 8 GiB): %s", filepath.Base(clean))
		}
		name := filepath.Base(clean)
		if _, ok := core.SanitizeFileName(name); !ok {
			name = "file"
		}
		remotePath := name
		if remoteDir != "" {
			remotePath = remoteDir + "/" + name
		}
		if _, ok := core.SanitizeFilePath(remotePath); !ok {
			return fmt.Errorf("invalid remote path: %s", remotePath)
		}
		out <- uploadTask{localPath: clean, remotePath: remotePath, policy: policy, info: info, gate: gates, chunkSize: stride, wantsAck: wantsAck}
	}
	return nil
}

// expandOneFolderStreaming mirrors expandOneFolder but feeds tasks as files
// are discovered and marks each visited dir's mkdir gate right after its
// mkdir frame is written (WalkDir visits parents before children, so a fed
// task's parent gate is always already closed).
func (s *Service) expandOneFolderStreaming(localDir, remoteDir, policy, batchID string, gates *mkdirGate, out chan<- uploadTask) error {
	base := filepath.Base(localDir)
	if _, ok := core.SanitizeFileName(base); !ok {
		base = "folder"
	}
	targetRemote := base
	if remoteDir != "" {
		targetRemote = remoteDir + "/" + base
	}
	if _, ok := core.SanitizeFilePath(targetRemote); !ok {
		return fmt.Errorf("invalid remote path: %s", targetRemote)
	}
	mkdir := core.FileMkdirPayload{Path: targetRemote}
	if err := s.sendFeatureToPhone(core.TypeFileMkdir, &mkdir); err != nil {
		s.appendLine("mkdir for folder failed path=" + targetRemote + " err=" + err.Error())
	}
	gates.markSent(targetRemote)
	stride := s.negotiatedChunkSize()
	wantsAck := s.peerSupportsFileAck()
	err := filepath.WalkDir(localDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if s.isBatchCancelled(batchID) {
			return fmt.Errorf("transfer cancelled")
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
			gates.markSent(remotePath)
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if fi.Size() > core.MaxFileTotalSize {
			return fmt.Errorf("file too large (max 8 GiB): %s", filepath.Base(path))
		}
		out <- uploadTask{localPath: path, remotePath: remotePath, policy: policy, info: fi, gate: gates, chunkSize: stride, wantsAck: wantsAck}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk folder: %w", err)
	}
	return nil
}

// uploadStreamBuffer caps tasks buffered between the walker and the send
// workers: enough to keep 8 workers fed through mkdir round-trips, small
// enough that cancel drains promptly.
const uploadStreamBuffer = 64

// uploadPathsStreaming runs the full folder-capable path with discovery
// overlapped with sending: the walker feeds tasks while workers stream
// them. The target root is pre-marked (the frontend lists it before
// uploading, so it exists). Walk errors fail closed after already-fed tasks
// stream; send errors report the first like runUploadTasks.
func (s *Service) uploadPathsStreaming(localPaths []string, remoteDir, policy, batchID string) error {
	gates := newMkdirGate()
	gates.markSent(remoteDir)
	taskCh := make(chan uploadTask, uploadStreamBuffer)
	var walkErr error
	go func() {
		walkErr = s.expandUploadTasksStreaming(localPaths, remoteDir, policy, batchID, gates, taskCh)
		close(taskCh)
	}()
	runErr := s.runUploadStream(taskCh, batchID)
	if walkErr != nil {
		return walkErr
	}
	return runErr
}

// runUploadStream consumes tasks until the channel closes, streaming each
// with bounded parallelism and pipelined acks like runUploadTasks. A
// cancelled batch skips undispatched tasks but keeps draining so the walker
// never blocks on a full channel forever.
func (s *Service) runUploadStream(tasks <-chan uploadTask, batchID string) error {
	sem := make(chan struct{}, maxParallelFileUploads)
	var wg sync.WaitGroup
	var firstMu sync.Mutex
	var firstErr error
	noteErr := func(err error) {
		if err == nil {
			return
		}
		firstMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		firstMu.Unlock()
	}
	for t := range tasks {
		if s.isBatchCancelled(batchID) {
			noteErr(fmt.Errorf("transfer cancelled"))
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(task uploadTask) {
			defer wg.Done()
			if s.isBatchCancelled(batchID) {
				<-sem
				noteErr(fmt.Errorf("transfer cancelled"))
				return
			}
			res, sendErr := s.sendUploadChunks(task, batchID)
			<-sem
			if sendErr != nil {
				noteErr(sendErr)
				return
			}
			if err := s.completeUploadSend(res); err != nil {
				noteErr(err)
			}
		}(t)
	}
	wg.Wait()
	return firstErr
}

// runUploadTasks streams tasks with bounded parallelism and returns the first
// error (fail-closed like the old sequential loop, which stopped at the first
// failure). A cancelled batch skips undispatched tasks; in-flight streams
// abort per chunk via isTransferCancelled. Workers share nothing but the
// Service (whose maps are fileMu-guarded and whose WS writes are
// mutex-serialized), so no additional locking is needed here.
//
// Pipelined acks: the send semaphore guards only chunk bytes. Each worker
// sends all chunks, releases its slot, then waits for the phone's file-ack
// without holding a slot — so 8 small files can commit concurrently while
// the next 8 already stream. Single-file batches keep the legacy blocking
// path (ack-confirmed before return) via UploadLocalFileToRemotePathInBatch.
func (s *Service) runUploadTasks(tasks []uploadTask, batchID string) error {
	if len(tasks) == 0 {
		return nil
	}
	if len(tasks) == 1 || maxParallelFileUploads <= 1 {
		_, err := s.UploadLocalFileToRemotePathInBatch(tasks[0].localPath, tasks[0].remotePath, tasks[0].policy, batchID)
		return err
	}
	sem := make(chan struct{}, maxParallelFileUploads)
	var wg sync.WaitGroup
	var firstMu sync.Mutex
	var firstErr error
	noteErr := func(err error) {
		if err == nil {
			return
		}
		firstMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		firstMu.Unlock()
	}
	for _, t := range tasks {
		if s.isBatchCancelled(batchID) {
			noteErr(fmt.Errorf("transfer cancelled"))
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(task uploadTask) {
			defer wg.Done()
			if s.isBatchCancelled(batchID) {
				<-sem
				noteErr(fmt.Errorf("transfer cancelled"))
				return
			}
			// Send phase holds the slot; ack wait below does not.
			res, sendErr := s.sendUploadChunks(task, batchID)
			<-sem
			if sendErr != nil {
				noteErr(sendErr)
				return
			}
			if err := s.completeUploadSend(res); err != nil {
				noteErr(err)
			}
		}(t)
	}
	wg.Wait()
	return firstErr
}

// pipelinedSend is one file whose chunk bytes are all on the wire (or failed
// before the last chunk) and whose delivery confirmation is still pending.
// The send slot is already released; completeUploadSend finishes the record.
type pipelinedSend struct {
	transferID  string
	batchID     string
	remotePath  string
	clean       string
	policy      string
	sourceMtime int64
	totalSize   int64
	totalChunks int
	chunkSize   int
	startedAt   time.Time
	waitAck     func(time.Duration) error
}

// sendUploadChunks streams one file's chunks without waiting for the phone's
// file-ack. It owns transfer registration, hashing, and send-side progress;
// the caller releases the send slot immediately after return and finishes
// via completeUploadSend. Send failures mark the transfer error and retain
// a resume session; the returned result is nil then.
func (s *Service) sendUploadChunks(task uploadTask, batchID string) (*pipelinedSend, error) {
	policy := normalizeWirePolicy(task.policy)
	clean := filepath.Clean(task.localPath)
	info := task.info
	if info == nil {
		var err error
		info, err = os.Stat(clean)
		if err != nil {
			return nil, fmt.Errorf("stat local file: %w", err)
		}
	}
	if info.IsDir() {
		return nil, fmt.Errorf("is directory: %s (use folder upload)", filepath.Base(clean))
	}
	if info.Size() > core.MaxFileTotalSize {
		return nil, fmt.Errorf("file too large (max 8 GiB): %s", filepath.Base(clean))
	}
	f, err := os.Open(clean)
	if err != nil {
		return nil, fmt.Errorf("open local file: %w", err)
	}
	defer func() { _ = f.Close() }()
	if task.info != nil {
		// The walk already statted this path: confirm against the open
		// handle (one fd syscall, no path lookup). A file that changed
		// between listing and sending fails closed instead of splicing two
		// different files into one transfer.
		if fi, ferr := f.Stat(); ferr != nil || fi.IsDir() || fi.Size() != info.Size() {
			return nil, fmt.Errorf("file changed since listing: %s", filepath.Base(clean))
		}
	}

	chunkSize := task.chunkSize
	wantsAckPeer := task.wantsAck
	if chunkSize <= 0 {
		// Ad-hoc single task: resolve live. Batch-built tasks carry the
		// once-per-batch snapshot so hundreds of files skip per-file locks.
		// Strides are per-transfer consistent, so a mid-batch peer change
		// stays valid (each transfer fixes its own stride for offsets and
		// any later resume).
		chunkSize = s.negotiatedChunkSize()
		wantsAckPeer = s.peerSupportsFileAck()
	}
	startedAt := time.Now()
	totalSize := info.Size()
	totalChunks := core.TotalChunksForSize(totalSize, chunkSize)
	transferID, err := freshTransferID()
	if err != nil {
		return nil, err
	}
	hasher := sha256.New()

	s.fileMu.Lock()
	if s.transfers == nil {
		s.transfers = make(map[string]*FileTransfer)
	}
	if s.pendingLists == nil {
		s.pendingLists = make(map[string]chan FileListResult)
	}
	s.transfers[transferID] = &FileTransfer{
		ID: transferID, Path: task.remotePath, Direction: "upload",
		TotalSize: totalSize, Status: "running", Source: "local",
		BatchID: batchID,
	}
	batched := batchID != ""
	s.fileMu.Unlock()
	// Batched 1-chunk files skip the register emit: the deferred completion
	// emit in completeUploadSend converges the UI with one frame per file
	// instead of start+chunk+done.
	if !batched {
		s.emitTransfersChanged()
	} else if totalChunks > 1 {
		s.emitTransfersChanged()
		s.emitBatchesChanged()
	}
	// Order the first chunk behind the parent mkdir on the shared WS. The
	// walker marks before feeding, so this resolves immediately in practice
	// and only ever blocks while the mkdir frame is genuinely in flight.
	task.gate.wait(parentRemoteDir(task.remotePath))

	// Small files allocate only what they hold instead of a full stride.
	bufSize := chunkSize
	if totalSize < int64(chunkSize) {
		bufSize = int(totalSize)
	}
	var buf []byte
	if bufSize > 0 {
		buf = make([]byte, bufSize)
	}
	for idx := 0; idx < totalChunks; idx++ {
		if s.isTransferCancelled(transferID) {
			s.failTransfer(transferID, "transfer cancelled")
			return nil, errors.New("transfer cancelled")
		}
		offset := int64(idx) * int64(chunkSize)
		n := 0
		if totalSize > 0 {
			var readErr error
			n, readErr = io.ReadFull(f, buf)
			if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) && !errors.Is(readErr, io.EOF) {
				s.failTransfer(transferID, readErr.Error())
				return nil, fmt.Errorf("read chunk %d: %w", idx, readErr)
			}
		}
		chunkRaw := buf[:n]
		if _, err := hasher.Write(chunkRaw); err != nil {
			s.failTransfer(transferID, err.Error())
			return nil, fmt.Errorf("hash chunk %d: %w", idx, err)
		}
		b64 := ""
		if len(chunkRaw) > 0 {
			b64 = base64.StdEncoding.EncodeToString(chunkRaw)
		}
		sha := ""
		if idx == totalChunks-1 {
			sha = hex.EncodeToString(hasher.Sum(nil))
		}
		payload := core.FileChunkPayload{
			TransferID:  transferID,
			Path:        task.remotePath,
			Offset:      offset,
			TotalSize:   totalSize,
			ChunkIndex:  idx,
			TotalChunks: totalChunks,
			DataB64:     b64,
			Sha256:      sha,
			Policy:      policy,
			SourceMtime: info.ModTime().Unix(),
		}
		var waitAck func(time.Duration) error
		if idx == totalChunks-1 && wantsAckPeer {
			waitAck = s.registerAckWaiter(transferID)
		}
		if err := s.sendFileChunkWithRetry(core.TypeFileChunk, &payload); err != nil {
			s.failTransfer(transferID, err.Error())
			s.saveNativeSession(clean, task.remotePath, policy, info, transferID, totalChunks, chunkSize)
			s.refreshBatchCompletion(batchID)
			return nil, fmt.Errorf("send chunk %d: %w", idx, err)
		}
		s.fileMu.Lock()
		if tr, ok := s.transfers[transferID]; ok {
			tr.DoneSize = offset + int64(n)
		}
		s.fileMu.Unlock()
		// Coalesced progress: multi-chunk streams emit every Nth chunk plus
		// the final send; single-chunk files converge on completion.
		if totalChunks > 1 && shouldEmitProgress(idx, totalChunks) {
			s.emitTransfersChanged()
			if batched {
				s.emitBatchesChanged()
			}
		}
		if waitAck != nil {
			return &pipelinedSend{
				transferID: transferID, batchID: batchID,
				remotePath: task.remotePath, clean: clean,
				policy: policy, sourceMtime: info.ModTime().Unix(),
				totalSize: totalSize, totalChunks: totalChunks,
				chunkSize: chunkSize, startedAt: startedAt,
				waitAck: waitAck,
			}, nil
		}
	}
	// Legacy peer (no ack): bytes are the commit.
	return &pipelinedSend{
		transferID: transferID, batchID: batchID,
		remotePath: task.remotePath, clean: clean,
		policy: policy, sourceMtime: info.ModTime().Unix(),
		totalSize: totalSize, totalChunks: totalChunks,
		chunkSize: chunkSize, startedAt: startedAt,
	}, nil
}

// completeUploadSend waits for delivery confirmation (when the peer acks)
// and marks the transfer done/error. It never holds the send semaphore:
// many files can commit concurrently while later files already stream.
func (s *Service) completeUploadSend(res *pipelinedSend) error {
	if res == nil {
		return errors.New("send chunk failed")
	}
	if res.waitAck != nil {
		if err := res.waitAck(fileAckTimeout); err != nil {
			s.failTransfer(res.transferID, err.Error())
			if info, statErr := os.Stat(res.clean); statErr == nil {
				s.saveNativeSession(res.clean, res.remotePath, res.policy, info, res.transferID, res.totalChunks, res.chunkSize)
			}
			s.refreshBatchCompletion(res.batchID)
			return fmt.Errorf("delivery not confirmed: %w", err)
		}
	}
	s.fileMu.Lock()
	if tr, ok := s.transfers[res.transferID]; ok && tr.Status == "running" {
		tr.Status = "done"
		tr.DoneSize = res.totalSize
		tr.completedAt = time.Now()
	}
	s.fileMu.Unlock()
	s.emitTransfersChanged()
	s.refreshBatchCompletion(res.batchID)
	elapsed := time.Since(res.startedAt)
	mbps := 0.0
	if elapsed > 0 && res.totalSize > 0 {
		mbps = float64(res.totalSize) / (1 << 20) / elapsed.Seconds()
	}
	s.appendLine(fmt.Sprintf("file upload done path=%s bytes=%d ms=%d mb_s=%.1f chunks=%d chunk_kb=%d",
		res.remotePath, res.totalSize, elapsed.Milliseconds(), mbps, res.totalChunks, res.chunkSize>>10))
	return nil
}
