// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"context"
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
		if !isPeerLost(err) {
			return "", err
		}
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", errors.New("no remembered phone reachable")
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
