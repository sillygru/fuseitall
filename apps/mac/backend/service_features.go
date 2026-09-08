// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"context"
	"errors"
	"fmt"
	"time"

	"fuseitall/core"
)

// featureCaps is stamped on every outbound feature envelope alongside ping
// so peers gate per message type, never per connection.
var featureCaps = []string{
	core.CapabilityPing,
	core.CapabilityNotifications,
	core.CapabilityClipboard,
	core.CapabilitySettingsSync,
	core.CapabilityFiles,
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
// notifications on its next heartbeat.
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
func (s *Service) PushClipboard(text string) (string, error) {
	if _, ok := core.SanitizeClipText(text); !ok {
		return "", fmt.Errorf("clipboard text must be under %d bytes", core.MaxClipLen)
	}
	now := time.Now().Unix()
	if cur := s.clips.Get(); cur.HasText && now <= cur.ChangedUnix {
		now = cur.ChangedUnix + 1
	}
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
	if err := s.sendFeatureToPhone(core.TypeClipPush, &pending); err != nil {
		s.requeueClip()
		return "", fmt.Errorf("send clipboard: %w", err)
	}
	s.appendLine("clipboard sent to phone")
	return "Clipboard sent to phone.", nil
}

// PushClipboardCurrent sends whatever is currently on the system pasteboard
// (image preferred, else text). Images are normalized to PNG on send
// (TIFF/HEIC/HEIF → PNG via sips/stdlib, over-cap PNG → JPEG q85) so the
// phone receives a universally renderable format. This is the single "Send
// clipboard" action.
func (s *Service) PushClipboardCurrent() (string, error) {
	if !s.IsPaired() {
		return "", errors.New("phone is offline — reconnect first")
	}
	// Prefer image if present: JXA image first, then Finder file-url image.
	img := readPasteboardImage()
	if img.B64 == "" {
		img = readPasteboardFileImage()
	}
	if img.B64 != "" {
		if _, ok := core.SanitizeClipImage(img.B64, img.Mime); !ok {
			return "", fmt.Errorf("invalid clipboard image (max %d bytes)", core.MaxClipImageRaw)
		}
		// Very normalized format: TIFF/HEIC/HEIF → PNG (sips), else pass through.
		if nb64, nmime, nfn, ok := NormalizeClipImageForSend(img.B64, img.Mime, img.Filename); ok {
			img.B64, img.Mime, img.Filename = nb64, nmime, nfn
		} else if normalizeNeedsTranscode(img.Mime) {
			return "", fmt.Errorf("image normalize failed (try copying as PNG)")
		}
		now := time.Now().Unix()
		if cur := s.clips.Get(); cur.HasText && now <= cur.ChangedUnix {
			now = cur.ChangedUnix + 1
		}
		if _, ok := s.clips.SetLocalImageWithFilename(img.B64, img.Mime, img.Filename, now); !ok {
			return "", fmt.Errorf("invalid clipboard image")
		}
		s.emitClipboardChanged(s.clips.Get())
			pending, ok := s.clips.TakePending()
		if !ok {
			m, _ := core.SanitizeClipMime(img.Mime)
			pending = core.ClipPushPayload{Kind: core.ClipKindImage, Mime: m, ImageB64: img.B64, Filename: core.SanitizeClipFilename(img.Filename), ChangedAt: now, Origin: core.OriginMac}
		}
		if pending.Origin == "" {
			pending.Origin = core.OriginMac
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
	if cur := s.clips.Get(); cur.HasText && now <= cur.ChangedUnix {
		now = cur.ChangedUnix + 1
	}
	if _, ok := s.clips.SetLocal(text, now); !ok {
		return "", fmt.Errorf("clipboard text must be under %d bytes", core.MaxClipLen)
	}
	s.emitClipboardChanged(s.clips.Get())
	pending, ok := s.clips.TakePending()
	if !ok {
		pending = core.ClipPushPayload{Kind: core.ClipKindText, Text: text, ChangedAt: now, Origin: core.OriginMac}
	}
	if pending.Origin == "" {
		pending.Origin = core.OriginMac
	}
	if err := s.sendFeatureToPhone(core.TypeClipPush, &pending); err != nil {
		s.requeueClip()
		return "", fmt.Errorf("send clipboard: %w", err)
	}
	s.appendLine("clipboard sent to phone")
	return "Clipboard sent to phone.", nil
}

// PushClipboardImage records a Mac-side image and sends it immediately.
// b64 must be base64-encoded image, mime whitelisted. Fail-closed on oversize.
func (s *Service) PushClipboardImage(b64, mime string) (string, error) {
	if _, ok := core.SanitizeClipImage(b64, mime); !ok {
		return "", fmt.Errorf("invalid clipboard image (max %d bytes, png/jpeg/webp/gif/tiff/heic)", core.MaxClipImageRaw)
	}
	now := time.Now().Unix()
	if cur := s.clips.Get(); cur.HasText && now <= cur.ChangedUnix {
		now = cur.ChangedUnix + 1
	}
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
	if err := s.sendFeatureToPhone(core.TypeClipPush, &pending); err != nil {
		s.requeueClip()
		return "", fmt.Errorf("send clipboard image: %w", err)
	}
	s.appendLine("clipboard image sent to phone")
	return "Clipboard image sent to phone.", nil
}

// ingestNotifBody learns from an accepted phone feature post (HTTP 200
// through the full version + token gate). Rejected posts never reach here.
func (s *Service) ingestNotifBody(body []byte) {
	if p, ok := ParseNotifPost(body); ok {
		if !s.settings.Get().NotificationsEnabled {
			// Loud drop: disabled master switch, never silent per ADR 0004.
			// ID only (low cardinality), never title/text (PII).
			s.appendLine("notification dropped: disabled id=" + p.ID)
			return
		}
		if s.notifs.Post(p) {
			s.appendLine("notification received")
			if p.IconB64 == "" && p.PackageName != "" {
				p.IconB64 = s.notifs.IconForPackage(p.PackageName)
			}
			notifyUserWithIcon(p)
		}
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
// pushes.
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
		return
	}
	if s.clips.ApplyRemote(p) {
		s.emitClipboardChanged(s.clips.Get())
		kind := core.NormalizeClipKind(p.Kind)
		if kind == core.ClipKindImage {
			s.appendLine("clipboard image synced from phone")
			if !writePasteboardImageWithFilename(p.ImageB64, p.Mime, p.Filename) {
				s.appendLine("clipboard image write failed")
			}
			if s.clipWatcher != nil {
				s.clipWatcher.NoteRemoteImage(p.ImageB64, p.Mime)
			}
		} else {
			s.appendLine("clipboard synced from phone")
			if !writePasteboard(p.Text) {
				s.appendLine("clipboard write failed")
			}
			if s.clipWatcher != nil {
				s.clipWatcher.NoteRemoteCopy(p.Text)
			}
		}
	}
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

// flushPendingToPhone sends queued settings and dismissal syncs plus any
// pending clipboard (retry from a failed manual/auto push) to the phone.
// Best-effort: failures keep their queue slots for the next heartbeat.
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
		payload := core.SettingsSyncPayload{
			NotificationsEnabled: &enabled,
			ClipboardMode:        mode,
			UpdatedUnix:          st.UpdatedUnix,
			UpdatedBy:            st.UpdatedBy,
		}
		if serr := s.sendFeatureToPhone(core.TypeSettingsSync, &payload); serr != nil {
			s.requeueSettings()
		}
	}
	if pending, ok := s.clips.TakePending(); ok {
		if serr := s.sendFeatureToPhone(core.TypeClipPush, &pending); serr != nil {
			s.requeueClip()
		} else {
			if pending.Kind == core.ClipKindImage {
				s.appendLine("clipboard image sent to phone")
			} else {
				s.appendLine("clipboard sent to phone")
			}
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

// sendFeatureToPhone sends one feature envelope to the captured phone peer.
// Prefers the persistent WebSocket connection (0ms latency, zero polling delay);
// falls back to HTTP POST when WebSocket is not yet connected.
func (s *Service) sendFeatureToPhone(msgType string, payload any) error {
	env, err := core.NewEnvelope(msgType, core.CurrentSender(senderPlatform), featureCaps, payload)
	if err != nil {
		return fmt.Errorf("create %s envelope: %w", msgType, err)
	}
	if s.WriteActiveWS(env) {
		return nil
	}

	s.mu.Lock()
	host, port, seen := s.peerHost, s.peerPort, s.lastSeen
	s.mu.Unlock()
	if host == "" || port <= 0 || time.Since(seen) >= peerTTL {
		return errors.New("no phone peer captured yet")
	}
	client, err := s.phoneClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = core.SendFeature(ctx, client, PeerBaseURL(host, port), s.token,
		core.CurrentSender(senderPlatform), featureCaps, msgType, payload)
	if err != nil {
		var upd *core.UpdateRequiredError
		if errors.As(err, &upd) {
			s.setUpdateDetail(upd.Message, upd.RequiredBuild > core.CurrentBuild, upd.RequiredVersion, upd.CurrentVersion, upd.RequiredBuild)
			return fmt.Errorf("send %s: %w", msgType, err)
		}
		if isCertMismatch(err) {
			s.logRotationOnce("cert-mismatch",
				"phone cert mismatch ("+err.Error()+") — waiting for phone ping to re-pin")
		} else if isPeerLost(err) {
			s.logRotationOnce("peer-lost", "phone peer lost ("+err.Error()+")")
		} else {
			s.appendLine("feature send failed: " + err.Error())
		}
		return fmt.Errorf("send %s: %w", msgType, err)
	}
	return nil
}
