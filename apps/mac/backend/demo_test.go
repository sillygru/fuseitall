// SPDX-License-Identifier: AGPL-3.0-only

package backend

import (
	"log/slog"
	"strings"
	"testing"
)

func TestDemoService(t *testing.T) {
	logBuf := NewLogBuffer(100)
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	svc := NewDemoService(logBuf, logger)

	// Pairing status & device
	if !svc.IsPaired() {
		t.Fatal("expected demo service to be paired")
	}
	peer := svc.GetPeerDevice()
	if !peer.HasDevice || peer.DeviceName != "Pixel 8 Pro" {
		t.Fatalf("unexpected peer device: %+v", peer)
	}
	if peer.BatteryPct == nil || *peer.BatteryPct != 84 {
		t.Fatalf("expected battery 84%%, got %v", peer.BatteryPct)
	}
	if peer.Charging == nil || !*peer.Charging {
		t.Fatal("expected charging to be true")
	}
	last := svc.GetLastDevice()
	if !last.HasDevice || last.DeviceName != "Pixel 8 Pro" {
		t.Fatalf("unexpected last device: %+v", last)
	}
	st := svc.GetPairStatus()
	if !st.Listening || st.QRHost != "192.168.1.142" {
		t.Fatalf("unexpected pair status: %+v", st)
	}

	// Rename & forget
	msg, err := svc.SetCustomName("My Test Phone")
	if err != nil || msg == "" {
		t.Fatalf("unexpected rename error: %v", err)
	}
	peer = svc.GetPeerDevice()
	if peer.CustomName != "My Test Phone" || peer.DisplayName != "My Test Phone" {
		t.Fatalf("expected custom name 'My Test Phone', got %q", peer.DisplayName)
	}

	// Files
	rootFiles, err := svc.ListPhoneFiles("")
	if err != nil || len(rootFiles.Entries) == 0 {
		t.Fatalf("failed to list root files: %v", err)
	}
	hasDocs := false
	for _, e := range rootFiles.Entries {
		if e.Name == "Documents" && e.IsDir {
			hasDocs = true
			break
		}
	}
	if !hasDocs {
		t.Fatal("expected Documents directory in root files")
	}

	docFiles, err := svc.ListPhoneFiles("Documents")
	if err != nil || len(docFiles.Entries) == 0 {
		t.Fatalf("failed to list Documents files: %v", err)
	}

	// Mkdir & Delete
	if _, err := svc.MkdirPhone("Documents/TestFolder"); err != nil {
		t.Fatalf("failed to mkdir: %v", err)
	}
	if _, err := svc.DeletePhone("Documents/TestFolder"); err != nil {
		t.Fatalf("failed to delete folder: %v", err)
	}

	// Photos
	photos, err := svc.ListPhonePhotos("", 50)
	if err != nil || len(photos.Entries) == 0 {
		t.Fatalf("failed to list photos: %v", err)
	}
	thumb, err := svc.RequestPhotoThumb("p01", 256)
	if err != nil || thumb.DataB64 == "" || thumb.Mime != "image/jpeg" {
		t.Fatalf("failed to get photo thumb: %v, %+v", err, thumb)
	}

	// Video streaming & download
	stream, err := svc.StartPhotoStream("v01", "video/mp4")
	if err != nil || stream.URL == "" {
		t.Fatalf("failed to start photo stream: %v", err)
	}
	rangeID, err := svc.RequestPhotoRange("v01", "video/mp4", 0, 1024)
	if err != nil || rangeID == "" {
		t.Fatalf("failed to request photo range: %v", err)
	}
	dlID, err := svc.RequestPhoneMedia("p01", "image/jpeg", t.TempDir())
	if err != nil || dlID == "" {
		t.Fatalf("failed to request phone media: %v", err)
	}

	// Contacts
	contacts, err := svc.ListContacts("", 50, false)
	if err != nil || len(contacts.Contacts) == 0 {
		t.Fatalf("failed to list contacts: %v", err)
	}
	c01Avatar, err := svc.GetContactAvatar("c01")
	if err != nil || c01Avatar.AvatarB64 == "" {
		t.Fatalf("failed to get contact avatar: %v", err)
	}
	lookup, err := svc.LookupContactForAddress("+1 (555) 014-2849")
	if err != nil || lookup["name"] != "Alex Rivera" {
		t.Fatalf("unexpected lookup result: %+v", lookup)
	}

	// Messages
	threads, err := svc.ListSMSThreads("", 50, false)
	if err != nil || len(threads.Threads) == 0 {
		t.Fatalf("failed to list threads: %v", err)
	}
	thread1Msgs, err := svc.ListSMSMessages(1, "", 50, false)
	if err != nil || len(thread1Msgs.Messages) == 0 {
		t.Fatalf("failed to list messages for thread 1: %v", err)
	}
	sendRes, err := svc.SendSMS("+1 (555) 014-2849", "Sounds great, see you there!")
	if err != nil || !sendRes.Ok {
		t.Fatalf("failed to send SMS: %v", err)
	}
	if err := svc.MarkThreadRead(1); err != nil {
		t.Fatalf("failed to mark thread read: %v", err)
	}

	// Playback & Controls
	pb := svc.GetPlayback()
	if !pb.HasState || pb.Title != "Midnight City" || pb.ArtworkB64 == "" || pb.ArtworkMime != "image/jpeg" {
		t.Fatalf("unexpected playback: %+v", pb)
	}
	if _, err := svc.SendPlaybackCmd("pause"); err != nil {
		t.Fatalf("failed to send playback pause: %v", err)
	}
	if _, err := svc.SendPlaybackCmd("play"); err != nil {
		t.Fatalf("failed to send playback play: %v", err)
	}

	// DND
	dnd := svc.GetDND()
	if !dnd.HasState {
		t.Fatal("expected DND state")
	}
	if _, err := svc.SetDND(true); err != nil {
		t.Fatalf("failed to set DND: %v", err)
	}
	if !svc.GetDND().Enabled {
		t.Fatal("expected DND enabled after toggle")
	}

	// Notifications
	notifs := svc.GetNotifications()
	if len(notifs.Items) == 0 {
		t.Fatal("expected demo notifications")
	}
	if _, err := svc.DismissNotification(notifs.Items[0].ID); err != nil {
		t.Fatalf("failed to dismiss notif: %v", err)
	}
	if _, err := svc.ClearNotifications(); err != nil {
		t.Fatalf("failed to clear notifications: %v", err)
	}

	// Clipboard
	clip := svc.GetClipboard()
	if clip.Text == "" {
		t.Fatal("expected demo clipboard text")
	}
	if _, err := svc.PushClipboard("https://example.com/test"); err != nil {
		t.Fatalf("failed to push clipboard: %v", err)
	}
	if svc.GetClipboard().Text != "https://example.com/test" {
		t.Fatalf("expected updated clipboard text")
	}

	// Known apps
	apps := svc.GetKnownNotifApps()
	if len(apps) == 0 {
		t.Fatal("expected known apps")
	}
	phoneApps, err := svc.RequestPhoneNotifApps(true)
	if err != nil || len(phoneApps) == 0 {
		t.Fatalf("failed to request phone apps: %v", err)
	}

	// Log snapshot
	logLines := svc.GetLog()
	if len(logLines) == 0 {
		t.Fatal("expected log lines")
	}
	hasDemoLog := false
	for _, l := range logLines {
		if strings.Contains(l, "demo mode active") {
			hasDemoLog = true
			break
		}
	}
	if !hasDemoLog {
		t.Fatalf("expected demo log line, got: %v", logLines)
	}
}
