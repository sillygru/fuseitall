// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"fuseitall/core"
)

// maxParallelFileUploads bounds concurrent file streams inside one batch.
// Chunks of different files interleave over the single WebSocket (each frame
// is one file-chunk envelope, serialized by the WS write mutex); the receiver
// reassembles by transfer_id+offset, so interleaving is safe. Three streams
// saturate a typical LAN without exhausting phone-side staging memory.
const maxParallelFileUploads = 3

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
// to an exact remote path with the batch's conflict policy.
type uploadTask struct {
	localPath  string
	remotePath string
	policy     string
}

// expandUploadTasks stats top-level drops, creates remote folders
// synchronously (mkdir is idempotent on the phone), and returns one task per
// file. Directories expand recursively. It sends no file bytes.
func (s *Service) expandUploadTasks(localPaths []string, remoteDir, policy, batchID string) ([]uploadTask, error) {
	policy = normalizeWirePolicy(policy)
	var tasks []uploadTask
	for _, lp := range localPaths {
		if s.isBatchCancelled(batchID) {
			return tasks, fmt.Errorf("transfer cancelled")
		}
		clean := filepath.Clean(lp)
		info, err := os.Stat(clean)
		if err != nil {
			return nil, fmt.Errorf("stat local file: %w", err)
		}
		if info.IsDir() {
			sub, err := s.expandOneFolder(clean, remoteDir, policy)
			if err != nil {
				return nil, err
			}
			tasks = append(tasks, sub...)
			continue
		}
		if info.Size() > core.MaxFileTotalSize {
			return nil, fmt.Errorf("file too large (max 8 GiB): %s", filepath.Base(clean))
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
			return nil, fmt.Errorf("invalid remote path: %s", remotePath)
		}
		tasks = append(tasks, uploadTask{localPath: clean, remotePath: remotePath, policy: policy})
	}
	return tasks, nil
}

// expandOneFolder ensures the remote folder exists and returns one task per
// file under localDir. Subdirectories are created remotely first so workers
// only stream bytes and never race on mkdir ordering.
func (s *Service) expandOneFolder(localDir, remoteDir, policy string) ([]uploadTask, error) {
	base := filepath.Base(localDir)
	if _, ok := core.SanitizeFileName(base); !ok {
		base = "folder"
	}
	targetRemote := base
	if remoteDir != "" {
		targetRemote = remoteDir + "/" + base
	}
	if _, ok := core.SanitizeFilePath(targetRemote); !ok {
		return nil, fmt.Errorf("invalid remote path: %s", targetRemote)
	}
	mkdir := core.FileMkdirPayload{Path: targetRemote}
	if err := s.sendFeatureToPhone(core.TypeFileMkdir, &mkdir); err != nil {
		s.appendLine("mkdir for folder failed path=" + targetRemote + " err=" + err.Error())
	}
	var tasks []uploadTask
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
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if fi.Size() > core.MaxFileTotalSize {
			return fmt.Errorf("file too large (max 8 GiB): %s", filepath.Base(path))
		}
		tasks = append(tasks, uploadTask{localPath: path, remotePath: remotePath, policy: policy})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk folder: %w", err)
	}
	return tasks, nil
}

// runUploadTasks streams tasks with bounded parallelism and returns the first
// error (fail-closed like the old sequential loop, which stopped at the first
// failure). A cancelled batch skips undispatched tasks; in-flight streams
// abort per chunk via isTransferCancelled. Workers share nothing but the
// Service (whose maps are fileMu-guarded and whose WS writes are
// mutex-serialized), so no additional locking is needed here.
func (s *Service) runUploadTasks(tasks []uploadTask, batchID string) error {
	if len(tasks) == 1 || maxParallelFileUploads <= 1 {
		_, err := s.UploadLocalFileToRemotePathInBatch(tasks[0].localPath, tasks[0].remotePath, tasks[0].policy, batchID)
		return err
	}
	sem := make(chan struct{}, maxParallelFileUploads)
	var wg sync.WaitGroup
	var firstMu sync.Mutex
	var firstErr error
	for _, t := range tasks {
		if s.isBatchCancelled(batchID) {
			firstMu.Lock()
			if firstErr == nil {
				firstErr = fmt.Errorf("transfer cancelled")
			}
			firstMu.Unlock()
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(task uploadTask) {
			defer wg.Done()
			defer func() { <-sem }()
			_, err := s.UploadLocalFileToRemotePathInBatch(task.localPath, task.remotePath, task.policy, batchID)
			if err != nil {
				firstMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				firstMu.Unlock()
				if strings.Contains(strings.ToLower(err.Error()), "cancelled") {
					return
				}
			}
		}(t)
	}
	wg.Wait()
	return firstErr
}
