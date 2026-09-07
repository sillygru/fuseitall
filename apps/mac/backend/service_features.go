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

// NotifView is the frontend row for one mirrored notification.
type NotifView struct {
	ID         string `json:"id"`
	App        string `json:"app"`
	Title      string `json:"title"`
	Text       string `json:"text"`
	PostedUnix int64  `json:"posted_unix"`
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

// PushClipboard records a Mac-side copy and sends it immediately to the
// phone (manual Send only). Fail-closed when offline or oversize; the
// local preview still updates so the pane reflects what was typed. Send
// failures requeue for the next heartbeat while also surfacing the error
// so the UI can show it.
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
	// Direct send for reliable manual push. Failures keep pending for heartbeat retry.
	if !s.IsPaired() {
		return "", errors.New("phone is offline — reconnect first")
	}
	// Consume pending we just set and send.
	takenText, takenAt, ok := s.clips.TakePending()
	if !ok {
		takenText, takenAt = text, now
	}
	if err := s.sendFeatureToPhone(core.TypeClipPush, &core.ClipPushPayload{
		Text:      takenText,
		ChangedAt: takenAt,
		Origin:    core.OriginMac,
	}); err != nil {
		s.requeueClip()
		return "", fmt.Errorf("send clipboard: %w", err)
	}
	s.appendLine("clipboard sent to phone")
	return "Clipboard sent to phone.", nil
}

// ingestNotifBody learns from an accepted phone feature post (HTTP 200
// through the full version + token gate). Rejected posts never reach here.
func (s *Service) ingestNotifBody(body []byte) {
	if p, ok := ParseNotifPost(body); ok {
		if !s.settings.Get().NotificationsEnabled {
			return
		}
		if s.notifs.Post(p) {
			s.appendLine("notification received")
			notifyUser(p.Title, p.Text)
		}
		return
	}
	if p, ok := ParseNotifDismiss(body); ok {
		s.notifs.ApplyRemoteDismiss(p.ID)
		s.appendLine("notification withdrawn by phone")
	}
}

// ingestClipBody learns from an accepted phone clip-push. Adopted remote
// text is written to the system pasteboard so cmd+v pastes immediately.
func (s *Service) ingestClipBody(body []byte) {
	p, ok := ParseClipPush(body)
	if !ok {
		return
	}
	if s.clips.ApplyRemote(p) {
		s.appendLine("clipboard synced from phone")
		if !writePasteboard(p.Text) {
			s.appendLine("clipboard write failed")
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
// pending clipboard (retry from a failed manual push) to the phone.
// Best-effort: failures keep their queue slots for the next heartbeat.
func (s *Service) flushPendingToPhone() {
	if !s.IsPaired() {
		return
	}
	if st, ok := s.settings.TakePending(); ok {
		enabled := st.NotificationsEnabled
		if serr := s.sendFeatureToPhone(core.TypeSettingsSync, &core.SettingsSyncPayload{
			NotificationsEnabled: &enabled,
			UpdatedUnix:          st.UpdatedUnix,
			UpdatedBy:            st.UpdatedBy,
		}); serr != nil {
			s.requeueSettings()
		}
	}
	if text, changedAt, ok := s.clips.TakePending(); ok {
		if serr := s.sendFeatureToPhone(core.TypeClipPush, &core.ClipPushPayload{
			Text:      text,
			ChangedAt: changedAt,
			Origin:    core.OriginMac,
		}); serr != nil {
			s.requeueClip()
		} else {
			s.appendLine("clipboard sent to phone")
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

// sendFeatureToPhone POSTs one feature envelope to the captured phone peer.
func (s *Service) sendFeatureToPhone(msgType string, payload any) error {
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
