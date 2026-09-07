// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Package core feature payloads: notifications, clipboard sync, and app
// settings sync. Every type rides the Envelope contract (packages/proto);
// decoders ignore unknown fields, encoders emit only known ones. Text
// bodies (notification text, clipboard text) are never logged verbatim:
// handlers log lengths and IDs only.
package core

import (
	"strings"
)

const (
	// TypeNotifPost pushes one phone notification to the Mac.
	// Capability: CapabilityNotifications.
	TypeNotifPost = "notif-post"
	// TypeNotifDismiss retracts a notification (either direction).
	// Capability: CapabilityNotifications.
	TypeNotifDismiss = "notif-dismiss"

	// TypeClipPush carries clipboard text one way (manual push only).
	// Capability: CapabilityClipboard.
	TypeClipPush = "clip-push"

	// TypeSettingsSync carries the app settings blob.
	// Capability: CapabilitySettingsSync.
	TypeSettingsSync = "settings-sync"

	// TypeUnpair is the goodbye message: the sender just unpaired and the
	// receiver should drop the peer (ephemeral + remembered) immediately.
	// Best-effort: the sender wipes locally regardless of the reply.
	// Capability: CapabilityPing (presence-level, no new capability).
	TypeUnpair = "unpair"

	// CapabilityNotifications advertises notification mirroring.
	CapabilityNotifications = "notifications"
	// CapabilityClipboard advertises clipboard sync.
	CapabilityClipboard = "clipboard"
	// CapabilitySettingsSync advertises app settings sync.
	CapabilitySettingsSync = "settings-sync"
)

// Origins for clip-push. "macos" is accepted on decode and normalized to
// "mac" so both spellings interoperate.
const (
	OriginMac     = "mac"
	OriginMacOS   = "macos"
	OriginAndroid = "android"
)

// Clipboard auto-sync directions. "both" is the default (bidirectional).
const (
	ClipboardBoth          = "both"
	ClipboardMacToAndroid  = "mac_to_android"
	ClipboardAndroidToMac  = "android_to_mac"
	ClipboardDisabled      = "disabled"
)

// Caps limits (mirror packages/proto/*.json).
const (
	MaxNotifIDLen    = 128
	MaxNotifAppLen   = 64
	MaxNotifTitleLen = 128
	MaxNotifTextLen  = 512
	// MaxClipLen caps clipboard text at 256KB (bytes, plain text only).
	MaxClipLen = 256 * 1024
)

// NotifPostPayload is the body of a TypeNotifPost envelope. Nonce is
// echoed in the pong ack so senders can match replies fail-closed.
type NotifPostPayload struct {
	Nonce    string `json:"nonce"`
	ID       string `json:"id"`
	App      string `json:"app,omitempty"`
	Title    string `json:"title,omitempty"`
	Text     string `json:"text,omitempty"`
	PostedAt int64  `json:"posted_at,omitempty"`
}

// NotifDismissPayload is the body of a TypeNotifDismiss envelope.
type NotifDismissPayload struct {
	Nonce string `json:"nonce"`
	ID    string `json:"id"`
}

// ClipPushPayload is the body of a TypeClipPush envelope. ChangedAt orders
// pushes; Origin breaks echo loops ("mac"/"macos" == Mac side).
type ClipPushPayload struct {
	Nonce     string `json:"nonce"`
	Text      string `json:"text,omitempty"`
	ChangedAt int64  `json:"changed_at,omitempty"`
	Origin    string `json:"origin,omitempty"`
}

// UnpairPayload is the body of a TypeUnpair envelope: a pure nonce
// goodbye with no fields. The receiver drops the peer on accept.
type UnpairPayload struct {
	Nonce string `json:"nonce"`
}

// SettingsSyncPayload is the body of a TypeSettingsSync envelope.
// Last-writer-wins: greater UpdatedUnix wins; ties go to the Mac side.
// NotificationsEnabled nil means true (absent = enabled, pre-toggle peers).
// ClipboardMode absent/unknown means "both" (bidirectional).
type SettingsSyncPayload struct {
	Nonce                string `json:"nonce"`
	NotificationsEnabled *bool  `json:"notifications_enabled,omitempty"`
	ClipboardMode        string `json:"clipboard_mode,omitempty"`
	UpdatedUnix          int64  `json:"updated_unix"`
	UpdatedBy            string `json:"updated_by,omitempty"`
}

// NormalizeOrigin maps "macos" to "mac", trims, lowercases. Pure.
func NormalizeOrigin(s string) string {
	n := strings.ToLower(strings.TrimSpace(s))
	if n == OriginMacOS {
		return OriginMac
	}
	return n
}

// NormalizeUpdatedBy maps "macos" to "mac" for tie-breaking. Pure.
func NormalizeUpdatedBy(s string) string {
	return NormalizeOrigin(s)
}

// IsMacSide reports whether an origin/updated_by value means the Mac. Pure.
func IsMacSide(s string) bool {
	return NormalizeOrigin(s) == OriginMac
}

// NormalizeClipboardMode trims, lowercases, and canonicalizes a clipboard
// direction. Unknown/empty input maps to ClipboardBoth (bidirectional) so
// older peers (missing field) interoperate as both-way. Pure.
func NormalizeClipboardMode(s string) string {
	n := strings.ToLower(strings.TrimSpace(s))
	switch n {
	case ClipboardBoth, ClipboardMacToAndroid, ClipboardAndroidToMac, ClipboardDisabled:
		return n
	case "":
		return ClipboardBoth
	default:
		return ClipboardBoth
	}
}

// IsValidClipboardMode reports whether s is one of the four canonical modes. Pure.
func IsValidClipboardMode(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case ClipboardBoth, ClipboardMacToAndroid, ClipboardAndroidToMac, ClipboardDisabled:
		return true
	default:
		return false
	}
}

// ClipboardModeAllowsSend reports whether the given mode allows an auto send
// from the caller side. Manual pushes (explicit user Send) bypass this; callers
// should check separately. Pure.
func ClipboardModeAllowsSend(mode, origin string) bool {
	m := NormalizeClipboardMode(mode)
	o := NormalizeOrigin(origin)
	switch m {
	case ClipboardDisabled:
		return false
	case ClipboardMacToAndroid:
		return o == OriginMac
	case ClipboardAndroidToMac:
		return o == OriginAndroid
	default:
		return true
	}
}

// ClipboardModeAllowsReceive reports whether the given mode allows receiving
// (and applying + pasteboard write) for an incoming origin. Pure.
func ClipboardModeAllowsReceive(mode, origin string) bool {
	m := NormalizeClipboardMode(mode)
	o := NormalizeOrigin(origin)
	switch m {
	case ClipboardDisabled:
		return false
	case ClipboardMacToAndroid:
		return o == OriginMac
	case ClipboardAndroidToMac:
		return o == OriginAndroid
	default:
		return true
	}
}

// SanitizeNotifID trims an ID and reports usability (1..128 chars). Pure.
func SanitizeNotifID(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || len([]rune(trimmed)) > MaxNotifIDLen {
		return "", false
	}
	return trimmed, true
}

// TruncateNotifField trims s and caps it at max runes (fail-soft: truncate,
// never reject). Pure.
func TruncateNotifField(s string, max int) string {
	trimmed := strings.TrimSpace(s)
	if max <= 0 {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) > max {
		return strings.TrimSpace(string(runes[:max]))
	}
	return trimmed
}

// SanitizeClipText validates clipboard text: caps at MaxClipLen bytes.
// Empty text is valid (means cleared). Over-long input returns ok=false so
// callers reject with BAD_REQUEST instead of truncating user data. Pure.
func SanitizeClipText(s string) (string, bool) {
	if len(s) > MaxClipLen {
		return "", false
	}
	return s, true
}

// SanitizeSettings validates a settings blob: negative timestamps fail
// closed, clipboard_mode is canonicalized (unknown→both, fail-soft). Pure.
func SanitizeSettings(p SettingsSyncPayload) (SettingsSyncPayload, bool) {
	if p.UpdatedUnix < 0 {
		return SettingsSyncPayload{}, false
	}
	p.UpdatedBy = NormalizeUpdatedBy(p.UpdatedBy)
	p.ClipboardMode = NormalizeClipboardMode(p.ClipboardMode)
	return p, true
}

// RemoteSettingsWins reports whether an incoming settings blob should
// replace the local one: greater timestamp wins; ties go to the Mac side
// (a Mac remote always beats an Android local, and vice versa loses).
// Pure.
func RemoteSettingsWins(local, remote SettingsSyncPayload) bool {
	if remote.UpdatedUnix != local.UpdatedUnix {
		return remote.UpdatedUnix > local.UpdatedUnix
	}
	remoteMac := IsMacSide(remote.UpdatedBy)
	localMac := IsMacSide(local.UpdatedBy)
	if remoteMac != localMac {
		return remoteMac
	}
	return false
}

// RemoteClipWins reports whether an incoming clip-push should replace the
// local text: strictly newer ChangedAt wins; ties keep local (idempotent
// redelivery must not flap). Zero ChangedAt never wins over a stamped
// value. Pure.
func RemoteClipWins(localChangedAt, remoteChangedAt int64) bool {
	if remoteChangedAt <= 0 {
		return false
	}
	return remoteChangedAt > localChangedAt
}


