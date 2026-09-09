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

func TestUploadPrefsRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc := NewService("{}", "fp", "tok", NewLogBuffer(20))
	if got := svc.GetDefaultUploadDir(); got != "" {
		t.Fatalf("default = %q, want empty", got)
	}
	msg, err := svc.SetDefaultUploadDir("Download")
	if err != nil || msg == "" {
		t.Fatalf("set default: %v %q", err, msg)
	}
	if got := svc.GetDefaultUploadDir(); got != "Download" {
		t.Fatalf("default = %q, want Download", got)
	}
	if _, err := svc.SetDefaultUploadDir("../evil"); err == nil {
		t.Fatal("traversal default must fail")
	}
	if _, err := svc.SetDefaultUploadDir(""); err != nil {
		t.Fatalf("clear default: %v", err)
	}
	if got := svc.GetDefaultUploadDir(); got != "" {
		t.Fatalf("default after clear = %q, want empty", got)
	}
}
