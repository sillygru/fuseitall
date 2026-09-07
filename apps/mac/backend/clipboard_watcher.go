// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"context"
	"os/exec"
	"sync"
	"time"

	"fuseitall/core"
)

// ClipboardWatcher polls the system pasteboard for auto sync. It only sends
// when paired and the current clipboard_mode allows Mac→phone sends.
// Polling uses pbpaste with debouncing so rapid copies coalesce to the latest
// text. Idle cost is one timer tick per 1.2s; pbpaste runs only on change.
type ClipboardWatcher struct {
	mu        sync.Mutex
	service   *Service
	stopCh    chan struct{}
	doneCh    chan struct{}
	running   bool
	lastText  string
	ignoreEnd time.Time
	debMu   sync.Mutex
	timer     *time.Timer
	pending   string
	hasPend   bool
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
	text := w.pending
	w.hasPend = false
	w.pending = ""
	w.debMu.Unlock()
	if text == "" || len(text) > core.MaxClipLen {
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
	now := time.Now().Unix()
	if cur := w.service.clips.Get(); cur.HasText && now <= cur.ChangedUnix {
		now = cur.ChangedUnix + 1
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
