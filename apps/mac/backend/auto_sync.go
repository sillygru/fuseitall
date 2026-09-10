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
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"fuseitall/core"
)

// clipFileURLKeep holds the last file-url temp image so Finder drags survive
// past the write call. The previous temp is deleted on the next write;
// stale files older than 1h are swept best-effort. In-memory only.
var clipFileURLKeep = struct {
	sync.Mutex
	path string
}{}

// retainClipFileURL keeps path alive for Finder drags, deleting the previous
// kept file. Call after a successful pasteboard write.
func retainClipFileURL(path string) {
	if path == "" {
		return
	}
	clipFileURLKeep.Lock()
	prev := clipFileURLKeep.path
	clipFileURLKeep.path = path
	clipFileURLKeep.Unlock()
	if prev != "" && prev != path {
		_ = os.Remove(prev)
	}
	sweepOldClipFileURLs()
}

// sweepOldClipFileURLs deletes fuse-clip-file-* temps older than 1h. Best-effort.
func sweepOldClipFileURLs() {
	dir := os.TempDir()
	if dir == "" {
		dir = "/tmp"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	now := time.Now()
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "fuse-clip-file-") {
			continue
		}
		full := filepath.Join(dir, name)
		clipFileURLKeep.Lock()
		kept := clipFileURLKeep.path
		clipFileURLKeep.Unlock()
		if full == kept {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > time.Hour {
			_ = os.Remove(full)
		}
	}
}

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

// writePasteboardImage writes a base64 image to the pasteboard via JXA (AppKit).
// Bytes-exact: preserves original PNG/JPEG/WEBP bytes (and EXIF/ICC) by calling
// setDataForType instead of NSImage.writeObjects (which re-encodes to uncompressed TIFF).
// Uses a temp file for the 7 MiB b64 payload to avoid JS literal injection and ARG_MAX.
// Also synthesizes a filename from mime when none provided so downstream saves get
// correct extension (never "unknown" with public.data).
// Returns false on failure. The image bytes are never logged.
func writePasteboardImage(b64, mime string) bool {
	return writePasteboardImageWithFilename(b64, mime, "")
}

// writePasteboardImageWithFilename writes image with optional filename for UTI/extension preservation.
func writePasteboardImageWithFilename(b64, mime, filename string) bool {
	if b64 == "" || mime == "" {
		return false
	}
	if _, ok := core.SanitizeClipImage(b64, mime); !ok {
		return false
	}
	// Map MIME to UTI for setDataForType. Fallback to PNG for unknown (already validated).
	utiForMime := map[string]string{
		"image/png":  "public.png",
		"image/jpeg": "public.jpeg",
		"image/jpg":  "public.jpeg",
		"image/webp": "org.webmproject.webp",
		"image/gif":  "com.compuserve.gif",
		"image/tiff": "public.tiff",
		"image/heic": "public.heic",
		"image/heif": "public.heic",
	}
	uti := utiForMime[strings.ToLower(strings.TrimSpace(mime))]
	if uti == "" {
		uti = "public.png"
	}
	// Write b64 to a temp file so JXA reads it without JS string interpolation (stable for 3.5 MiB+).
	tmpDir := os.TempDir()
	if tmpDir == "" {
		tmpDir = "/tmp"
	}
	// security: use os.CreateTemp which is 0600, not world-readable; clean up after.
	f, err := os.CreateTemp(tmpDir, "fuse-clip-*.b64")
	if err != nil {
		return false
	}
	tmpPath := f.Name()
	// Ensure path is local (no traversal) — CreateTemp guarantees it, but validate for future-proofing.
	if !filepath.IsLocal(filepath.Base(tmpPath)) {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return false
	}
	if _, err := f.WriteString(b64); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return false
	}
	_ = f.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	// Synthesize filename for downstream consumers that expect file-url.
	// This ensures Drag-to-Downloads / Finder gives correct extension even when
	// original copy had no filename (browser image).
	safeFilename := core.SanitizeClipFilename(filename)
	if safeFilename == "" {
		// Deterministic fallback, never "unknown".
		safeFilename = "clip-" + strings.TrimPrefix(core.ClipImageExt(mime), ".") + core.ClipImageExt(mime)
		// e.g. clip-tiff.tiff, clip-png.png — simple stable name.
		if safeFilename == "clip-.png" {
			safeFilename = "clip.png"
		}
	}
	// Prepare file-url temp image so Finder drag preserves extension.
	// Write decoded bytes to a temp file and expose via public.file-url alongside UTI.
	// The temp is retained past this call (see retainClipFileURL) so a later
	// Finder drag still resolves; the previous kept file is deleted on the
	// next write.
	fileURITemp := ""
	if raw, ok := core.SanitizeClipImage(b64, mime); ok {
		ext := core.ClipImageExt(mime)
		if f2, err2 := os.CreateTemp("", "fuse-clip-file-*"+ext); err2 == nil {
			_ = f2.Chmod(0600)
			if _, werr := f2.Write(raw); werr == nil {
				fileURITemp = f2.Name()
			}
			_ = f2.Close()
			if fileURITemp == "" {
				_ = os.Remove(f2.Name())
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// JXA reads the temp file via NSString.stringWithContentsOfFile, then sets dataForType bytes-exact.
	// Escape tmpPath for JS string literal: JSON-quoted.
	fileURLSnippet := ""
	if fileURITemp != "" {
		fileURLSnippet = `
    // Also set file-url for Finder/Preview drags so basename/extension survive.
    var filePath = ` + strconv.Quote(fileURITemp) + `;
    var fileURL = $.NSURL.fileURLWithPath(filePath);
    if (fileURL) {
      var urlStr = fileURL.absoluteString;
      if (urlStr) pb.setStringForType(ObjC.unwrap(urlStr), "public.file-url");
    }`
	}
	script := `ObjC.import("AppKit");
var path = ` + strconv.Quote(tmpPath) + `;
var uti = ` + strconv.Quote(uti) + `;
var b64Str = $.NSString.stringWithContentsOfFileEncodingError(path, $.NSUTF8StringEncoding, null);
if (!b64Str) { "no file"; } else {
  var js = ObjC.unwrap(b64Str);
  // Trim whitespace/newlines that NSString may preserve.
  js = js.trim();
  var data = $.NSData.alloc.initWithBase64EncodedStringOptions(js, 0);
  if (!data || data.length == 0) { "no data"; } else {
    var pb = $.NSPasteboard.generalPasteboard;
    pb.clearContents;
    var ok = pb.setDataForType(data, uti);
    // Also set generic public.image for broad compatibility
    if (ok) { try { pb.setDataForType(data, "public.image"); } catch(e) {} }
    ` + fileURLSnippet + `
    if (ok) { "ok"; } else {
      // Fallback for apps that only accept NSImage TIFF (compat): writeObjects
      var img = $.NSImage.alloc.initWithData(data);
      if (!img) { "no img"; } else {
        pb.clearContents;
        var ok2 = pb.writeObjects($.NSArray.arrayWithObject(img));
        ok2 ? "ok" : "fail";
      }
    }
  }
}`
	out, err := exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-e", script).Output()
	if err != nil {
		if fileURITemp != "" {
			_ = os.Remove(fileURITemp)
		}
		return false
	}
	if strings.TrimSpace(string(out)) != "ok" {
		if fileURITemp != "" {
			_ = os.Remove(fileURITemp)
		}
		return false
	}
	retainClipFileURL(fileURITemp)
	return true
}

// notifyUser posts a best-effort macOS notification for a mirrored phone
// notification. Deprecated: use notifyUserWithIcon. Kept for internal callers.
// Now delegates to the bundle-attributed notifier so the banner shows as
// FuseItAll (com.fuseitall.mac) instead of Script Editor.
func notifyUser(title, body string) {
	notifyUserInternal(title, body, "")
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
