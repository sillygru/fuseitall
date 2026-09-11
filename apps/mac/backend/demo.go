// SPDX-License-Identifier: AGPL-3.0-only

package backend

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"fuseitall/core"
)

// demoState holds in-memory mock datasets so demo mode is fully interactive,
// stateful, and completely decoupled from any network, phone, or private disk files.
type demoState struct {
	mu            sync.Mutex
	files         map[string][]FileEntryView
	photos        []PhotoEntryView
	contacts      []core.ContactEntry
	threads       []core.SMSThread
	messages      map[int64][]core.SMSMessage
	notifs        []NotifItem
	unseenNotifs  int
	nextMessageID int64
	// uploadDir is the demo-local default upload folder (Mac-local, never
	// synced). Kept here so demo never reads/writes upload_prefs.json.
	uploadDir string
}

func newDemoState() *demoState {
	notifs := defaultDemoNotifications()
	return &demoState{
		files:         defaultDemoFiles(),
		photos:        defaultDemoPhotos(),
		contacts:      defaultDemoContacts(),
		threads:       defaultDemoThreads(),
		messages:      defaultDemoMessages(),
		notifs:        notifs,
		unseenNotifs:  len(notifs),
		nextMessageID: 1000,
	}
}

// NewDemoService initializes a Service instance pre-loaded with mock data
// for screenshots, layout evaluation, and local UI development. Real disk
// state, network listeners, and system clipboard monitoring remain dormant.
func NewDemoService(logBuf *LogBuffer, logger *slog.Logger) *Service {
	_ = logger
	now := time.Now()

	s := &Service{
		pairJSON:         `{"device_name":"Pixel 8 Pro","platform":"android","host":"192.168.1.142","port":18789,"code":"DEMO-8492"}`,
		fingerprint:      "DEMO-SHA256-FINGERPRINT-8492",
		token:            "demo-token",
		logs:             logBuf,
		peerHost:         "192.168.1.142",
		peerPort:         18789,
		lastHost:         "192.168.1.142",
		lastPort:         18789,
		lastSeen:         now,
		lastDeviceSeen:   now,
		deviceName:       "Pixel 8 Pro",
		deviceModel:      "Pixel 8 Pro (Android 15)",
		batteryPct:       84,
		hasBattery:       true,
		charging:         true,
		batteryAt:        now,
		filesPermission:  "granted",
		photosPermission: "granted",
		settings:         NewInMemorySettingsStore(),
		notifs:           NewNotifStore(),
		clips:            NewClipStore(),
		playback:         NewPlaybackStore(),
		dnd:              NewDNDStore(),
		clipChunks:       NewClipChunkHub(),
		demoMode:         true,
		demo:             newDemoState(),
	}

	// Seed demo playback (Spotify · Midnight City)
	s.playback.ApplyRemote(core.PlaybackStatePayload{
		Nonce:       "demo-playback-init",
		Title:       "Midnight City",
		Artist:      "M83",
		Album:       "Hurry Up, We're Dreaming",
		PackageName: "com.spotify.music",
		App:         "Spotify",
		State:       core.PlaybackPlaying,
		PositionMs:  92000,
		DurationMs:  244000,
		UpdatedMs:   now.UnixMilli(),
		Origin:      core.OriginMac,
		ArtworkB64:  demoSongCoverArtB64(),
		ArtworkMime: "image/jpeg",
	})

	// Seed demo clipboard
	s.clips.SetLocal("https://fuseitall.org", now.Unix())

	// Seed demo DND
	s.dnd.ApplyRemote(core.DNDStatePayload{
		Enabled:       false,
		HasPermission: true,
		UpdatedMs:     now.UnixMilli(),
	})

	// Log lines reflecting demo activity
	if logBuf != nil {
		lines := []byte("demo mode active (real data & phone connection disabled)\n" +
			"link established to Pixel 8 Pro over Wi-Fi (5 GHz)\n" +
			"battery level 84% · fast charging\n" +
			"contacts synchronized (24 entries)\n" +
			"messages synchronized (14 active threads)\n" +
			"media session connected: Spotify · Midnight City\n")
		_, _ = logBuf.Write(lines)
	}

	return s
}

func (s *Service) demoPeerDeviceLocked() LastDeviceNotice {
	pct := 84
	ch := true
	return LastDeviceNotice{
		HasDevice:        true,
		Host:             "192.168.1.142",
		Port:             18789,
		Addr:             "192.168.1.142:18789",
		LastSeenUnix:     time.Now().Unix(),
		DeviceName:       "Pixel 8 Pro",
		Model:            "Pixel 8 Pro (Android 15)",
		BatteryPct:       &pct,
		Charging:         &ch,
		BatteryUnix:      time.Now().Unix(),
		CustomName:       s.customName,
		DisplayName:      displayPhoneName(s.customName, "Pixel 8 Pro"),
		FilesPermission:  "granted",
		PhotosPermission: "granted",
	}
}

func (s *Service) demoPairStatusLocked() PairStatus {
	return PairStatus{
		Listening:      true,
		QRHost:         "192.168.1.142",
		QRPort:         18789,
		QRCandidates:   []string{"192.168.1.142"},
		LastAcceptUnix: time.Now().Unix(),
	}
}

func (s *Service) demoListFiles(path string) (FileListResult, error) {
	s.demo.mu.Lock()
	defer s.demo.mu.Unlock()

	norm := strings.Trim(strings.TrimPrefix(path, "/"), "/")
	entries, ok := s.demo.files[norm]
	if !ok {
		// Return empty list if folder doesn't exist
		return FileListResult{Path: norm, Entries: []FileEntryView{}}, nil
	}
	res := make([]FileEntryView, len(entries))
	copy(res, entries)
	return FileListResult{Path: norm, Entries: res}, nil
}

func (s *Service) demoMkdir(path string) (string, error) {
	s.demo.mu.Lock()
	defer s.demo.mu.Unlock()

	norm := strings.Trim(strings.TrimPrefix(path, "/"), "/")
	dir := filepath.Dir(norm)
	base := filepath.Base(norm)
	if dir == "." {
		dir = ""
	}

	parentList := s.demo.files[dir]
	parentList = append(parentList, FileEntryView{
		Name:    base,
		Path:    norm,
		IsDir:   true,
		ModTime: time.Now().Unix(),
	})
	s.demo.files[dir] = parentList
	s.demo.files[norm] = []FileEntryView{}
	return "Folder created.", nil
}

func (s *Service) demoDeleteFile(path string) (string, error) {
	s.demo.mu.Lock()
	defer s.demo.mu.Unlock()

	norm := strings.Trim(strings.TrimPrefix(path, "/"), "/")
	dir := filepath.Dir(norm)
	if dir == "." {
		dir = ""
	}

	if items, ok := s.demo.files[dir]; ok {
		filtered := make([]FileEntryView, 0, len(items))
		for _, item := range items {
			if item.Path != norm {
				filtered = append(filtered, item)
			}
		}
		s.demo.files[dir] = filtered
	}
	delete(s.demo.files, norm)
	return "Deleted.", nil
}

func (s *Service) demoStatLocalFiles(localPaths []string) ([]LocalFileInfo, error) {
	res := make([]LocalFileInfo, len(localPaths))
	for i, p := range localPaths {
		res[i] = LocalFileInfo{
			Path:  p,
			Name:  filepath.Base(p),
			Size:  1024,
			Mtime: time.Now().Unix(),
		}
	}
	return res, nil
}

func (s *Service) demoListPhotos(cursor string, limit int) (PhotoListResult, error) {
	_ = cursor
	_ = limit
	s.demo.mu.Lock()
	defer s.demo.mu.Unlock()

	copied := make([]PhotoEntryView, len(s.demo.photos))
	copy(copied, s.demo.photos)
	return PhotoListResult{Entries: copied}, nil
}

func (s *Service) demoRequestPhotoThumb(photoID string, thumbSize int) (PhotoThumbResult, error) {
	b64 := demoPhotoThumbB64(photoID)
	if thumbSize > 256 {
		b64 = demoPhotoHiResB64(photoID)
	}
	return PhotoThumbResult{
		PhotoID: photoID,
		Mime:    "image/jpeg",
		DataB64: b64,
	}, nil
}

func (s *Service) demoDeletePhotos(photoIDs []string) (PhotoDeleteResult, error) {
	s.demo.mu.Lock()
	defer s.demo.mu.Unlock()

	toDelete := make(map[string]bool)
	for _, id := range photoIDs {
		toDelete[id] = true
	}

	filtered := make([]PhotoEntryView, 0, len(s.demo.photos))
	results := make([]PhotoDeleteItemView, len(photoIDs))
	for i, id := range photoIDs {
		results[i] = PhotoDeleteItemView{PhotoID: id, Ok: true}
	}
	for _, p := range s.demo.photos {
		if !toDelete[p.PhotoID] {
			filtered = append(filtered, p)
		}
	}
	s.demo.photos = filtered
	return PhotoDeleteResult{Results: results}, nil
}

func (s *Service) demoListContacts(cursor string, limit int, query string) (ContactListResult, error) {
	_ = cursor
	_ = limit
	s.demo.mu.Lock()
	defer s.demo.mu.Unlock()

	q := strings.ToLower(strings.TrimSpace(query))
	var matches []core.ContactEntry
	for _, c := range s.demo.contacts {
		if q == "" || strings.Contains(strings.ToLower(c.DisplayName), q) {
			matches = append(matches, c)
		}
	}
	return ContactListResult{
		Contacts:   matches,
		TotalCount: len(matches),
	}, nil
}

func (s *Service) demoGetContactAvatar(contactID string) (ContactAvatarResult, error) {
	s.demo.mu.Lock()
	defer s.demo.mu.Unlock()

	for _, c := range s.demo.contacts {
		if c.ContactID == contactID {
			return ContactAvatarResult{
				ContactID:    contactID,
				AvatarB64:    c.AvatarB64,
				PhotoVersion: "1",
			}, nil
		}
	}
	return ContactAvatarResult{
		ContactID:    contactID,
		AvatarB64:    demoContactAvatarB64(contactID),
		PhotoVersion: "1",
	}, nil
}

func (s *Service) demoDeleteContact(contactID string) (ContactDeleteResult, error) {
	s.demo.mu.Lock()
	defer s.demo.mu.Unlock()

	filtered := make([]core.ContactEntry, 0, len(s.demo.contacts))
	for _, c := range s.demo.contacts {
		if c.ContactID != contactID {
			filtered = append(filtered, c)
		}
	}
	s.demo.contacts = filtered
	return ContactDeleteResult{OK: true, ContactID: contactID}, nil
}

func (s *Service) demoLookupContact(address string) (map[string]string, error) {
	s.demo.mu.Lock()
	defer s.demo.mu.Unlock()

	normAddr := strings.ReplaceAll(strings.ReplaceAll(address, " ", ""), "-", "")
	for _, c := range s.demo.contacts {
		for _, p := range c.Phones {
			pNorm := strings.ReplaceAll(strings.ReplaceAll(p.Number, " ", ""), "-", "")
			if strings.Contains(pNorm, normAddr) || strings.Contains(normAddr, pNorm) {
				return map[string]string{
					"name":       c.DisplayName,
					"contact_id": c.ContactID,
					"avatar_b64": c.AvatarB64,
				}, nil
			}
		}
	}
	return nil, nil
}

func (s *Service) demoFindThreadForAddress(address string) int64 {
	s.demo.mu.Lock()
	defer s.demo.mu.Unlock()

	normAddr := strings.ReplaceAll(strings.ReplaceAll(address, " ", ""), "-", "")
	for _, t := range s.demo.threads {
		tNorm := strings.ReplaceAll(strings.ReplaceAll(t.Address, " ", ""), "-", "")
		if strings.Contains(tNorm, normAddr) || strings.Contains(normAddr, tNorm) {
			return t.ThreadID
		}
	}
	return 0
}

func (s *Service) demoListSMSThreads() (SMSThreadsResult, error) {
	s.demo.mu.Lock()
	defer s.demo.mu.Unlock()

	threadsCopy := make([]core.SMSThread, len(s.demo.threads))
	copy(threadsCopy, s.demo.threads)
	return SMSThreadsResult{Threads: threadsCopy}, nil
}

func (s *Service) demoListSMSMessages(threadID int64) (SMSMessagesResult, error) {
	s.demo.mu.Lock()
	defer s.demo.mu.Unlock()

	msgs := s.demo.messages[threadID]
	msgsCopy := make([]core.SMSMessage, len(msgs))
	copy(msgsCopy, msgs)
	return SMSMessagesResult{
		ThreadID: threadID,
		Messages: msgsCopy,
	}, nil
}

func (s *Service) demoSendSMS(recipient, body string) (SMSSendResult, error) {
	s.demo.mu.Lock()
	defer s.demo.mu.Unlock()

	s.demo.nextMessageID++
	msgID := s.demo.nextMessageID

	// Find or create thread
	var threadID int64
	for _, t := range s.demo.threads {
		if t.Address == recipient {
			threadID = t.ThreadID
			break
		}
	}

	nowMs := time.Now().UnixMilli()
	if threadID == 0 {
		threadID = int64(len(s.demo.threads) + 1)
		s.demo.threads = append([]core.SMSThread{{
			ThreadID:     threadID,
			Address:      recipient,
			ContactName:  recipient,
			Snippet:      body,
			Date:         nowMs,
			MessageCount: 1,
			Read:         true,
		}}, s.demo.threads...)
	} else {
		// Update existing thread snippet
		for i := range s.demo.threads {
			if s.demo.threads[i].ThreadID == threadID {
				s.demo.threads[i].Snippet = body
				s.demo.threads[i].Date = nowMs
				s.demo.threads[i].MessageCount++
				break
			}
		}
	}

	newMsg := core.SMSMessage{
		ID:       msgID,
		ThreadID: threadID,
		Address:  recipient,
		Body:     body,
		Date:     nowMs,
		Type:     2, // sent
		Read:     true,
	}

	s.demo.messages[threadID] = append(s.demo.messages[threadID], newMsg)
	s.emitMessagesChanged()

	return SMSSendResult{
		Ok:        true,
		ClientID:  fmt.Sprintf("demo-%d", msgID),
		MessageID: msgID,
		ThreadID:  threadID,
	}, nil
}

func (s *Service) demoMarkThreadRead(threadID int64) error {
	s.demo.mu.Lock()
	defer s.demo.mu.Unlock()

	for i := range s.demo.threads {
		if s.demo.threads[i].ThreadID == threadID {
			s.demo.threads[i].Read = true
			s.demo.threads[i].UnreadCount = 0
			break
		}
	}
	s.emitMessagesChanged()
	return nil
}

func (s *Service) demoSendPlaybackCmd(cmd string) (string, error) {
	cur := s.playback.Get()
	newState := cur.State
	switch cmd {
	case "play":
		newState = core.PlaybackPlaying
	case "pause":
		newState = core.PlaybackPaused
	case "play_pause", "toggle":
		if cur.State == core.PlaybackPlaying {
			newState = core.PlaybackPaused
		} else {
			newState = core.PlaybackPlaying
		}
	case "next", "previous":
		cur.PositionMs = 0
	}
	s.playback.ApplyRemote(core.PlaybackStatePayload{
		Nonce:       fmt.Sprintf("cmd-%d", time.Now().UnixNano()),
		Title:       cur.Title,
		Artist:      cur.Artist,
		Album:       cur.Album,
		PackageName: cur.PackageName,
		App:         cur.App,
		State:       newState,
		PositionMs:  cur.PositionMs,
		DurationMs:  cur.DurationMs,
		UpdatedMs:   time.Now().UnixMilli(),
		Origin:      core.OriginMac,
		ArtworkB64:  cur.ArtworkB64,
		ArtworkMime: cur.ArtworkMime,
	})
	s.emitPlaybackChanged()
	return "Command sent.", nil
}

func (s *Service) demoSetDND(enabled bool) (string, error) {
	s.dnd.ApplyRemote(core.DNDStatePayload{
		Enabled:       enabled,
		HasPermission: true,
		UpdatedMs:     time.Now().UnixMilli(),
	})
	s.emitDNDChanged()
	if enabled {
		return "Do Not Disturb on.", nil
	}
	return "Do Not Disturb off.", nil
}

func (s *Service) demoGetNotifications() NotifList {
	s.demo.mu.Lock()
	defer s.demo.mu.Unlock()

	views := make([]NotifView, len(s.demo.notifs))
	for i, n := range s.demo.notifs {
		views[i] = NotifView(n)
	}
	return NotifList{
		Items:  views,
		Unseen: s.demo.unseenNotifs,
	}
}

func (s *Service) demoMarkNotificationsSeen() {
	s.demo.mu.Lock()
	s.demo.unseenNotifs = 0
	s.demo.mu.Unlock()
	s.emitNotifsChanged()
}

func (s *Service) demoDismissNotification(id string) (string, error) {
	s.demo.mu.Lock()
	filtered := make([]NotifItem, 0, len(s.demo.notifs))
	for _, n := range s.demo.notifs {
		if n.ID != id {
			filtered = append(filtered, n)
		}
	}
	s.demo.notifs = filtered
	if s.demo.unseenNotifs > len(filtered) {
		s.demo.unseenNotifs = len(filtered)
	}
	s.demo.mu.Unlock()
	s.emitNotifsChanged()
	return "Dismissed.", nil
}

func (s *Service) demoClearNotifications() (string, error) {
	s.demo.mu.Lock()
	s.demo.notifs = nil
	s.demo.unseenNotifs = 0
	s.demo.mu.Unlock()
	s.emitNotifsChanged()
	return "Cleared.", nil
}

func (s *Service) demoPushClipboard(text string) (string, error) {
	s.clips.SetLocal(text, time.Now().Unix())
	s.emitClipChanged()
	return "Copied to phone.", nil
}

func (s *Service) demoKnownApps() []KnownNotifApp {
	apps := defaultDemoKnownApps()
	sort.Slice(apps, func(i, j int) bool {
		return apps[i].Count > apps[j].Count
	})
	return apps
}
