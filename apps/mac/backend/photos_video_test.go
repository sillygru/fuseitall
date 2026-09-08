// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"testing"
)

func TestRangeAddMerges(t *testing.T) {
	var r [][2]int64
	r = rangeAdd(r, 0, 100)
	r = rangeAdd(r, 200, 100)
	if rangeBytes(r) != 200 {
		t.Fatalf("rangeBytes = %d, want 200", rangeBytes(r))
	}
	if rangeCovered(r, 50, 100) {
		t.Fatal("gap [100,200) reported covered")
	}
	r = rangeAdd(r, 50, 200)
	if !rangeCovered(r, 0, 300) {
		t.Fatal("merged [0,300) not covered")
	}
	if !rangeCovered(r, 0, 0) {
		t.Fatal("empty range must be covered")
	}
}

func TestPhotoExtForMime(t *testing.T) {
	cases := map[string]string{
		"image/jpeg": ".jpg", "video/mp4": ".mp4", "video/quicktime": ".mov",
		"video/3gpp": ".3gp", "video/x-matroska": ".mkv", "": ".bin",
		"application/octet-stream": ".bin", "video/unknown-x": ".mp4",
	}
	for mime, want := range cases {
		if got := photoExtForMime(mime, "vid:1"); got != want {
			t.Fatalf("photoExtForMime(%q) = %q, want %q", mime, got, want)
		}
	}
	if got := photoFileStem("vid:123"); got != "vid_123" {
		t.Fatalf("photoFileStem = %q, want vid_123", got)
	}
}

func TestParseSingleRange(t *testing.T) {
	if off, ln := parseSingleRange("", 1000); off != 0 || ln != 1000 {
		t.Fatalf("empty header = %d,%d, want 0,1000", off, ln)
	}
	if off, ln := parseSingleRange("bytes=100-199", 1000); off != 100 || ln != 100 {
		t.Fatalf("bytes=100-199 = %d,%d, want 100,100", off, ln)
	}
	if off, ln := parseSingleRange("bytes=900-", 1000); off != 900 || ln != 100 {
		t.Fatalf("bytes=900- = %d,%d, want 900,100", off, ln)
	}
	if off, ln := parseSingleRange("bytes=0-99999", 1000); off != 0 || ln != 1000 {
		t.Fatalf("overlong end = %d,%d, want 0,1000", off, ln)
	}
	if off, ln := parseSingleRange("bytes=abc", 1000); off != 0 || ln != 1000 {
		t.Fatalf("garbage header = %d,%d, want 0,1000", off, ln)
	}
}

func TestRequestPhotoRangeRejectsWithoutPair(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	if _, err := svc.RequestPhotoRange("vid:1", "video/mp4", 0, 1<<20); err == nil {
		t.Fatal("range pull while unpaired accepted")
	}
	if _, err := svc.RequestPhotoRange("vid:1", "video/mp4", 0, 0); err == nil {
		t.Fatal("zero-length range accepted")
	}
	if _, err := svc.RequestPhotoRange("../evil", "video/mp4", 0, 1<<20); err == nil {
		t.Fatal("traversal photo id accepted")
	}
}

func TestWaitPhotoRangePastEnd(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	svc.photoMu.Lock()
	svc.photoTransfers = map[string]*PhotoTransfer{
		"t1": {ID: "t1", PhotoID: "vid:1", Status: "running", TotalSize: 100, tmpPath: "/tmp/x"},
	}
	svc.photoMu.Unlock()
	if _, _, err := svc.waitPhotoRange("t1", 100, 10, 1000000000); err == nil {
		t.Fatal("range past end accepted")
	} else if err.Error() != "range past end of video" {
		t.Fatalf("err = %q, want range past end", err)
	}
}
