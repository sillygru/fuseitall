// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package core

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func base64ForTest(n int) string {
	return base64.StdEncoding.EncodeToString([]byte(strings.Repeat("v", n)))
}

func TestSanitizePhotoID(t *testing.T) {
	if _, ok := SanitizePhotoID("12345"); !ok {
		t.Fatal("SanitizePhotoID(digits) = false, want true")
	}
	for _, bad := range []string{"", "../x", "a/b", "a\\b", ".", "..", string(make([]byte, MaxPhotoIDLen+1))} {
		if _, ok := SanitizePhotoID(bad); ok {
			t.Fatalf("SanitizePhotoID(%q) = true, want false", bad)
		}
	}
}

func TestSanitizePhotoList(t *testing.T) {
	if !SanitizePhotoList(PhotoListPayload{Nonce: "n", ReqID: "r", Limit: 100}) {
		t.Fatal("valid photo-list rejected")
	}
	if SanitizePhotoList(PhotoListPayload{Nonce: "", ReqID: "r"}) {
		t.Fatal("empty nonce accepted")
	}
	if SanitizePhotoList(PhotoListPayload{Nonce: "n", ReqID: "r", Limit: 999}) {
		t.Fatal("over-limit accepted")
	}
}

func TestSanitizePhotoListResp(t *testing.T) {
	ok := PhotoListRespPayload{
		Nonce: "n", ReqID: "r",
		Entries:    []PhotoEntry{{PhotoID: "1", TakenAt: 1}},
		ErrorCode:  ErrorCodePermissionDenied,
		Permission: PermissionPhotos,
	}
	if !SanitizePhotoListResp(ok) {
		t.Fatal("valid photo-list-resp rejected")
	}
	bad := ok
	bad.Permission = "bogus"
	if SanitizePhotoListResp(bad) {
		t.Fatal("bogus permission accepted")
	}
	bad = ok
	bad.ErrorCode = "bogus"
	if SanitizePhotoListResp(bad) {
		t.Fatal("bogus error_code accepted")
	}
}

func TestSanitizePhotoChunk(t *testing.T) {
	// Zero-byte photo: 1 chunk, empty data.
	zero := PhotoChunkPayload{
		Nonce: "n", TransferID: "0123456789abcdef",
		PhotoID: "1", TotalSize: 0, Offset: 0,
		ChunkIndex: 0, TotalChunks: 1,
	}
	if !SanitizePhotoChunk(zero) {
		t.Fatal("valid zero-byte photo-chunk rejected")
	}
	bad := zero
	bad.PhotoID = "../evil"
	if SanitizePhotoChunk(bad) {
		t.Fatal("traversal photo_id accepted")
	}
}

func TestParsePhotoID(t *testing.T) {
	if kind, row, ok := ParsePhotoID("12345"); !ok || kind != MediaTypePhoto || row != "12345" {
		t.Fatalf("ParsePhotoID(digits) = %q,%q,%v, want photo,12345,true", kind, row, ok)
	}
	if kind, row, ok := ParsePhotoID("img:12345"); !ok || kind != MediaTypePhoto || row != "12345" {
		t.Fatalf("ParsePhotoID(img:) = %q,%q,%v, want photo,12345,true", kind, row, ok)
	}
	if kind, row, ok := ParsePhotoID("vid:6789"); !ok || kind != MediaTypeVideo || row != "6789" {
		t.Fatalf("ParsePhotoID(vid:) = %q,%q,%v, want video,6789,true", kind, row, ok)
	}
	for _, bad := range []string{"", "vid:", "img:", "vid:../x", "vid:a/b", "vid:.", "img:vid:1"} {
		if _, _, ok := ParsePhotoID(bad); ok {
			t.Fatalf("ParsePhotoID(%q) = true, want false", bad)
		}
	}
	// Unknown prefixes stay opaque image IDs (fail closed at resolution).
	if kind, row, ok := ParsePhotoID("foo:1"); !ok || kind != MediaTypePhoto || row != "foo:1" {
		t.Fatalf("ParsePhotoID(foo:1) = %q,%q,%v, want photo,foo:1,true", kind, row, ok)
	}
}

func TestSanitizePhotoListRespVideo(t *testing.T) {
	vid := PhotoListRespPayload{
		Nonce: "n", ReqID: "r",
		Entries: []PhotoEntry{{
			PhotoID: "vid:42", TakenAt: 1700000000000,
			Mime: "video/mp4", Size: 1 << 20,
			MediaType: MediaTypeVideo, DurationMs: 61000,
		}},
	}
	if !SanitizePhotoListResp(vid) {
		t.Fatal("valid video entry rejected")
	}
	legacy := PhotoListRespPayload{
		Nonce: "n", ReqID: "r",
		Entries: []PhotoEntry{{PhotoID: "1", TakenAt: 1, Mime: "image/jpeg"}},
	}
	if !SanitizePhotoListResp(legacy) {
		t.Fatal("legacy entry without media_type rejected")
	}
	bad := vid
	bad.Entries[0].MediaType = "gif"
	if SanitizePhotoListResp(bad) {
		t.Fatal("bogus media_type accepted")
	}
	bad = vid
	bad.Entries[0].DurationMs = MaxVideoDurationMs + 1
	if SanitizePhotoListResp(bad) {
		t.Fatal("over-long duration accepted")
	}
	bad = vid
	bad.Entries[0].DurationMs = -1
	if SanitizePhotoListResp(bad) {
		t.Fatal("negative duration accepted")
	}
}

func TestSanitizePhotoPullReqRange(t *testing.T) {
	full := PhotoPullReqPayload{Nonce: "n", PhotoID: "vid:1"}
	if !SanitizePhotoPullReq(full) {
		t.Fatal("legacy full pull rejected")
	}
	rng := PhotoPullReqPayload{Nonce: "n", PhotoID: "vid:1", Offset: 1 << 20, Length: 1 << 20}
	if !SanitizePhotoPullReq(rng) {
		t.Fatal("valid range pull rejected")
	}
	for _, bad := range []PhotoPullReqPayload{
		{Nonce: "n", PhotoID: "vid:1", Offset: -1},
		{Nonce: "n", PhotoID: "vid:1", Length: -1},
		{Nonce: "n", PhotoID: "vid:1", Offset: MaxFileTotalSize + 1},
		{Nonce: "n", PhotoID: "vid:1", Length: MaxPhotoRangeLen + 1},
		{Nonce: "n", PhotoID: "vid:1", Offset: MaxFileTotalSize, Length: 1},
	} {
		if SanitizePhotoPullReq(bad) {
			t.Fatalf("bad range pull %+v accepted", bad)
		}
	}
}

func TestSanitizePhotoChunkRangeSubset(t *testing.T) {
	// Range pulls reuse absolute chunk_index/total_chunks, so a range tail
	// at the absolute end of the file validates standalone.
	tail := PhotoChunkPayload{
		Nonce: "n", TransferID: "0123456789abcdef",
		PhotoID: "vid:1", TotalSize: 10*MaxFileChunkRaw + 100, Offset: 10 * MaxFileChunkRaw,
		ChunkIndex: 10, TotalChunks: 11,
		DataB64: base64ForTest(100),
	}
	if !SanitizePhotoChunk(tail) {
		t.Fatal("valid absolute-index range tail rejected")
	}
	mid := PhotoChunkPayload{
		Nonce: "n", TransferID: "0123456789abcdef",
		PhotoID: "vid:1", TotalSize: 10 * MaxFileChunkRaw, Offset: 3 * MaxFileChunkRaw,
		ChunkIndex: 3, TotalChunks: 10, DataB64: "AA==",
	}
	if SanitizePhotoChunk(mid) {
		t.Fatal("short non-last chunk accepted")
	}
}

func TestSanitizePhotoDelete(t *testing.T) {
	if !SanitizePhotoDelete(PhotoDeletePayload{Nonce: "n", ReqID: "r", PhotoIDs: []string{"1", "2"}}) {
		t.Fatal("valid delete rejected")
	}
	if SanitizePhotoDelete(PhotoDeletePayload{Nonce: "n", ReqID: "r", PhotoIDs: []string{"1", "1"}}) {
		t.Fatal("duplicate ids accepted")
	}
	if SanitizePhotoDelete(PhotoDeletePayload{Nonce: "n", ReqID: "r"}) {
		t.Fatal("empty batch accepted")
	}
}

func TestPhotoMissingCapabilityGatesUpdate(t *testing.T) {
	// A 0.6.0-era peer (no photos cap) gets 426 on photo-list.
	srv, client := testPair(t, "mac")
	baseURL := testServer(t, srv)
	sender := SenderInfo{Platform: "android", AppBuild: 6, MinPeerBuild: CurrentMinPeerBuild}
	_, _, err := SendFeature(context.Background(), client, baseURL, srv.token, sender,
		[]string{CapabilityPing, CapabilityNotifications, CapabilityClipboard, CapabilitySettingsSync, CapabilityFiles},
		TypePhotoList, &PhotoListPayload{ReqID: "r1"})
	if !errors.Is(err, ErrPeerOutdated) {
		t.Fatalf("SendFeature(photo-list, no photos cap) = %v, want ErrPeerOutdated", err)
	}
	var upd *UpdateRequiredError
	if !errors.As(err, &upd) {
		t.Fatalf("err = %T, want *UpdateRequiredError", err)
	}
	if upd.RequiredBuild != CurrentBuild {
		t.Fatalf("RequiredBuild = %d, want %d", upd.RequiredBuild, CurrentBuild)
	}
}

func TestPhotoListRespPermissionFieldsSurvive(t *testing.T) {
	srv, client := testPair(t, "android")
	_ = srv
	_ = client
	p := PhotoListRespPayload{
		Nonce: "n", ReqID: "r", Error: "Photos access needed",
		ErrorCode: ErrorCodePermissionDenied, Permission: PermissionPhotos,
	}
	if !SanitizePhotoListResp(p) {
		t.Fatal("permission-denied resp rejected")
	}
	_ = time.Now
	_ = slog.Default
	_ = io.Discard
}
