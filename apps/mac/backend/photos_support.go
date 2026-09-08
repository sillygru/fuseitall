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
)

// Photo support helpers: staging paths, filename mapping, thumbnail LRU,
// and sparse-range interval math. No wire logic; see photos.go (transfers)
// and photos_stream.go (loopback range server).

// photoStagingRoot returns the isolated staging dir for photo downloads.
func photoStagingRoot() (string, error) {
	base := filepath.Join(os.TempDir(), "fuseitall-photos")
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", fmt.Errorf("mkdir photo staging: %w", err)
	}
	return base, nil
}

// photoExtForMime maps a listing mime to a download/stream file extension.
// Unknown mimes fall back to .bin so the file never lies about its type.
func photoExtForMime(mime, photoID string) string {
	m := strings.ToLower(strings.TrimSpace(mime))
	switch m {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/heic", "image/heif":
		return ".heic"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "video/quicktime":
		return ".mov"
	case "video/3gpp", "video/3gpp2":
		return ".3gp"
	case "video/x-matroska":
		return ".mkv"
	case "video/webm":
		return ".webm"
	}
	if strings.HasPrefix(m, "video/") {
		return ".mp4"
	}
	if strings.HasPrefix(m, "image/") {
		return ".jpg"
	}
	return ".bin"
}

// photoFileStem sanitizes a photo ID for filenames (colon is legal but
// confusing on some volumes; keep it filesystem-inert).
func photoFileStem(photoID string) string {
	stem := strings.ReplaceAll(photoID, ":", "_")
	stem = strings.ReplaceAll(stem, "/", "_")
	if stem == "" {
		stem = "item"
	}
	return stem
}

// photoPartPath returns the sparse part file for a transfer. Call with any
// lock state; reads immutable fields set at creation.
func photoPartPath(tr *PhotoTransfer) string {
	return tr.tmpPath + ".part." + tr.ID
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

// rangeAdd inserts [off, off+ln) into a sorted interval set, merging
// overlaps. Pure (caller holds photoMu for the transfer copy).
func rangeAdd(ranges [][2]int64, off, ln int64) [][2]int64 {
	if ln <= 0 {
		return ranges
	}
	start, end := off, off+ln
	out := make([][2]int64, 0, len(ranges)+1)
	inserted := false
	for _, r := range ranges {
		if r[1] < start {
			out = append(out, r)
			continue
		}
		if r[0] > end {
			if !inserted {
				out = append(out, [2]int64{start, end})
				inserted = true
			}
			out = append(out, r)
			continue
		}
		if r[0] < start {
			start = r[0]
		}
		if r[1] > end {
			end = r[1]
		}
	}
	if !inserted {
		out = append(out, [2]int64{start, end})
	}
	return out
}

// rangeBytes totals covered bytes. Pure.
func rangeBytes(ranges [][2]int64) int64 {
	var n int64
	for _, r := range ranges {
		n += r[1] - r[0]
	}
	return n
}

// rangeCovered reports whether [off, off+ln) is fully received. Pure.
func rangeCovered(ranges [][2]int64, off, ln int64) bool {
	if ln <= 0 {
		return true
	}
	end := off + ln
	for _, r := range ranges {
		if r[0] <= off && r[1] >= end {
			return true
		}
	}
	return false
}
