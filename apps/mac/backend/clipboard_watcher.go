// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"context"
	"encoding/base64"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fuseitall/core"
)

// ClipboardWatcher polls the system pasteboard for auto sync. It only sends
// when paired and the current clipboard_mode allows Mac→phone sends.
// Polling uses pbpaste / osascript with debouncing so rapid copies coalesce.
// Idle cost is one timer tick per 1.2s; shell runs only on change.
type ClipboardWatcher struct {
	mu        sync.Mutex
	service   *Service
	stopCh    chan struct{}
	doneCh    chan struct{}
	running   bool
	lastText  string
	lastImage string // last image b64 hash key (mime+len) to dedupe
	ignoreEnd time.Time
	debMu   sync.Mutex
	timer     *time.Timer
	pending   string
	pendingIsImage  bool
	pendingMime     string
	pendingB64      string
	pendingFilename string
	hasPend         bool
}

// readPasteboard reads plain-text from the system clipboard via pbpaste.
// Var for tests.
var readPasteboard = func() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "pbpaste").Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// imagePasteboardResult holds a read image.
type imagePasteboardResult struct {
	Mime     string
	B64      string
	Filename string
}

// readPasteboardImage reads an image from the pasteboard via JXA (AppKit).
// Returns empty B64 when no image. Var for tests.
var readPasteboardImage = func() imagePasteboardResult {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	script := `ObjC.import("AppKit");
var pb = $.NSPasteboard.generalPasteboard;
var mimeMap = [
  ["public.png", "image/png"],
  ["public.jpeg", "image/jpeg"],
  ["public.tiff", "image/tiff"],
  ["com.compuserve.gif", "image/gif"],
  ["public.heic", "image/heic"],
  ["public.heif", "image/heic"],
  ["org.webmproject.webp", "image/webp"]
];
var result = "";
for (var i=0;i<mimeMap.length;i++) {
  var uti = mimeMap[i][0];
  var mime = mimeMap[i][1];
  var data = pb.dataForType(uti);
  if (data && data.length > 0) {
    var b64Data = data.base64EncodedDataWithOptions(0);
    var str = $.NSString.alloc.initWithDataEncoding(b64Data, $.NSUTF8StringEncoding);
    var js = ObjC.unwrap(str);
    // sanity: must be big enough to be real image
    if (js.length > 100) { result = mime + "|" + js; break; }
  }
}
result`
	out, err := exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-e", script).Output()
	if err != nil {
		return imagePasteboardResult{}
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || !strings.Contains(trimmed, "|") {
		return imagePasteboardResult{}
	}
	parts := strings.SplitN(trimmed, "|", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return imagePasteboardResult{}
	}
	mime, b64 := parts[0], strings.TrimSpace(parts[1])
	if len(b64) > core.MaxClipImageB64Len {
		return imagePasteboardResult{}
	}
	if _, ok := core.SanitizeClipImage(b64, mime); !ok {
		return imagePasteboardResult{}
	}
	return imagePasteboardResult{Mime: mime, B64: b64}
}

// readPasteboardFileImage reads an image file from a Finder file-url clipboard
// (public.file-url). Finder copies of image files expose file-url, not direct
// image data. Returns empty when no file image found.
var readPasteboardFileImage = func() imagePasteboardResult {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	script := `ObjC.import("AppKit");
var pb = $.NSPasteboard.generalPasteboard;
var s = pb.stringForType("public.file-url");
if (!s) s = pb.stringForType("public.file-URL");
s ? ObjC.unwrap(s) : ""`
	out, err := exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-e", script).Output()
	if err != nil {
		return imagePasteboardResult{}
	}
	rawURL := strings.TrimSpace(string(out))
	if rawURL == "" {
		return imagePasteboardResult{}
	}
	// osascript may return multiple URLs newline-separated; take first.
	if idx := strings.Index(rawURL, "\n"); idx >= 0 {
		rawURL = strings.TrimSpace(rawURL[:idx])
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "file" {
		return imagePasteboardResult{}
	}
	path := u.Path
	if path == "" {
		return imagePasteboardResult{}
	}
	// Security: only allow regular files, cap size, sniff magic.
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return imagePasteboardResult{}
	}
	if info.Size() == 0 || info.Size() > int64(core.MaxClipImageRaw) {
		return imagePasteboardResult{}
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 || len(data) > core.MaxClipImageRaw {
		return imagePasteboardResult{}
	}
	mime := core.SniffImageMime(data)
	if mime == "" {
		return imagePasteboardResult{}
	}
	b64 := base64.StdEncoding.EncodeToString(data)
	if len(b64) > core.MaxClipImageB64Len {
		return imagePasteboardResult{}
	}
	if _, ok := core.SanitizeClipImage(b64, mime); !ok {
		return imagePasteboardResult{}
	}
	filename := core.SanitizeClipFilename(filepath.Base(path))
	return imagePasteboardResult{Mime: mime, B64: b64, Filename: filename}
}

// NewClipboardWatcher builds a watcher bound to s.
func NewClipboardWatcher(s *Service) *ClipboardWatcher {
	return &ClipboardWatcher{service: s}
}

// Start launches the poll loop if not already running.
func (w *ClipboardWatcher) Start() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		return
	}
	w.running = true
	w.stopCh = make(chan struct{})
	w.doneCh = make(chan struct{})
	go w.loop()
}

// Stop halts the poll loop.
func (w *ClipboardWatcher) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	close(w.stopCh)
	done := w.doneCh
	w.running = false
	w.mu.Unlock()
	<-done
}

// NoteRemoteCopy tells the watcher to ignore the next echo of this text for a window.
func (w *ClipboardWatcher) NoteRemoteCopy(text string) {
	w.mu.Lock()
	w.lastText = text
	w.ignoreEnd = time.Now().Add(900 * time.Millisecond)
	w.mu.Unlock()
	w.debMu.Lock()
	if w.timer != nil {
		w.timer.Stop()
	}
	w.hasPend = false
	w.pending = ""
	w.pendingB64 = ""
	w.debMu.Unlock()
}

// NoteRemoteImage tells the watcher to ignore the next echo of this image.
func (w *ClipboardWatcher) NoteRemoteImage(b64, mime string) {
	w.mu.Lock()
	w.lastImage = mime + ":" + b64[:min(64, len(b64))]
	w.ignoreEnd = time.Now().Add(900 * time.Millisecond)
	w.mu.Unlock()
	w.debMu.Lock()
	if w.timer != nil {
		w.timer.Stop()
	}
	w.hasPend = false
	w.pending = ""
	w.pendingB64 = ""
	w.debMu.Unlock()
}

func (w *ClipboardWatcher) loop() {
	defer close(w.doneCh)
	ticker := time.NewTicker(1200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.tick()
		}
	}
}

func (w *ClipboardWatcher) tick() {
	if w.service == nil {
		return
	}
	if !w.service.IsPaired() {
		return
	}
	mode := w.service.settings.Get().ClipboardMode
	if mode == "" {
		mode = core.ClipboardBoth
	}
	if !core.ClipboardModeAllowsSend(mode, core.OriginMac) {
		return
	}
	// Prefer image over text: if image exists, use it (throttled).
	img := readPasteboardImage()
	if img.B64 == "" {
		// Finder file copy fallback: public.file-url -> image bytes.
		img = readPasteboardFileImage()
	}
	if img.B64 != "" {
		if len(img.B64) > core.MaxClipImageB64Len {
			return
		}
		if raw, ok := core.SanitizeClipImage(img.B64, img.Mime); ok {
			if len(raw) > core.ClipAutoImageRaw {
				// Visible feedback instead of silent drop.
				w.service.appendLine("clipboard auto image skipped (large, use Send clipboard)")
				return
			}
		} else {
			return
		}
		w.mu.Lock()
		key := img.Mime + ":" + img.B64[:min(64, len(img.B64))]
		if time.Now().Before(w.ignoreEnd) && key == w.lastImage {
			w.mu.Unlock()
			return
		}
		if w.lastImage == key {
			w.mu.Unlock()
			return
		}
		w.mu.Unlock()
		w.debMu.Lock()
		w.pendingMime = img.Mime
		w.pendingB64 = img.B64
		w.pendingFilename = core.SanitizeClipFilename(img.Filename)
		w.pendingIsImage = true
		w.hasPend = true
		if w.timer == nil {
			w.timer = time.AfterFunc(450*time.Millisecond, w.flushDebounced)
		} else {
			w.timer.Reset(450 * time.Millisecond)
		}
		w.debMu.Unlock()
		return
	}
	text := readPasteboard()
	if text == "" {
		return
	}
	if len(text) > core.MaxClipLen {
		return
	}
	w.mu.Lock()
	if time.Now().Before(w.ignoreEnd) && text == w.lastText {
		w.mu.Unlock()
		return
	}
	if w.lastText == text {
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()
	w.debMu.Lock()
	w.pending = text
	w.pendingIsImage = false
	w.hasPend = true
	if w.timer == nil {
		w.timer = time.AfterFunc(450*time.Millisecond, w.flushDebounced)
	} else {
		w.timer.Reset(450 * time.Millisecond)
	}
	w.debMu.Unlock()
}

func (w *ClipboardWatcher) flushDebounced() {
	w.debMu.Lock()
	if !w.hasPend {
		w.debMu.Unlock()
		return
	}
	isImage := w.pendingIsImage
	text := w.pending
	mime := w.pendingMime
	b64 := w.pendingB64
	filename := w.pendingFilename
	w.hasPend = false
	w.pending = ""
	w.pendingB64 = ""
	w.pendingFilename = ""
	w.pendingIsImage = false
	w.debMu.Unlock()
	if !w.service.IsPaired() {
		return
	}
	mode := w.service.settings.Get().ClipboardMode
	if mode == "" {
		mode = core.ClipboardBoth
	}
	if !core.ClipboardModeAllowsSend(mode, core.OriginMac) {
		return
	}
	now := time.Now().Unix()
	if cur := w.service.clips.Get(); cur.HasText && now <= cur.ChangedUnix {
		now = cur.ChangedUnix + 1
	}
	if isImage {
		if _, ok := w.service.clips.SetLocalImageWithFilename(b64, mime, filename, now); !ok {
			return
		}
		w.mu.Lock()
		w.lastImage = mime + ":" + b64[:min(64, len(b64))]
		w.mu.Unlock()
		w.service.appendLine("clipboard auto sync (image): staged")
		w.service.flushPendingToPhone()
		return
	}
	if text == "" || len(text) > core.MaxClipLen {
		return
	}
	if _, ok := w.service.clips.SetLocal(text, now); !ok {
		return
	}
	w.mu.Lock()
	w.lastText = text
	w.mu.Unlock()
	w.service.appendLine("clipboard auto sync: staged")
	w.service.flushPendingToPhone()
}
