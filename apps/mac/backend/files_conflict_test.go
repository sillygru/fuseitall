// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"os"
	"path/filepath"
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
