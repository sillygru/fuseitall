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
	"time"

	"fuseitall/core"
)

// featureCaps is stamped on every outbound feature envelope alongside ping
// so peers gate per message type, never per connection. files-large-chunk
// advertises that our download ingest accepts 4 MiB chunks as well as
// legacy 1 MiB ones (dual-stride validation in core).
var featureCaps = []string{
	core.CapabilityPing,
	core.CapabilityNotifications,
	core.CapabilityClipboard,
	core.CapabilitySettingsSync,
	core.CapabilityFiles,
	core.CapabilityFilesLargeChunk,
	core.CapabilityPhotos,
	core.CapabilityPlayback,
	core.CapabilityContacts,
	core.CapabilityMessages,
}

// GetSettings returns the current app settings for the Settings pane.
func (s *Service) GetSettings() AppSettings {
	return s.settings.Get()
}

// SetNotificationsEnabled flips the notification master switch, persists,
// and syncs when paired.
func (s *Service) SetNotificationsEnabled(enabled bool) (string, error) {
	updated, err := s.settings.SetNotificationsEnabled(enabled)
	if err != nil {
		return "", err
	}
	if serr := s.settings.persistSnapshot(); serr != nil {
		s.appendLine("settings save failed: " + serr.Error())
	}
	if !updated.NotificationsEnabled {
		s.notifs.Clear()
	}
	s.appendLine("notification setting saved")
	s.flushPendingToPhone()
	if updated.NotificationsEnabled {
		return "Notifications on.", nil
	}
	return "Notifications off.", nil
}

// SetClipboardMode flips the clipboard auto direction, persists, and syncs
// when paired. Modes: both, android_to_mac, mac_to_android, disabled.
// A mode change re-arms the watcher once (one-shot fetch, never a schedule)
// so newly-allowed content syncs without waiting for the next copy.
func (s *Service) SetClipboardMode(mode string) (string, error) {
	updated, err := s.settings.SetClipboardMode(mode)
	if err != nil {
		return "", err
	}
	if serr := s.settings.persistSnapshot(); serr != nil {
		s.appendLine("settings save failed: " + serr.Error())
	}
	s.appendLine("clipboard mode set to " + updated.ClipboardMode)
	s.flushPendingToPhone()
	if s.clipWatcher != nil {
		go s.clipWatcher.TriggerNow()
	}
	switch updated.ClipboardMode {
	case core.ClipboardDisabled:
		return "Clipboard auto sync off.", nil
	case core.ClipboardMacToAndroid:
		return "Clipboard: Mac → phone only.", nil
	case core.ClipboardAndroidToMac:
		return "Clipboard: phone → Mac only.", nil
	default:
		return "Clipboard: both ways.", nil
	}
}

// SetClipboardAllowSensitive flips the sensitive auto-sync opt-in, persists,
// and syncs when paired. Auto watchers skip concealed content unless true;
// manual Send always bypasses.
func (s *Service) SetClipboardAllowSensitive(allow bool) (string, error) {
	updated, err := s.settings.SetClipboardAllowSensitive(allow)
	if err != nil {
		return "", err
	}
	if serr := s.settings.persistSnapshot(); serr != nil {
		s.appendLine("settings save failed: " + serr.Error())
	}
	if updated.ClipboardAllowSensitive {
		s.appendLine("clipboard sensitive auto sync on")
		return "Sensitive clipboard auto sync on.", nil
	}
	s.appendLine("clipboard sensitive auto sync off")
	return "Sensitive clipboard auto sync off (manual Send still works).", nil
}

// SetNotifMode flips the per-app filter mode, persists, and syncs when
// paired. Modes: all_except_muted, only_allowed.
func (s *Service) SetNotifMode(mode string) (string, error) {
	updated, err := s.settings.SetNotifMode(mode)
	if err != nil {
		return "", err
	}
	if serr := s.settings.persistSnapshot(); serr != nil {
		s.appendLine("settings save failed: " + serr.Error())
	}
	s.appendLine("notification filter set to " + updated.NotifMode)
	s.flushPendingToPhone()
	if updated.NotifMode == core.NotifOnlyAllowed {
		return "Notifications: allowed apps only.", nil
	}
	return "Notifications: all except muted.", nil
}

// SetPlaybackMode flips the playback sync direction, persists, and syncs
// when paired. Modes: both, android_to_mac, mac_to_android, disabled.
func (s *Service) SetPlaybackMode(mode string) (string, error) {
	updated, err := s.settings.SetPlaybackMode(mode)
	if err != nil {
		return "", err
	}
	if serr := s.settings.persistSnapshot(); serr != nil {
		s.appendLine("settings save failed: " + serr.Error())
	}
	s.appendLine("playback mode set to " + updated.PlaybackMode)
	if updated.PlaybackMode == core.PlaybackDisabled {
		s.playback.Clear()
		s.emitPlaybackChanged()
		mirrorPlaybackToSystem(updated.PlaybackOutput, s.playback.Get(), nil)
	}
	s.flushPendingToPhone()
	switch updated.PlaybackMode {
	case core.PlaybackDisabled:
		return "Playback sync off.", nil
	case core.PlaybackMacToAndroid:
		return "Playback: Mac to phone only.", nil
	case core.PlaybackAndroidToMac:
		return "Playback: phone to Mac only.", nil
	default:
		return "Playback: both ways.", nil
	}
}

// SetPlaybackOutput flips the Mac presentation output, persists, and syncs
// when paired. Outputs: inapp, system.
func (s *Service) SetPlaybackOutput(output string) (string, error) {
	updated, err := s.settings.SetPlaybackOutput(output)
	if err != nil {
		return "", err
	}
	if serr := s.settings.persistSnapshot(); serr != nil {
		s.appendLine("settings save failed: " + serr.Error())
	}
	s.appendLine("playback output set to " + updated.PlaybackOutput)
	mirrorPlaybackToSystem(updated.PlaybackOutput, s.playback.Get(), nil)
	s.flushPendingToPhone()
	if updated.PlaybackOutput == core.PlaybackOutputSystem {
		return "Playback shows in app and system.", nil
	}
	return "Playback shows in app only.", nil
}

// GetPlayback returns the current now-playing snapshot for the player pane.
func (s *Service) GetPlayback() PlaybackView {
	return s.playback.Get()
}

// SendPlaybackCmd sends one transport command to the phone (user-initiated).
// Gated by playback_mode: both + mac_to_android allow commands; otherwise
// fails loud so the UI disables with a note instead of silently dropping.
func (s *Service) SendPlaybackCmd(cmd string) (string, error) {
	norm := core.PlaybackCmdPause
	switch cmd {
	case core.PlaybackCmdPlay, core.PlaybackCmdPause, core.PlaybackCmdToggle, core.PlaybackCmdNext, core.PlaybackCmdPrev:
		norm = cmd
	default:
		return "", errors.New("unknown playback command")
	}
	mode := s.settings.Get().PlaybackMode
	if mode == "" || mode == core.PlaybackAndroidToMac {
		// Auto-promote to two-way control when the user initiates a playback command,
		// persisting and syncing with the phone so control is immediately seamless.
		if updated, err := s.settings.SetPlaybackMode(core.PlaybackBoth); err == nil {
			if serr := s.settings.persistSnapshot(); serr != nil {
				s.appendLine("settings save failed: " + serr.Error())
			}
			s.flushPendingToPhone()
			s.appendLine("playback mode upgraded to " + updated.PlaybackMode)
			mode = updated.PlaybackMode
		}
	}
	if !core.PlaybackModeAllowsCommand(mode) {
		return "", errors.New("playback commands are off for this direction")
	}
	if !s.IsPaired() {
		return "", errors.New("phone is offline — reconnect first")
	}
	nonce, err := core.RandomPlaybackNonce()
	if err != nil {
		return "", fmt.Errorf("generate playback nonce: %w", err)
	}
	payload := core.PlaybackCmdPayload{Nonce: nonce, Origin: core.OriginMac, Cmd: norm}
	if err := s.sendFeatureToPhone(core.TypePlaybackCmd, &payload); err != nil {
		return "", fmt.Errorf("send playback command: %w", err)
	}
	s.appendLine("playback command sent")
	return "Command sent.", nil
}

// SetAppMuted toggles one package on the denylist, persists, and syncs.
// muted=true mutes, muted=false unmutes.
func (s *Service) SetAppMuted(pkg string, muted bool) (string, error) {
	updated, err := s.settings.SetAppMuted(pkg, muted)
	if err != nil {
		return "", err
	}
	if serr := s.settings.persistSnapshot(); serr != nil {
		s.appendLine("settings save failed: " + serr.Error())
	}
	s.appendLine("notification app filter saved")
	s.flushPendingToPhone()
	_ = updated
	if muted {
		return "App muted.", nil
	}
	return "App unmuted.", nil
}

// SetAppAllowed toggles one package on the allowlist (only_allowed mode),
// persists, and syncs.
func (s *Service) SetAppAllowed(pkg string, allowed bool) (string, error) {
	updated, err := s.settings.SetAppAllowed(pkg, allowed)
	if err != nil {
		return "", err
	}
	if serr := s.settings.persistSnapshot(); serr != nil {
		s.appendLine("settings save failed: " + serr.Error())
	}
	s.appendLine("notification app filter saved")
	s.flushPendingToPhone()
	_ = updated
	if allowed {
		return "App allowed.", nil
	}
	return "App removed.", nil
}

// KnownNotifApp is one app row for the per-app filter UI: the package,
// display label, known icon (may be ""), mirror count, and muted/allowed
// state under the current filter mode.
type KnownNotifApp struct {
	PackageName string `json:"package_name"`
	App         string `json:"app"`
	IconB64     string `json:"app_icon_b64"`
	Count       int    `json:"count"`
	Muted       bool   `json:"muted"`
	Allowed     bool   `json:"allowed"`
}

// GetKnownNotifApps returns the union of mirrored packages, cached icons,
// and filter lists (so muted apps with zero live rows stay toggleable),
// sorted by count desc then label. Pure view over store state.
func (s *Service) GetKnownNotifApps() []KnownNotifApp {
	st := s.settings.Get()
	items, _ := s.notifs.List()
	byPkg := make(map[string]*KnownNotifApp)
	order := make([]string, 0)
	ensure := func(pkg, label string) *KnownNotifApp {
		if a, ok := byPkg[pkg]; ok {
			if a.App == "" && label != "" {
				a.App = label
			}
			return a
		}
		a := &KnownNotifApp{PackageName: pkg, App: label}
		byPkg[pkg] = a
		order = append(order, pkg)
		return a
	}
	for _, it := range items {
		pkg := it.PackageName
		if pkg == "" {
			continue
		}
		a := ensure(pkg, it.App)
		a.Count++
		if a.IconB64 == "" && it.IconB64 != "" {
			a.IconB64 = it.IconB64
		}
	}
	for _, pkg := range st.MutedPackages {
		ensure(pkg, "")
	}
	for _, pkg := range st.AllowedPackages {
		ensure(pkg, "")
	}
	muted := make(map[string]bool, len(st.MutedPackages))
	for _, p := range st.MutedPackages {
		muted[p] = true
	}
	allowed := make(map[string]bool, len(st.AllowedPackages))
	for _, p := range st.AllowedPackages {
		allowed[p] = true
	}
	out := make([]KnownNotifApp, 0, len(byPkg))
	for _, pkg := range order {
		a := byPkg[pkg]
		a.Muted = muted[pkg]
		a.Allowed = allowed[pkg]
		if a.IconB64 == "" {
			a.IconB64 = s.notifs.IconForPackage(pkg)
		}
		if a.App == "" {
			a.App = pkg
		}
		out = append(out, *a)
	}
	// Insertion sort: lists are tiny (<=100+live); count desc, label asc.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			swap := false
			if out[j].Count != out[j-1].Count {
				swap = out[j].Count > out[j-1].Count
			} else {
				swap = out[j].App < out[j-1].App
			}
			if !swap {
				break
			}
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if out == nil {
		out = []KnownNotifApp{}
	}
	return out
}

// NotifView is the frontend row for one mirrored notification. PackageName
// and IconB64 are additive 0.3.0+ (may be "" from older phones). IconB64 is
// base64 PNG ~96px, cached per package, never logged.
type NotifView struct {
	ID          string `json:"id"`
	App         string `json:"app"`
	PackageName string `json:"package_name"`
	IconB64     string `json:"app_icon_b64"`
	GroupKey    string `json:"group_key"`
	Title       string `json:"title"`
	Text        string `json:"text"`
	PostedUnix  int64  `json:"posted_unix"`
}

// NotifList is the typed notification mirror for the frontend: rows plus
// the unseen badge count. A struct (not a tuple) so the Wails binding
// carries both halves in one JSON object.
type NotifList struct {
	Items  []NotifView
	Unseen int
}

// GetNotifications returns mirrored notifications (newest first) plus the
// unseen badge count. Opening the pane should call MarkNotificationsSeen.
func (s *Service) GetNotifications() NotifList {
	items, unseen := s.notifs.List()
	views := make([]NotifView, 0, len(items))
	for _, it := range items {
		views = append(views, NotifView(it))
	}
	if views == nil {
		views = []NotifView{}
	}
	return NotifList{Items: views, Unseen: unseen}
}

// MarkNotificationsSeen resets the badge count.
func (s *Service) MarkNotificationsSeen() {
	s.notifs.MarkSeen()
}

// DismissNotification drops one notification locally and syncs the dismissal
// to the phone when paired (queued otherwise).
func (s *Service) DismissNotification(id string) (string, error) {
	if _, ok := core.SanitizeNotifID(id); !ok {
		return "", errors.New("unknown notification")
	}
	s.notifs.Dismiss(id)
	s.appendLine("notification dismissed")
	s.flushPendingToPhone()
	return "Notification dismissed.", nil
}

// ClearNotifications empties the mirror locally. The phone reposts live
// notifications over the WebSocket as they arrive.
func (s *Service) ClearNotifications() (string, error) {
	s.notifs.Clear()
	return "Notifications cleared.", nil
}

// GetClipboard returns the synced clipboard state for the Clipboard pane.
func (s *Service) GetClipboard() ClipNotice {
	return s.clips.Get()
}

// PushClipboard records a Mac-side text copy and sends it immediately to the
// phone (manual Send only). Kept for typed draft fallback; prefer PushClipboardCurrent.
// Manual bypasses the mode send-gate and the sensitive auto-gate by design.
func (s *Service) PushClipboard(text string) (string, error) {
	if _, ok := core.SanitizeClipText(text); !ok {
		return "", fmt.Errorf("clipboard text must be under %d bytes", core.MaxClipLen)
	}
	now := time.Now().Unix()
	if _, ok := s.clips.SetLocal(text, now); !ok {
		return "", fmt.Errorf("clipboard text must be under %d bytes", core.MaxClipLen)
	}
	s.appendLine("clipboard updated")
	if len(text) == 0 {
		return "Clipboard cleared.", nil
	}
	if !s.IsPaired() {
		return "", errors.New("phone is offline — reconnect first")
	}
	pending, ok := s.clips.TakePending()
	if !ok {
		pending = core.ClipPushPayload{Kind: core.ClipKindText, Text: text, ChangedAt: now, Origin: core.OriginMac}
	}
	if pending.Origin == "" {
		pending.Origin = core.OriginMac
	}
	if err := ensureClipNonce(&pending); err != nil {
		s.requeueClip()
		return "", err
	}
	if err := s.sendFeatureToPhone(core.TypeClipPush, &pending); err != nil {
		s.requeueClip()
		return "", fmt.Errorf("send clipboard: %w", err)
	}
	s.appendLine("clipboard sent to phone")
	return "Clipboard sent to phone.", nil
}

// PushClipboardCurrent sends whatever is currently on the system pasteboard
// (image preferred, else text). This is the single "Send clipboard" action:
// manual bypasses the mode send-gate and the sensitive auto-gate by design
// (explicit intent), still respects pairing + version + size gates. Inline
// images ride one clip-push; large images (>5 MiB, ≤25 MiB) ride the chunk
// lane (manifest + 1 MiB chunks, sha256-verified).
func (s *Service) PushClipboardCurrent() (string, error) {
	if !s.IsPaired() {
		return "", errors.New("phone is offline — reconnect first")
	}
	sensitive := isSensitivePasteboard()
	// Prefer image if present: JXA image first, then Finder file-url image.
	img := readPasteboardImage()
	if img.B64 == "" {
		img = readPasteboardFileImage()
	}
	if img.B64 != "" {
		// Large-image fast path: over inline cap but within chunk cap.
		if raw, ok := decodeClipB64ForChunk(img.B64, img.Mime); ok && len(raw) > core.MaxClipImageRaw {
			return s.sendLargeClipboardImage(raw, img.Mime, img.Filename, sensitive)
		}
		if _, ok := core.SanitizeClipImage(img.B64, img.Mime); !ok {
			// Maybe large but decodable: try chunk validation before failing.
			if raw, ok2 := decodeClipB64ForChunk(img.B64, img.Mime); ok2 {
				return s.sendLargeClipboardImage(raw, img.Mime, img.Filename, sensitive)
			}
			return "", fmt.Errorf("invalid clipboard image (max %d bytes inline, %d bytes chunked)", core.MaxClipImageRaw, core.MaxClipTotalRaw)
		}
		// Very normalized format: TIFF/HEIC/HEIF → PNG (sips), else pass through.
		if nb64, nmime, nfn, ok := NormalizeClipImageForSend(img.B64, img.Mime, img.Filename); ok {
			img.B64, img.Mime, img.Filename = nb64, nmime, nfn
		} else if normalizeNeedsTranscode(img.Mime) {
			return "", fmt.Errorf("image normalize failed (try copying as PNG)")
		}
		now := time.Now().Unix()
		if _, ok := s.clips.SetLocalImageWithMeta(img.B64, img.Mime, img.Filename, now, sensitive); !ok {
			return "", fmt.Errorf("invalid clipboard image")
		}
		s.emitClipboardChanged(s.clips.Get())
		pending, ok := s.clips.TakePending()
		if !ok {
			m, _ := core.SanitizeClipMime(img.Mime)
			pending = core.ClipPushPayload{Kind: core.ClipKindImage, Mime: m, ImageB64: img.B64, Filename: core.SanitizeClipFilename(img.Filename), ChangedAt: now, Origin: core.OriginMac, Sensitive: sensitive}
		}
		if pending.Origin == "" {
			pending.Origin = core.OriginMac
		}
		if err := ensureClipNonce(&pending); err != nil {
			s.requeueClip()
			return "", err
		}
		if err := s.sendFeatureToPhone(core.TypeClipPush, &pending); err != nil {
			s.requeueClip()
			return "", fmt.Errorf("send clipboard image: %w", err)
		}
		s.appendLine("clipboard image sent to phone")
		return "Clipboard image sent to phone.", nil
	}
	text := readPasteboard()
	if text == "" {
		return "", errors.New("clipboard is empty")
	}
	if _, ok := core.SanitizeClipText(text); !ok {
		return "", fmt.Errorf("clipboard text must be under %d bytes", core.MaxClipLen)
	}
	now := time.Now().Unix()
	if _, ok := s.clips.SetLocalWithMeta(text, now, sensitive); !ok {
		return "", fmt.Errorf("clipboard text must be under %d bytes", core.MaxClipLen)
	}
	s.emitClipboardChanged(s.clips.Get())
	pending, ok := s.clips.TakePending()
	if !ok {
		pending = core.ClipPushPayload{Kind: core.ClipKindText, Text: text, ChangedAt: now, Origin: core.OriginMac, Sensitive: sensitive}
	}
	if pending.Origin == "" {
		pending.Origin = core.OriginMac
	}
	if err := ensureClipNonce(&pending); err != nil {
		s.requeueClip()
		return "", err
	}
	if err := s.sendFeatureToPhone(core.TypeClipPush, &pending); err != nil {
		s.requeueClip()
		return "", fmt.Errorf("send clipboard: %w", err)
	}
	s.appendLine("clipboard sent to phone")
	return "Clipboard sent to phone.", nil
}

// PushClipboardImage records a Mac-side image and sends it immediately.
// Inline images ride one clip-push; large images ride the chunk lane.
func (s *Service) PushClipboardImage(b64, mime string) (string, error) {
	if raw, ok := decodeClipB64ForChunk(b64, mime); ok && len(raw) > core.MaxClipImageRaw {
		return s.sendLargeClipboardImage(raw, mime, "", isSensitivePasteboard())
	}
	if _, ok := core.SanitizeClipImage(b64, mime); !ok {
		return "", fmt.Errorf("invalid clipboard image (max %d bytes, png/jpeg/webp/gif/tiff/heic)", core.MaxClipImageRaw)
	}
	now := time.Now().Unix()
	if _, ok := s.clips.SetLocalImage(b64, mime, now); !ok {
		return "", fmt.Errorf("invalid clipboard image")
	}
	s.appendLine("clipboard image updated")
	if !s.IsPaired() {
		return "", errors.New("phone is offline — reconnect first")
	}
	pending, ok := s.clips.TakePending()
	if !ok {
		m, _ := core.SanitizeClipMime(mime)
		pending = core.ClipPushPayload{Kind: core.ClipKindImage, Mime: m, ImageB64: b64, ChangedAt: now, Origin: core.OriginMac}
	}
	if pending.Origin == "" {
		pending.Origin = core.OriginMac
	}
	if err := ensureClipNonce(&pending); err != nil {
		s.requeueClip()
		return "", err
	}
	if err := s.sendFeatureToPhone(core.TypeClipPush, &pending); err != nil {
		s.requeueClip()
		return "", fmt.Errorf("send clipboard image: %w", err)
	}
	s.appendLine("clipboard image sent to phone")
	return "Clipboard image sent to phone.", nil
}

// ingestNotifBody learns from an accepted phone feature post (HTTP 200
// through the full version + token gate). Rejected posts never reach here.
// Gate order: progress → per-app filter → master switch → store. Every drop
// is loud (ID only, never title/text) per docs/connection.md (loud errors).
func (s *Service) ingestNotifBody(body []byte) {
	// Inventory pages share the /notif lane: resolve the pending page
	// waiter without touching the mirror or banner paths below.
	var sniff struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &sniff); err == nil && sniff.Type == core.TypeNotifAppsResp {
		s.ingestNotifAppsRespBody(body)
		return
	}
	if p, ok := ParseNotifPost(body); ok {
		if p.HasProgress {
			s.appendLine("notification dropped: progress id=" + p.ID)
			return
		}
		st := s.settings.Get()
		if !core.ShouldMirrorNotif(st.NotifMode, st.MutedPackages, st.AllowedPackages, p.PackageName, p.HasProgress) {
			s.appendLine("notification dropped: filtered id=" + p.ID)
			return
		}
		if !st.NotificationsEnabled {
			// Loud drop: disabled master switch, never silent per docs/connection.md.
			// ID only (low cardinality), never title/text (PII).
			s.appendLine("notification dropped: disabled id=" + p.ID)
			return
		}
		accepted, shouldBanner := s.notifs.Post(p)
		if !accepted {
			s.appendLine("notification dropped: stale id=" + p.ID)
			return
		}
		if !shouldBanner {
			return
		}
		s.appendLine("notification received")
		if p.IconB64 == "" && p.PackageName != "" {
			p.IconB64 = s.notifs.IconForPackage(p.PackageName)
		}
		notifyUserWithIcon(p)
		return
	}
	if p, ok := ParseNotifDismiss(body); ok {
		s.notifs.ApplyRemoteDismiss(p.ID)
		s.appendLine("notification withdrawn by phone")
	}
}

// ingestClipBody learns from an accepted phone clip-push. Adopted remote
// payload is written to the system pasteboard so cmd+v pastes immediately.
// Respects the local clipboard_mode: disabled or mac-only drops phone-origin
// pushes. Suppress is armed BEFORE the pasteboard write so slow shells never
// re-ingest; a failed write disarms. Stale/conflict drops log feed-only with
// lengths (never bodies) per loud-errors law.
func (s *Service) ingestClipBody(body []byte) {
	p, ok := ParseClipPush(body)
	if !ok {
		return
	}
	mode := s.settings.Get().ClipboardMode
	if mode == "" {
		mode = core.ClipboardBoth
	}
	if !core.ClipboardModeAllowsReceive(mode, p.Origin) {
		s.appendLine("clipboard dropped: mode " + mode)
		return
	}
	if !s.clips.ApplyRemote(p) {
		s.appendLine(clipConflictLine(p))
		return
	}
	s.applyRemoteToPasteboard(p)
}

// ingestSettingsBody adopts an accepted phone settings blob when it wins.
func (s *Service) ingestSettingsBody(body []byte) {
	p, ok := ParseSettingsSync(body)
	if !ok {
		return
	}
	if s.settings.ApplyRemote(p) {
		if err := s.settings.persistSnapshot(); err != nil {
			s.appendLine("settings save failed: " + err.Error())
		}
		s.appendLine("settings synced from phone")
	}
}

// ingestPlaybackBody learns from an accepted phone playback-state post.
// Respects the local playback_mode: disabled or mac-only drops
// phone-origin states. Every drop is loud (package only, never title).
func (s *Service) ingestPlaybackBody(body []byte) {
	p, ok := ParsePlaybackState(body)
	if !ok {
		return
	}
	mode := s.settings.Get().PlaybackMode
	if mode == "" {
		mode = core.PlaybackBoth
	}
	if !core.PlaybackModeAllowsReceive(mode, p.Origin) {
		return
	}
	if !core.PlaybackModeAllowsState(mode) {
		s.appendLine("playback dropped: direction id=" + core.PlaybackTrackID(p))
		return
	}
	if s.playback.ApplyRemote(p) {
		s.emitPlaybackChanged()
		st := s.settings.Get()
		mirrorPlaybackToSystem(st.PlaybackOutput, s.playback.Get(), nil)
		s.appendLine("playback updated")
	}
}

// flushPendingToPhone sends queued settings and dismissal syncs plus any
// pending clipboard (retry from a failed manual/auto push) to the phone.
// Best-effort: failures keep their queue slots for the next WS connect.
func (s *Service) flushPendingToPhone() {
	if !s.IsPaired() {
		return
	}
	if st, ok := s.settings.TakePending(); ok {
		enabled := st.NotificationsEnabled
		mode := st.ClipboardMode
		if mode == "" {
			mode = core.ClipboardBoth
		}
		notifMode := st.NotifMode
		if notifMode == "" {
			notifMode = core.NotifAllExceptMuted
		}
		playbackMode := st.PlaybackMode
		if playbackMode == "" {
			playbackMode = core.PlaybackBoth
		}
		playbackOutput := st.PlaybackOutput
		if playbackOutput == "" {
			playbackOutput = core.PlaybackOutputInApp
		}
		allowSensitive := st.ClipboardAllowSensitive
		payload := core.SettingsSyncPayload{
			NotificationsEnabled:    &enabled,
			ClipboardMode:           mode,
			NotifMode:               notifMode,
			MutedPackages:           st.MutedPackages,
			AllowedPackages:         st.AllowedPackages,
			PlaybackMode:            playbackMode,
			PlaybackOutput:          playbackOutput,
			ClipboardAllowSensitive: &allowSensitive,
			UpdatedUnix:             st.UpdatedUnix,
			UpdatedBy:               st.UpdatedBy,
		}
		if serr := s.sendFeatureToPhone(core.TypeSettingsSync, &payload); serr != nil {
			s.requeueSettings()
		}
	}
	if pending, ok := s.clips.TakePending(); ok {
		if err := ensureClipNonce(&pending); err == nil {
			if serr := s.sendFeatureToPhone(core.TypeClipPush, &pending); serr != nil {
				s.requeueClip()
			} else {
				if pending.Kind == core.ClipKindImage {
					s.appendLine("clipboard image sent to phone")
				} else {
					s.appendLine("clipboard sent to phone")
				}
			}
		} else {
			s.requeueClip()
		}
	}
	for _, id := range s.notifs.TakePendingDismissals() {
		if serr := s.sendFeatureToPhone(core.TypeNotifDismiss, &core.NotifDismissPayload{ID: id}); serr != nil {
			s.notifs.Dismiss(id)
		}
	}
}

// requeueSettings re-arms the settings pending flag without restamping.
func (s *Service) requeueSettings() {
	s.settings.mu.Lock()
	s.settings.pending = true
	s.settings.mu.Unlock()
}

// requeueClip re-arms the clipboard pending flag without touching content.
func (s *Service) requeueClip() {
	s.clips.mu.Lock()
	if s.clips.has {
		s.clips.pending = true
	}
	s.clips.mu.Unlock()
}

// sendFeatureToPhone sends one feature envelope over the persistent WebSocket
// (0ms latency, zero polling). Realtime-only: when no WebSocket is connected
// it fails closed so callers keep their pending queue for the next WS connect
// (flushPendingToPhone) instead of silently dropping on an HTTP fallback the
// phone no longer drains. Version gating rides checkPeerCapability before send
// plus inbound update-required envelopes; capability build gates stay.
func (s *Service) sendFeatureToPhone(msgType string, payload any) error {
	env, err := core.NewEnvelope(msgType, core.CurrentSender(senderPlatform), featureCaps, payload)
	if err != nil {
		return fmt.Errorf("create %s envelope: %w", msgType, err)
	}
	if s.WriteActiveWS(env) {
		return nil
	}
	return errors.New("phone is offline — reconnect first")
}
