// SPDX-License-Identifier: AGPL-3.0-only

package backend

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// TestDemoNeverTouchesRealDisk guards the demo<->real switch: mutating
// anything in a demo session must leave pair.json, device.json,
// settings.json, and upload_prefs.json byte-identical so returning to real
// mode reconnects to the real phone with real settings.
func TestDemoNeverTouchesRealDisk(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	dir := filepath.Join(tmp, "Library", "Application Support", "FuseItAll")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	seed := map[string]string{
		"pair.json":         `{"identity_seed":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=","token":"real-token"}`,
		"device.json":       `{"host":"192.168.1.10","port":18789,"last_seen_unix":1700000000}`,
		"settings.json":     `{"notifications_enabled":true,"notif_mode":"all_except_muted","clipboard_mode":"both","playback_mode":"both","playback_output":"inapp","updated_unix":1700000000,"updated_by":"mac"}`,
		"upload_prefs.json": `{"default_upload_dir":"Download"}`,
	}
	before := map[string][]byte{}
	for name, body := range seed {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body+"\n"), 0o600); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("reread %s: %v", name, err)
		}
		before[name] = raw
	}

	logBuf := NewLogBuffer(50)
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	svc := NewDemoService(logBuf, logger)
	if !svc.IsDemoMode() {
		t.Fatal("demo service must report IsDemoMode true")
	}

	// Exercise every demo-reachable mutation.
	if _, err := svc.SetNotificationsEnabled(false); err != nil {
		t.Fatalf("demo set notif: %v", err)
	}
	if _, err := svc.SetClipboardMode("disabled"); err != nil {
		t.Fatalf("demo set clip mode: %v", err)
	}
	if _, err := svc.SetClipboardAllowSensitive(true); err != nil {
		t.Fatalf("demo set clip sensitive: %v", err)
	}
	if _, err := svc.SetNotifMode("only_allowed"); err != nil {
		t.Fatalf("demo set notif mode: %v", err)
	}
	if _, err := svc.SetAppMuted("com.example.app", true); err != nil {
		t.Fatalf("demo mute app: %v", err)
	}
	if _, err := svc.SetAppAllowed("com.example.app", true); err != nil {
		t.Fatalf("demo allow app: %v", err)
	}
	if _, err := svc.SetPlaybackMode("disabled"); err != nil {
		t.Fatalf("demo playback mode: %v", err)
	}
	if _, err := svc.SetPlaybackOutput("system"); err != nil {
		t.Fatalf("demo playback output: %v", err)
	}
	if _, err := svc.SetDefaultUploadDir("DCIM"); err != nil {
		t.Fatalf("demo upload default: %v", err)
	}
	if _, err := svc.SetCustomName("Demo Rename"); err != nil {
		t.Fatalf("demo rename: %v", err)
	}
	if _, err := svc.PushClipboard("demo"); err != nil {
		t.Fatalf("demo clipboard: %v", err)
	}

	// Demo serves in-memory values, not the seeded real ones.
	if got := svc.GetDefaultUploadDir(); got != "DCIM" {
		t.Fatalf("demo upload default should be in-memory DCIM, got %q", got)
	}
	if got := svc.GetSettings().ClipboardMode; got != "disabled" {
		t.Fatalf("demo settings should reflect in-memory change, got %q", got)
	}

	for name, want := range before {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("reread %s after demo: %v", name, err)
		}
		if string(raw) != string(want) {
			t.Fatalf("%s changed by demo mode:\nbefore %q\nafter  %q", name, want, raw)
		}
	}

	// Real service on the same HOME still sees the real phone + settings.
	real := NewService("{}", "fp", "real-token", NewLogBuffer(10))
	if real.IsDemoMode() {
		t.Fatal("real service must report IsDemoMode false")
	}
	if got := real.GetDefaultUploadDir(); got != "Download" {
		t.Fatalf("real upload default should stay Download, got %q", got)
	}
	if got := real.GetSettings().ClipboardMode; got != "both" {
		t.Fatalf("real settings should stay both, got %q", got)
	}
	last := real.GetLastDevice()
	if !last.HasDevice || last.Host != "192.168.1.10" {
		t.Fatalf("real last device should survive demo, got %+v", last)
	}
}
