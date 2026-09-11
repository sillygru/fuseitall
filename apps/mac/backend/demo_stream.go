// SPDX-License-Identifier: AGPL-3.0-only

package backend

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fuseitall/core"
)

// demoStartPhotoStream sets up a local in-memory/disk loopback video stream
// for demo mode using the bundled sample MP4 data, completely offline.
func (s *Service) demoStartPhotoStream(photoID, mime string) (PhotoStreamStart, error) {
	if _, ok := core.SanitizePhotoID(photoID); !ok {
		return PhotoStreamStart{}, errors.New("invalid photo id")
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
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return PhotoStreamStart{}, fmt.Errorf("mkdir staging: %w", err)
	}

	ext := photoExtForMime(mime, photoID)
	if ext == "" {
		ext = ".mp4"
	}
	partPath := filepath.Join(staging, "stream-"+photoFileStem(photoID)+ext)
	videoData := demoSampleMP4()
	total := int64(len(videoData))
	if err := os.WriteFile(partPath, videoData, 0o644); err != nil {
		return PhotoStreamStart{}, fmt.Errorf("write demo stream: %w", err)
	}

	s.photoMu.Lock()
	if s.photoTransfers == nil {
		s.photoTransfers = make(map[string]*PhotoTransfer)
	}
	if s.photoWaiters == nil {
		s.photoWaiters = make(map[string]chan error)
	}
	tr := newPhotoTransfer(transferID, photoID, partPath)
	tr.IsRange = true
	tr.Mime = "video/mp4"
	tr.TotalSize = total
	tr.DoneSize = total
	tr.Ranges = [][2]int64{{0, total}}
	tr.Status = "completed"
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

	url := fmt.Sprintf("http://127.0.0.1:%d/v/%s?token=%s", srv.port, transferID, token)
	return PhotoStreamStart{TransferID: transferID, URL: url}, nil
}

// demoRequestPhotoRange satisfies range requests in demo mode without talking to a phone.
func (s *Service) demoRequestPhotoRange(photoID, mime string, offset, length int64) (string, error) {
	_ = offset
	_ = length
	started, err := s.demoStartPhotoStream(photoID, mime)
	if err != nil {
		return "", err
	}
	return started.TransferID, nil
}

// demoRequestPhoneMedia allows downloading photos and videos in demo mode.
func (s *Service) demoRequestPhoneMedia(photoID, mime, downloadDir string) (string, error) {
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

	ext := photoExtForMime(mime, photoID)
	prefix := "photo-"
	var data []byte
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(mime)), "video/") {
		prefix = "video-"
		data = demoSampleMP4()
	} else {
		b64 := demoPhotoHiResB64(photoID)
		b, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return "", fmt.Errorf("decode demo photo: %w", err)
		}
		data = b
	}

	finalName := prefix + photoFileStem(photoID) + ext
	dst := filepath.Join(dir, finalName)
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", fmt.Errorf("write demo media: %w", err)
	}

	total := int64(len(data))
	s.photoMu.Lock()
	if s.photoTransfers == nil {
		s.photoTransfers = make(map[string]*PhotoTransfer)
	}
	tr := newPhotoTransfer(transferID, photoID, dst)
	tr.TotalSize = total
	tr.DoneSize = total
	tr.Status = "completed"
	s.photoTransfers[transferID] = tr
	s.photoMu.Unlock()

	s.emitPhotoTransfersChanged()
	return transferID, nil
}
