// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package core

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

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
