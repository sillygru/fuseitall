// SPDX-License-Identifier: AGPL-3.0-only

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
	// TypeNotifAppsReq asks the phone for one page of its launchable app
	// inventory (Mac -> phone). Capability: CapabilityNotifications.
	// Additive: old phones answer 400 wrong_type; new Macs fall back to
	// the mirrored known-apps view.
	TypeNotifAppsReq = "notif-apps-req"
	// TypeNotifAppsResp answers one NotifAppsReq page (phone -> Mac).
	// Capability: CapabilityNotifications.
	TypeNotifAppsResp = "notif-apps-resp"

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

// Notification per-app filter modes. AllExceptMuted mirrors everything
// except muted_packages (default, preserves pre-0.9.0 behavior).
// OnlyAllowed mirrors solely allowed_packages.
const (
	NotifAllExceptMuted = "all_except_muted"
	NotifOnlyAllowed    = "only_allowed"
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
	// MaxNotifFilterApps caps muted/allowed package lists (proto maxItems 100).
	MaxNotifFilterApps = 100
	// MaxNotifAppsPerResp caps one notif-apps-resp page (proto maxItems 50):
	// 50 icons x 32KB b64 worst case ~= 1.6MB, well under MaxBodyBytes.
	MaxNotifAppsPerResp = 50
	// MaxNotifAppsTotal caps a full multi-page inventory traversal (matches
	// the launchable-apps 500 cap on the phone).
	MaxNotifAppsTotal = 500
	// DefaultNotifAppsLimit is the page size when the request omits limit.
	DefaultNotifAppsLimit = 50
	// MaxNotifAppsReqIDLen caps req_id at 64 chars (mirrors file/photo lists).
	MaxNotifAppsReqIDLen = 64
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
// Ongoing and HasProgress are additive 0.9.0+: reliable senders drop
// HasProgress before sending; receivers drop/throttle them fail-soft and
// never banner same-ID reposts. Older peers omit them (zero value false).
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
	Ongoing     bool   `json:"ongoing,omitempty"`
	HasProgress bool   `json:"has_progress,omitempty"`
}

// NotifDismissPayload is the body of a TypeNotifDismiss envelope.
type NotifDismissPayload struct {
	Nonce string `json:"nonce"`
	ID    string `json:"id"`
}

// NotifAppEntry is one row of the phone app inventory: the launchable
// package plus its display label and optional icon (PNG 96px base64,
// max 32KB). IconB64 empty means absent; receivers drop invalid icons
// fail-soft and keep the row. Labels are truncated by senders (app 64);
// receivers truncate fail-soft the same way.
type NotifAppEntry struct {
	PackageName string `json:"package_name"`
	App         string `json:"app,omitempty"`
	IconB64     string `json:"app_icon_b64,omitempty"`
}

// NotifAppsReqPayload is the body of a TypeNotifAppsReq envelope
// (Mac -> phone, POST /notif). Cursor empty means the first page; each
// response echoes ReqID and returns NextCursor (empty = last page).
// Limit 0 means DefaultNotifAppsLimit. WithIcons nil (absent) means true:
// the phone includes icons unless the Mac opts out for a labels-only pass.
type NotifAppsReqPayload struct {
	Nonce     string `json:"nonce"`
	ReqID     string `json:"req_id"`
	Cursor    string `json:"cursor,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	WithIcons *bool  `json:"with_icons,omitempty"`
}

// NotifAppsRespPayload is the body of a TypeNotifAppsResp envelope
// (phone -> Mac, same /notif lane, req_id correlation like file-list).
// Entries holds at most MaxNotifAppsPerResp rows sorted by package_name.
// Error is set when the listing failed; ErrorCode is machine-readable
// (invalid_arg, internal).
type NotifAppsRespPayload struct {
	Nonce      string          `json:"nonce"`
	ReqID      string          `json:"req_id"`
	Entries    []NotifAppEntry `json:"entries,omitempty"`
	NextCursor string          `json:"next_cursor,omitempty"`
	Error      string          `json:"error,omitempty"`
	ErrorCode  string          `json:"error_code,omitempty"`
}

// ClipPushPayload is the body of a TypeClipPush envelope. ChangedAt orders
// pushes; Origin breaks echo loops ("mac"/"macos" == Mac side).
// Kind discriminates text vs image (empty defaults to text for compat).
// Image payloads carry Mime + ImageB64 (base64, max 5 MiB raw). Text
// payloads carry Text (max 256KB). Exactly one of Text/ImageB64 should be set.
// Filename is optional basename (max 255) for UTI/extension preservation; empty
// means synthesize from mime on the receiver.
// ContentHash is additive sha256 hex of canonical bytes (text utf8 or raw
// image bytes): receivers dedupe on it, old peers ignore it.
// ChangedC is the additive HLC counter: new receivers compare
// (ChangedAt, ChangedC) lexicographically, old peers compare ChangedAt only.
// Sensitive marks OS-flagged secrets (Android EXTRA_IS_SENSITIVE, macOS
// ConcealedType): auto watchers skip unless the peer opts in, manual Send
// always bypasses.
type ClipPushPayload struct {
	Nonce       string `json:"nonce"`
	Kind        string `json:"kind,omitempty"`
	Text        string `json:"text,omitempty"`
	Mime        string `json:"mime,omitempty"`
	ImageB64    string `json:"image_b64,omitempty"`
	ChangedAt   int64  `json:"changed_at,omitempty"`
	ChangedC    int64  `json:"changed_c,omitempty"`
	Origin      string `json:"origin,omitempty"`
	Filename    string `json:"filename,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
	Sensitive   bool   `json:"sensitive,omitempty"`
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
// NotifMode absent/unknown means all_except_muted; muted/allowed lists
// absent mean empty. All 0.9.0+ filter fields are additive: older peers
// ignore them and interoperate as allow-all.
// PlaybackMode absent/unknown means both (0.12.0+ default, two-way);
// PlaybackOutput absent/unknown means inapp. Both are additive 0.10.0+:
// older peers ignore them.
type SettingsSyncPayload struct {
	Nonce                string   `json:"nonce"`
	NotificationsEnabled *bool    `json:"notifications_enabled,omitempty"`
	ClipboardMode        string   `json:"clipboard_mode,omitempty"`
	NotifMode            string   `json:"notif_mode,omitempty"`
	MutedPackages        []string `json:"muted_packages,omitempty"`
	AllowedPackages      []string `json:"allowed_packages,omitempty"`
	PlaybackMode         string   `json:"playback_mode,omitempty"`
	PlaybackOutput       string   `json:"playback_output,omitempty"`
	ClipboardAllowSensitive *bool `json:"clipboard_allow_sensitive,omitempty"`
	UpdatedUnix          int64    `json:"updated_unix"`
	UpdatedBy            string   `json:"updated_by,omitempty"`
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

// NormalizeNotifMode trims, lowercases, and canonicalizes a per-app filter
// mode. Unknown/empty input maps to NotifAllExceptMuted so older peers
// (missing field) interoperate as allow-all. Pure.
func NormalizeNotifMode(s string) string {
	n := strings.ToLower(strings.TrimSpace(s))
	switch n {
	case NotifAllExceptMuted, NotifOnlyAllowed:
		return n
	case "":
		return NotifAllExceptMuted
	default:
		return NotifAllExceptMuted
	}
}

// IsValidNotifMode reports whether s is one of the two canonical modes. Pure.
func IsValidNotifMode(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case NotifAllExceptMuted, NotifOnlyAllowed:
		return true
	default:
		return false
	}
}

// SanitizeNotifFilterList trims, drops empties, dedupes (case-sensitive:
// Android package names are case-sensitive), caps each entry at
// MaxNotifPackageLen runes via SanitizePackageName, caps the list at
// MaxNotifFilterApps, and sorts for a stable LWW compare. Pure.
func SanitizeNotifFilterList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		pkg := SanitizePackageName(raw)
		if pkg == "" {
			continue
		}
		if _, ok := seen[pkg]; ok {
			continue
		}
		seen[pkg] = struct{}{}
		out = append(out, pkg)
		if len(out) >= MaxNotifFilterApps {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	// Insertion sort: lists are tiny (<=100); avoid importing sort for one call.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// ShouldMirrorNotif is the single canonical per-app filter (code-judo: one
// predicate instead of scattered ifs in Kotlin/Dart/Go adapters). Progress
// always loses (Play Store downloads etc. must never spam the Mac);
// otherwise the mode decides: all_except_muted drops muted only,
// only_allowed keeps allowed only. Empty package is never filtered by list
// (absent field from old senders). Pure.
func ShouldMirrorNotif(mode string, muted, allowed []string, packageName string, hasProgress bool) bool {
	if hasProgress {
		return false
	}
	m := NormalizeNotifMode(mode)
	pkg := SanitizePackageName(packageName)
	switch m {
	case NotifOnlyAllowed:
		if pkg == "" {
			return false
		}
		for _, a := range allowed {
			if a == pkg {
				return true
			}
		}
		return false
	default:
		if pkg == "" {
			return true
		}
		for _, b := range muted {
			if b == pkg {
				return false
			}
		}
		return true
	}
}

// IsStaleNotifPost reports whether a post timestamp is too old for live-only
// mirroring: postedAt <= 0 means unknown (kept: old senders omit it),
// otherwise it must be within maxAgeSec of nowUnix. Pure.
func IsStaleNotifPost(postedAt, nowUnix, maxAgeSec int64) bool {
	if postedAt <= 0 {
		return false
	}
	if maxAgeSec <= 0 {
		return false
	}
	return nowUnix-postedAt > maxAgeSec
}

// SanitizeNotifID trims an ID and reports usability (1..MaxNotifIDLen runes). Pure.
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

// NotifAppsWantIcons reports whether a request wants per-entry icons:
// absent (nil) means true for backward compat with labels-only callers
// that omit the field. Pure.
func NotifAppsWantIcons(p NotifAppsReqPayload) bool {
	if p.WithIcons == nil {
		return true
	}
	return *p.WithIcons
}

// EffectiveNotifAppsLimit returns the page size for a request: explicit
// 1..MaxNotifAppsPerResp wins, 0 (absent) means DefaultNotifAppsLimit,
// out-of-range values pass through so validators can reject them. Pure.
func EffectiveNotifAppsLimit(limit int) int {
	if limit == 0 {
		return DefaultNotifAppsLimit
	}
	return limit
}

// SanitizeNotifAppsReq validates an app-inventory request: nonce present,
// req_id 1..64 chars, cursor empty or a valid package, limit 0 (default)
// or 1..MaxNotifAppsPerResp. Pure.
func SanitizeNotifAppsReq(p NotifAppsReqPayload) bool {
	if p.Nonce == "" {
		return false
	}
	reqID := strings.TrimSpace(p.ReqID)
	if reqID == "" || len(reqID) > MaxNotifAppsReqIDLen {
		return false
	}
	if p.Cursor != "" && SanitizePackageName(p.Cursor) == "" {
		return false
	}
	if p.Limit != 0 && (p.Limit < 1 || p.Limit > MaxNotifAppsPerResp) {
		return false
	}
	return true
}

// SanitizeNotifAppEntry validates one inventory row: package required
// (1..128), label at most MaxNotifAppLen runes, icon empty or valid
// base64 within MaxNotifIconB64Len. Pure.
func SanitizeNotifAppEntry(e NotifAppEntry) bool {
	if SanitizePackageName(e.PackageName) == "" {
		return false
	}
	if len([]rune(e.App)) > MaxNotifAppLen {
		return false
	}
	if e.IconB64 == "" {
		return true
	}
	return SanitizeNotifIconB64(e.IconB64) != ""
}

// SanitizeNotifAppsResp validates an app-inventory response: nonce + req_id
// present, at most MaxNotifAppsPerResp entries (each a valid row),
// next_cursor empty or a valid package, error at most 512 chars with a
// known error_code. Pure. Receivers additionally drop invalid icons
// fail-soft per row (defense in depth: the gate rejects whole malformed
// pages, ingest never throws on one bad row).
func SanitizeNotifAppsResp(p NotifAppsRespPayload) bool {
	if p.Nonce == "" {
		return false
	}
	reqID := strings.TrimSpace(p.ReqID)
	if reqID == "" || len(reqID) > MaxNotifAppsReqIDLen {
		return false
	}
	if len(p.Entries) > MaxNotifAppsPerResp {
		return false
	}
	for _, e := range p.Entries {
		if !SanitizeNotifAppEntry(e) {
			return false
		}
	}
	if p.NextCursor != "" && SanitizePackageName(p.NextCursor) == "" {
		return false
	}
	if len(p.Error) > 512 {
		return false
	}
	if !SanitizeErrorCode(p.ErrorCode) {
		return false
	}
	return true
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
// Pure: checks PNG/JPEG/GIF/WEBP/HEIC/HEIF/TIFF signatures with brand
// allowlists; does not validate size cap.
func SniffImageMime(raw []byte) string {
	if len(raw) >= 8 && raw[0] == 0x89 && raw[1] == 0x50 && raw[2] == 0x4E && raw[3] == 0x47 &&
		raw[4] == 0x0D && raw[5] == 0x0A && raw[6] == 0x1A && raw[7] == 0x0A {
		return "image/png"
	}
	if len(raw) >= 3 && raw[0] == 0xFF && raw[1] == 0xD8 && raw[2] == 0xFF {
		return "image/jpeg"
	}
	if len(raw) >= 6 && raw[0] == 'G' && raw[1] == 'I' && raw[2] == 'F' && raw[3] == '8' &&
		(raw[4] == '7' || raw[4] == '9') && raw[5] == 'a' {
		return "image/gif"
	}
	if len(raw) >= 12 && raw[0] == 'R' && raw[1] == 'I' && raw[2] == 'F' && raw[3] == 'F' && raw[8] == 'W' && raw[9] == 'E' && raw[10] == 'B' && raw[11] == 'P' {
		return "image/webp"
	}
	if len(raw) >= 12 && raw[4] == 'f' && raw[5] == 't' && raw[6] == 'y' && raw[7] == 'p' {
		return sniffHEICBrand(raw)
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

// sniffHEICBrand distinguishes heic/heif stills from sequences and foreign
// ftyp boxes (avif/mp4). Pure.
func sniffHEICBrand(raw []byte) string {
	if len(raw) < 12 {
		return ""
	}
	brands := string(raw[8:min(12, len(raw))])
	compat := ""
	if len(raw) >= 32 {
		compat = string(raw[16:32])
	} else if len(raw) > 16 {
		compat = string(raw[16:])
	}
	blob := brands + "\x00" + compat
	for _, still := range []string{"heic", "heix", "heim", "heis"} {
		if strings.Contains(blob, still) {
			return "image/heic"
		}
	}
	// Sequence brands are valid HEIF-family but deferred in v1 (no agreed
	// still-vs-video wire form): callers treat "" as foreign-mime loud-defer.
	for _, seq := range []string{"msf1", "hevc", "hevx", "hevs"} {
		if strings.Contains(blob, seq) {
			return ""
		}
	}
	if strings.Contains(blob, "mif1") {
		return "image/heif"
	}
	return ""
}

// sanitizeMagic validates magic bytes for a normalized MIME. Pure.
func sanitizeMagic(mime string, raw []byte) bool {
	switch mime {
	case "image/png":
		return len(raw) >= 8 && raw[0] == 0x89 && raw[1] == 0x50 && raw[2] == 0x4E && raw[3] == 0x47 &&
			raw[4] == 0x0D && raw[5] == 0x0A && raw[6] == 0x1A && raw[7] == 0x0A
	case "image/jpeg":
		return len(raw) >= 3 && raw[0] == 0xFF && raw[1] == 0xD8 && raw[2] == 0xFF
	case "image/gif":
		return len(raw) >= 6 && raw[0] == 'G' && raw[1] == 'I' && raw[2] == 'F' && raw[3] == '8' &&
			(raw[4] == '7' || raw[4] == '9') && raw[5] == 'a'
	case "image/webp":
		if len(raw) < 12 || raw[0] != 'R' || raw[1] != 'I' || raw[2] != 'F' || raw[3] != 'F' ||
			raw[8] != 'W' || raw[9] != 'E' || raw[10] != 'B' || raw[11] != 'P' {
			return false
		}
		// RIFF size must equal len-8 (catches truncation).
		size := int(raw[4]) | int(raw[5])<<8 | int(raw[6])<<16 | int(raw[7])<<24
		return size == len(raw)-8
	case "image/heic", "image/heif":
		if len(raw) < 12 || raw[4] != 'f' || raw[5] != 't' || raw[6] != 'y' || raw[7] != 'p' {
			return false
		}
		return SniffImageMime(raw) == mime
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
// Nonce is required (proto); origin empty means unknown (receiver defaults
// per direction for backward compat); content_hash empty means absent.
func SanitizeClipPush(p ClipPushPayload) bool {
	if p.Nonce == "" {
		return false
	}
	if p.ChangedAt < 0 || p.ChangedC < 0 {
		return false
	}
	if !SanitizeContentHash(p.ContentHash) {
		return false
	}
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
// closed, clipboard_mode and notif_mode are canonicalized (unknown→default,
// fail-soft), filter lists are trimmed/deduped/capped, playback_mode maps
// unknown→android_to_mac and playback_output unknown→inapp (fail-soft).
// Pure.
func SanitizeSettings(p SettingsSyncPayload) (SettingsSyncPayload, bool) {
	if p.UpdatedUnix < 0 {
		return SettingsSyncPayload{}, false
	}
	p.UpdatedBy = NormalizeUpdatedBy(p.UpdatedBy)
	p.ClipboardMode = NormalizeClipboardMode(p.ClipboardMode)
	p.NotifMode = NormalizeNotifMode(p.NotifMode)
	p.MutedPackages = SanitizeNotifFilterList(p.MutedPackages)
	p.AllowedPackages = SanitizeNotifFilterList(p.AllowedPackages)
	p.PlaybackMode = NormalizePlaybackMode(p.PlaybackMode)
	p.PlaybackOutput = NormalizePlaybackOutput(p.PlaybackOutput)
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


