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
	"encoding/base64"
	"path/filepath"
	"strings"
	"unicode"
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
	MaxNotifIDLen       = 256
	MaxNotifAppLen      = 64
	MaxNotifTitleLen    = 128
	MaxNotifTextLen     = 512
	MaxNotifPackageLen  = 128
	MaxNotifIconB64Len  = 32768
	MaxNotifGroupLen    = 128
	// MaxClipLen caps clipboard text at 256KB (bytes, plain text only).
	MaxClipLen = 256 * 1024
	// MaxClipImageRaw caps clipboard image raw bytes at 5 MiB.
	MaxClipImageRaw = 5 << 20
	// MaxClipImageB64Len caps base64-encoded image at ~7.1 MiB (4*ceil(5MiB/3)).
	MaxClipImageB64Len = 7100000
	// ClipAutoImageRaw caps auto-synced images at 5 MiB raw (manual also 5 MiB;
	// bumped from 1 MiB per user decision for 2.6 MiB PNG auto + manual).
	ClipAutoImageRaw = 5 << 20
)

// Clipboard kinds.
const (
	ClipKindText  = "text"
	ClipKindImage = "image"
)

// NotifPostPayload is the body of a TypeNotifPost envelope. Nonce is
// echoed in the pong ack so senders can match replies fail-closed.
// PackageName and IconB64 are additive 0.3.0+ fields: older peers ignore
// them, receivers fail-soft by dropping invalid icons.
type NotifPostPayload struct {
	Nonce       string `json:"nonce"`
	ID          string `json:"id"`
	App         string `json:"app,omitempty"`
	PackageName string `json:"package_name,omitempty"`
	IconB64     string `json:"app_icon_b64,omitempty"`
	GroupKey    string `json:"group_key,omitempty"`
	Title       string `json:"title,omitempty"`
	Text        string `json:"text,omitempty"`
	PostedAt    int64  `json:"posted_at,omitempty"`
}

// NotifDismissPayload is the body of a TypeNotifDismiss envelope.
type NotifDismissPayload struct {
	Nonce string `json:"nonce"`
	ID    string `json:"id"`
}

// ClipPushPayload is the body of a TypeClipPush envelope. ChangedAt orders
// pushes; Origin breaks echo loops ("mac"/"macos" == Mac side).
// Kind discriminates text vs image (empty defaults to text for compat).
// Image payloads carry Mime + ImageB64 (base64, max 5 MiB raw). Text
// payloads carry Text (max 256KB). Exactly one of Text/ImageB64 should be set.
// Filename is optional basename (max 255) for UTI/extension preservation; empty
// means synthesize from mime on the receiver.
type ClipPushPayload struct {
	Nonce     string `json:"nonce"`
	Kind      string `json:"kind,omitempty"`
	Text      string `json:"text,omitempty"`
	Mime      string `json:"mime,omitempty"`
	ImageB64  string `json:"image_b64,omitempty"`
	ChangedAt int64  `json:"changed_at,omitempty"`
	Origin    string `json:"origin,omitempty"`
	Filename  string `json:"filename,omitempty"`
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

// SanitizePackageName trims and caps a package name (1..128 chars). Pure.
// Empty is allowed (optional field); callers treat "" as absent.
func SanitizePackageName(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) > MaxNotifPackageLen {
		return strings.TrimSpace(string(runes[:MaxNotifPackageLen]))
	}
	return trimmed
}

// SanitizeNotifIconB64 validates an icon base64 string: trims, checks length
// cap (32KB) and basic base64 shape. Invalid/oversize returns "" (fail-soft:
// caller drops icon, keeps notification). Pure.
func SanitizeNotifIconB64(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) > MaxNotifIconB64Len {
		return ""
	}
	// Basic shape check: must be base64 chars + padding. Full decode is
	// expensive per notification; length + charset is sufficient to reject
	// garbage without allocating.
	for _, r := range trimmed {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '/' || r == '=' {
			continue
		}
		return ""
	}
	return trimmed
}

// SanitizeGroupKey trims and caps a group key. Empty means absent. Pure.
func SanitizeGroupKey(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) > MaxNotifGroupLen {
		return strings.TrimSpace(string(runes[:MaxNotifGroupLen]))
	}
	return trimmed
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

// NormalizeClipKind trims, lowercases, and canonicalizes. Empty/invalid → text. Pure.
func NormalizeClipKind(s string) string {
	n := strings.ToLower(strings.TrimSpace(s))
	switch n {
	case ClipKindImage:
		return ClipKindImage
	default:
		return ClipKindText
	}
}

// AllowedClipImageMimes is the whitelist for image sync.
var AllowedClipImageMimes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/jpg":  true,
	"image/webp": true,
	"image/gif":  true,
	"image/tiff": true,
	"image/heic": true,
	"image/heif": true,
}

// SanitizeClipMime lowercases and validates MIME. Pure.
func SanitizeClipMime(s string) (string, bool) {
	n := strings.ToLower(strings.TrimSpace(s))
	if n == "image/jpg" {
		n = "image/jpeg"
	}
	if AllowedClipImageMimes[n] {
		return n, true
	}
	return "", false
}

// SanitizeClipFilename sanitizes a basename for the clipboard filename field.
// Pure: trims, takes Base, rejects dirs/traversal, allows alphanum/_/.-, caps 255.
func SanitizeClipFilename(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	base := filepath.Base(trimmed)
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	// Reject traversal attempts.
	if !filepath.IsLocal(base) {
		return ""
	}
	if strings.Contains(base, "/") || strings.Contains(base, "\\") {
		return ""
	}
	if base == "." || base == ".." {
		return ""
	}
	// Must have at least one alphanum and only safe chars.
	for _, r := range base {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_' {
			continue
		}
		return ""
	}
	if len(base) > 255 {
		ext := filepath.Ext(base)
		if len(ext) > 20 {
			ext = ""
		}
		// Keep extension, truncate stem.
		stem := strings.TrimSuffix(base, ext)
		maxStem := 255 - len(ext)
		if maxStem < 1 {
			return ""
		}
		runes := []rune(stem)
		if len(runes) > maxStem {
			stem = string(runes[:maxStem])
		}
		base = stem + ext
	}
	if len(base) == 0 || len(base) > 255 {
		return ""
	}
	return base
}

// SniffImageMime returns the MIME inferred from magic bytes, or "" if unknown.
// Pure: checks PNG/JPEG/GIF/WEBP/HEIC/TIFF signatures; does not validate size cap.
func SniffImageMime(raw []byte) string {
	if len(raw) >= 8 && raw[0] == 0x89 && raw[1] == 0x50 && raw[2] == 0x4E && raw[3] == 0x47 {
		return "image/png"
	}
	if len(raw) >= 3 && raw[0] == 0xFF && raw[1] == 0xD8 && raw[2] == 0xFF {
		return "image/jpeg"
	}
	if len(raw) >= 6 && raw[0] == 'G' && raw[1] == 'I' && raw[2] == 'F' {
		return "image/gif"
	}
	if len(raw) >= 12 && raw[0] == 'R' && raw[1] == 'I' && raw[2] == 'F' && raw[3] == 'F' && raw[8] == 'W' && raw[9] == 'E' && raw[10] == 'B' && raw[11] == 'P' {
		return "image/webp"
	}
	if len(raw) >= 12 && raw[4] == 'f' && raw[5] == 't' && raw[6] == 'y' && raw[7] == 'p' {
		return "image/heic"
	}
	if len(raw) >= 4 {
		if raw[0] == 0x49 && raw[1] == 0x49 && raw[2] == 0x2A && raw[3] == 0x00 {
			return "image/tiff"
		}
		if raw[0] == 0x4D && raw[1] == 0x4D && raw[2] == 0x00 && raw[3] == 0x2A {
			return "image/tiff"
		}
	}
	return ""
}

// sanitizeMagic validates magic bytes for a normalized MIME. Pure.
func sanitizeMagic(mime string, raw []byte) bool {
	switch mime {
	case "image/png":
		return len(raw) >= 8 && raw[0] == 0x89 && raw[1] == 0x50 && raw[2] == 0x4E && raw[3] == 0x47
	case "image/jpeg":
		return len(raw) >= 3 && raw[0] == 0xFF && raw[1] == 0xD8 && raw[2] == 0xFF
	case "image/gif":
		return len(raw) >= 6 && raw[0] == 'G' && raw[1] == 'I' && raw[2] == 'F'
	case "image/webp":
		return len(raw) >= 12 && raw[0] == 'R' && raw[1] == 'I' && raw[2] == 'F' && raw[3] == 'F' && raw[8] == 'W' && raw[9] == 'E' && raw[10] == 'B' && raw[11] == 'P'
	case "image/heic", "image/heif":
		return len(raw) >= 12 && raw[4] == 'f' && raw[5] == 't' && raw[6] == 'y' && raw[7] == 'p'
	case "image/tiff":
		if len(raw) < 4 {
			return false
		}
		return (raw[0] == 0x49 && raw[1] == 0x49 && raw[2] == 0x2A && raw[3] == 0x00) || (raw[0] == 0x4D && raw[1] == 0x4D && raw[2] == 0x00 && raw[3] == 0x2A)
	default:
		return false
	}
}

// ClipImageExt returns the canonical file extension for a MIME (including dot).
func ClipImageExt(mime string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/tiff":
		return ".tiff"
	case "image/heic", "image/heif":
		return ".heic"
	default:
		return ".png"
	}
}

// SynthesizeClipFilename returns a safe filename, preferring sanitized input and
// falling back to clip-<ext>.ext derived from mime. Pure.
// If input has no extension or mismatched extension, appends correct ext.
func SynthesizeClipFilename(input, mime string, ts int64) string {
	ext := ClipImageExt(mime)
	if s := SanitizeClipFilename(input); s != "" {
		if strings.EqualFold(filepath.Ext(s), ext) {
			return s
		}
		if filepath.Ext(s) == "" {
			return s + ext
		}
		// Wrong extension: keep stem, fix ext.
		return strings.TrimSuffix(s, filepath.Ext(s)) + ext
	}
	if ts <= 0 {
		ts = 0
	}
	return "clip-" + strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(ext, ".", ""), "/", "")) + ext
}

// SanitizeClipImage validates base64 image + MIME. Returns decoded bytes on success.
// Pure: checks b64 length cap, base64 validity, raw size cap, MIME whitelist, magic bytes.
func SanitizeClipImage(b64, mime string) ([]byte, bool) {
	if len(b64) == 0 || len(b64) > MaxClipImageB64Len {
		return nil, false
	}
	m, ok := SanitizeClipMime(mime)
	if !ok {
		return nil, false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		raw, err = base64.StdEncoding.WithPadding(base64.StdPadding).DecodeString(strings.TrimSpace(b64))
		if err != nil {
			return nil, false
		}
	}
	if len(raw) == 0 || len(raw) > MaxClipImageRaw {
		return nil, false
	}
	if !sanitizeMagic(m, raw) {
		return nil, false
	}
	return raw, true
}

// SanitizeClipPush validates a full clip-push payload (text or image). Pure.
func SanitizeClipPush(p ClipPushPayload) bool {
	kind := NormalizeClipKind(p.Kind)
	if kind == ClipKindImage {
		if _, ok := SanitizeClipImage(p.ImageB64, p.Mime); !ok {
			return false
		}
		if p.Filename != "" && SanitizeClipFilename(p.Filename) == "" {
			return false
		}
		return true
	}
	_, ok := SanitizeClipText(p.Text)
	return ok
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


