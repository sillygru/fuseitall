// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fuseitall/core"
)

func TestNormalizeWirePolicy(t *testing.T) {
	if normalizeWirePolicy(core.FilePolicyOverwrite) != core.FilePolicyOverwrite {
		t.Fatal("overwrite must ride the wire")
	}
	if normalizeWirePolicy(core.FilePolicyIfNewer) != core.FilePolicyIfNewer {
		t.Fatal("if_newer must ride the wire")
	}
	for _, p := range []string{"", core.FilePolicySkip, core.FilePolicyKeepBoth, core.FilePolicyStop, "merge"} {
		if normalizeWirePolicy(p) != "" {
			t.Fatalf("policy %q must normalize to legacy keep-both", p)
		}
	}
}

func TestStatLocalFiles(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(fp, []byte("hi"), 0o600); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	infos, err := (&Service{}).StatLocalFiles([]string{fp})
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if len(infos) != 1 || infos[0].Name != "a.txt" || infos[0].Size != 2 || infos[0].IsDir {
		t.Fatalf("unexpected info: %+v", infos)
	}
	if _, err := (&Service{}).StatLocalFiles(nil); err == nil {
		t.Fatal("empty stat must fail")
	}
}

func TestBeginBrowserUploadValidation(t *testing.T) {
	s := &Service{}
	if _, err := s.BeginBrowserUpload("a.txt", "", "", core.MaxFileTotalSize+1, "", 0); err == nil {
		t.Fatal("over-cap begin must fail")
	}
	if _, err := s.BeginBrowserUpload("a.txt", "", "../evil", 10, "", 0); err == nil {
		t.Fatal("bad remote dir must fail")
	}
	if _, err := s.BeginBrowserUploadToPath("", 10, "", 0); err == nil {
		t.Fatal("empty remote path must fail")
	}
	if _, err := s.BeginBrowserUploadToPath("ok.txt", -1, "", 0); err == nil {
		t.Fatal("negative size must fail")
	}
}

func TestSendBrowserChunkUnknown(t *testing.T) {
	s := &Service{}
	if _, err := s.SendBrowserChunk("0123456789abcdef", "eA=="); err == nil {
		t.Fatal("unknown session must fail")
	}
	if _, err := s.AbortBrowserUpload("0123456789abcdef"); err == nil {
		t.Fatal("unknown abort must fail")
	}
}

func TestSweepBrowserUploads(t *testing.T) {
	s := &Service{
		transfers:      map[string]*FileTransfer{"id1": {ID: "id1", Status: "running"}},
		browserUploads: map[string]*BrowserUploadSession{"id1": {ID: "id1", UpdatedAt: time.Now().Add(-time.Hour)}},
	}
	s.sweepBrowserUploads(time.Now())
	if _, ok := s.browserUploads["id1"]; ok {
		t.Fatal("stale session must be swept")
	}
	if s.transfers["id1"].Status != "error" {
		t.Fatalf("stale transfer status = %q, want error", s.transfers["id1"].Status)
	}
}

func TestUploadDecidedFilesValidation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	if _, err := svc.UploadDecidedFiles(nil, ""); err == nil {
		t.Fatal("empty decided list must fail")
	}
	if _, err := svc.UploadDecidedFiles([]DecidedUpload{}, ""); err == nil {
		t.Fatal("empty decided list must fail")
	}
	many := make([]DecidedUpload, 10001)
	if _, err := svc.UploadDecidedFiles(many, ""); err == nil {
		t.Fatal("oversize decided list must fail")
	}
	one := []DecidedUpload{{LocalPath: "/tmp/x", RemotePath: "x", Policy: ""}}
	if _, err := svc.UploadDecidedFiles(one, ""); err == nil {
		t.Fatal("offline decided upload must fail")
	}
	if _, err := svc.UploadDecidedFiles(one, "nope"); err == nil {
		t.Fatal("offline decided upload must fail before batch lookup")
	}
}

func TestUploadDecidedFilesRejectsBadEntries(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	// Fake a live peer without network: presence fields only, same package.
	svc.mu.Lock()
	svc.peerHost, svc.peerPort, svc.lastSeen = "127.0.0.1", 1, time.Now()
	svc.mu.Unlock()
	dir := t.TempDir()
	if _, err := svc.UploadDecidedFiles([]DecidedUpload{{LocalPath: dir, RemotePath: "d", Policy: ""}}, ""); err == nil {
		t.Fatal("directory entry must fail closed")
	}
	if _, err := svc.UploadDecidedFiles([]DecidedUpload{{LocalPath: filepath.Join(dir, "nope"), RemotePath: "x", Policy: ""}}, ""); err == nil {
		t.Fatal("missing local file must fail")
	}
	if _, err := svc.UploadDecidedFiles([]DecidedUpload{{LocalPath: filepath.Join(dir, "nope"), RemotePath: "../esc", Policy: ""}}, ""); err == nil {
		t.Fatal("escaping remote path must fail")
	}
	f := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(f, []byte("hi"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if _, err := svc.UploadDecidedFiles([]DecidedUpload{{LocalPath: f, RemotePath: "", Policy: ""}}, ""); err == nil {
		t.Fatal("empty remote path must fail")
	}
}

func TestStatLocalFilesRejectsEmptyDrop(t *testing.T) {
	s := &Service{}
	for _, bad := range [][]string{{""}, {"  "}, {""}} {
		if _, err := s.StatLocalFiles(bad); !errors.Is(err, ErrInvalidLocal) {
			t.Fatalf("StatLocalFiles(%q) err = %v, want ErrInvalidLocal", bad, err)
		}
	}
}

func TestUploadDecidedFilesRejectsEmptyDrop(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	svc.mu.Lock()
	svc.peerHost, svc.peerPort, svc.lastSeen = "127.0.0.1", 1, time.Now()
	svc.mu.Unlock()
	_, err := svc.UploadDecidedFiles([]DecidedUpload{{LocalPath: "", RemotePath: "x", Policy: ""}}, "")
	if !errors.Is(err, ErrInvalidLocal) {
		t.Fatalf("empty LocalPath err = %v, want ErrInvalidLocal", err)
	}
	if strings.HasSuffix(err.Error(), ": ") || err.Error() == "invalid local path: " {
		t.Fatalf("empty-drop message must carry a hint, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "empty drop") {
		t.Fatalf("empty-drop message must hint re-select, got %q", err.Error())
	}
	_, err = svc.UploadDecidedFiles([]DecidedUpload{{LocalPath: "   ", RemotePath: "x", Policy: ""}}, "")
	if !errors.Is(err, ErrInvalidLocal) {
		t.Fatalf("whitespace LocalPath err = %v, want ErrInvalidLocal", err)
	}
}

func TestDecidedUploadUnmarshalJSON(t *testing.T) {
	// CamelCase from JS/Wails frontend
	camelJSON := `[{"localPath":"/tmp/foo.txt","remotePath":"remote/foo.txt","policy":"overwrite"}]`
	var fromCamel []DecidedUpload
	if err := json.Unmarshal([]byte(camelJSON), &fromCamel); err != nil {
		t.Fatalf("unmarshal camel: %v", err)
	}
	if len(fromCamel) != 1 {
		t.Fatalf("expected 1 item, got %d", len(fromCamel))
	}
	if fromCamel[0].LocalPath != "/tmp/foo.txt" || fromCamel[0].RemotePath != "remote/foo.txt" || fromCamel[0].Policy != "overwrite" {
		t.Fatalf("unexpected camel unmarshal result: %+v", fromCamel[0])
	}

	// Snake_case from Go tags / bindings
	snakeJSON := `[{"local_path":"/tmp/bar.txt","remote_path":"remote/bar.txt","policy":""}]`
	var fromSnake []DecidedUpload
	if err := json.Unmarshal([]byte(snakeJSON), &fromSnake); err != nil {
		t.Fatalf("unmarshal snake: %v", err)
	}
	if len(fromSnake) != 1 {
		t.Fatalf("expected 1 item, got %d", len(fromSnake))
	}
	if fromSnake[0].LocalPath != "/tmp/bar.txt" || fromSnake[0].RemotePath != "remote/bar.txt" || fromSnake[0].Policy != "" {
		t.Fatalf("unexpected snake unmarshal result: %+v", fromSnake[0])
	}

	// Integration test with svc.UploadDecidedFiles: camelCase payload must NOT fail with ErrInvalidLocal
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	svc.mu.Lock()
	svc.peerHost, svc.peerPort, svc.lastSeen = "127.0.0.1", 1, time.Now()
	svc.mu.Unlock()

	dir := t.TempDir()
	f := filepath.Join(dir, "camel_test.txt")
	if err := os.WriteFile(f, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write temp: %v", err)
	}

	ipcPayload := fmt.Sprintf(`[{"localPath":%q,"remotePath":"camel_test.txt","policy":""}]`, f)
	var decidedFromIPC []DecidedUpload
	if err := json.Unmarshal([]byte(ipcPayload), &decidedFromIPC); err != nil {
		t.Fatalf("unmarshal IPC: %v", err)
	}
	// Upload should not fail with ErrInvalidLocal (may fail later if network is mock/offline, but not invalid local path)
	_, err := svc.UploadDecidedFiles(decidedFromIPC, "")
	if errors.Is(err, ErrInvalidLocal) {
		t.Fatalf("UploadDecidedFiles rejected valid unmarshaled localPath with ErrInvalidLocal: %v", err)
	}
}

func TestNotifyLocalNetworkDownDropsPeer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	svc.setPeer("192.168.1.5", 18790)
	if !svc.IsPaired() {
		t.Fatal("setup must be paired")
	}
	msg, err := svc.NotifyLocalNetworkDown()
	if err != nil {
		t.Fatalf("down must succeed: %v", err)
	}
	if msg == "" {
		t.Fatal("down must return a message")
	}
	if svc.IsPaired() || svc.GetPeerAddr() != "" {
		t.Fatal("down must drop the ephemeral peer immediately")
	}
	if got := svc.GetPeerDevice(); got.HasDevice {
		t.Fatalf("down must clear peer device: %+v", got)
	}
	// Remembered device survives for the offline card + manual reconnect.
	if got := svc.GetLastDevice(); !got.HasDevice {
		t.Fatal("down must keep the remembered device")
	}
	msg2, err := svc.NotifyLocalNetworkDown()
	if err != nil {
		t.Fatalf("second down must succeed: %v", err)
	}
	if msg2 == "" {
		t.Fatal("second down must return a message")
	}
}
