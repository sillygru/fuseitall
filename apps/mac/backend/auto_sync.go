// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"fuseitall/core"
)

// ReconnectToLastDevice redials the remembered phone even after the
// ephemeral peer expired or the app restarted. It tries the last host first,
// then remembered candidate IPs (DHCP changes), so a next-day / new-WiFi
// reconnect heals without a fresh QR scan. Success refreshes the peer
// (IsPaired flips true); dial failures keep the remembered device so the UI
// still shows "Last connected". Fail closed when no phone ever paired.
func (s *Service) ReconnectToLastDevice() (string, error) {
	s.mu.Lock()
	host, port, candidates := s.lastHost, s.lastPort, append([]string{}, s.candidateHosts...)
	s.mu.Unlock()
	if host == "" || port <= 0 {
		return "", errors.New("no last device yet — pair with the QR first")
	}
	targets := core.OrderedPeerTargets(host, port, candidates)
	var lastErr error
	for _, addr := range targets {
		h, p := SplitRemoteHost(addr), port
		if hh, pp, err := net.SplitHostPort(addr); err == nil {
			h = hh
			if n, aerr := strconv.Atoi(pp); aerr == nil {
				p = n
			}
		}
		msg, err := s.pingPhone(h, p, false)
		if err == nil {
			return msg, nil
		}
		lastErr = err
		// Cert mismatches and update gates are not dial failures: stop
		// trying other IPs, the route is right but identity/version needs
		// attention (logRotationOnce already recorded it).
		if !isPeerLost(err) {
			return "", err
		}
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", errors.New("no remembered phone reachable")
}

// clipboardWatchInterval is the change-check period for the Mac pasteboard
// watcher. macOS offers no clipboard-change event to a plain Go binary, so
// this polls the pasteboard hash — but it only pushes on actual content
// change (hash differs), making it event-driven in effect and idle-cheap:
// one pbpaste every 3s, zero network traffic unless the user copied.
const clipboardWatchInterval = 3 * time.Second

// StartClipboardWatcher watches the Mac pasteboard and pushes changes via
// PushClipboard (mode-gated downstream). It returns a stop func; the caller
// (main.go) owns lifecycle. Change-only: identical hashes never push, so an
// idle Mac costs one local pbpaste per tick and nothing else.
func (s *Service) StartClipboardWatcher(ctx context.Context) context.CancelFunc {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(clipboardWatchInterval)
		defer ticker.Stop()
		last := ""
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				text, ok := readPasteboard()
				if !ok {
					continue
				}
				hash := hashText(text)
				if hash == last {
					continue
				}
				last = hash
				if text == "" {
					continue
				}
				if _, ok := core.SanitizeClipText(text); !ok {
					continue
				}
				// PushClipboard logs "clipboard updated" and flushes when
				// the mode allows outbound flow; inbound-blocked modes keep
				// it local without network. Failures are log-only inside.
				_, _ = s.PushClipboard(text)
			}
		}
	}()
	return cancel
}

// readPasteboard returns the current plain-text pasteboard via pbpaste.
// ok=false when pbpaste is missing, fails, or holds no text. Bodies never
// reach the log; callers hash before comparing.
func readPasteboard() (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "pbpaste").Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

// writePasteboard writes plain-text to the system pasteboard via pbcopy.
// Bodies never reach the log; callers log lengths only. Returns false when
// pbcopy is missing or fails.
func writePasteboard(text string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pbcopy")
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// answerClipRequest pushes the current Mac clipboard when the phone asks
// (clip-request). Called on accepted requests only (WrapHandler 200 path).
// Mode-gated downstream by flushPendingToPhone; re-arms pending so even a
// previously-sent value is answered, preserving ordering.
func (s *Service) answerClipRequest() {
	notice := s.clips.Get()
	if !notice.HasText || notice.Text == "" {
		return
	}
	s.appendLine("clipboard requested by phone")
	// Re-arm pending: the current text may have no pending flag (already
	// sent or set before pairing). We must send it now without bumping
	// changedAt — the phone orders by changedAt, not send time.
	s.clips.mu.Lock()
	if s.clips.has {
		s.clips.pending = true
	}
	s.clips.mu.Unlock()
	s.flushPendingToPhone()
}

// notifyUser posts a best-effort macOS notification for a mirrored phone
// notification. Uses osascript display notification (no new deps, no TCC
// prompt for posting). Titles/bodies are passed as argv (never shell
// interpolated); failures are silent so mirror never breaks presence.
func notifyUser(title, body string) {
	if title == "" && body == "" {
		return
	}
	if title == "" {
		title = "FuseItAll"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "osascript", "-e",
		`on run argv
  display notification (item 2 of argv) with title (item 1 of argv)
end run`, title, body)
	_ = cmd.Run()
}

// discoveryAdvertisement builds the mDNS TXT record for this Mac's pair
// server (parsed by phones via core.ParseDiscoveryTXT). Host is filled by
// the caller (current LAN IP); port is the fixed pair-server port.
func discoveryAdvertisement(fingerprint, host string, port int) map[string]string {
	return core.BuildDiscoveryTXT(core.DiscoveryRecord{
		Fingerprint: fingerprint,
		Host:        host,
		Port:        port,
		Build:       core.CurrentBuild,
	})
}

var _ = fmt.Sprintf
var _ = discoveryAdvertisement
