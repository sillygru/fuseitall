// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package core

import (
	"encoding/base64"
	"strings"
)

// Playback sync (capability playback, build 10). Phone is the state source
// in 0.10.0: playback-state flows phone->Mac, playback-cmd flows Mac->phone.
// Every type rides the Envelope contract (packages/proto/playback.json);
// decoders ignore unknown fields, encoders emit only known ones. Titles and
// artwork are never logged verbatim: handlers log lengths and IDs only.
const (
	// TypePlaybackState carries the now-playing snapshot (phone -> Mac).
	// Capability: CapabilityPlayback.
	TypePlaybackState = "playback-state"
	// TypePlaybackCmd carries one transport command (Mac -> phone).
	// Capability: CapabilityPlayback.
	TypePlaybackCmd = "playback-cmd"

	// CapabilityPlayback advertises playback sync.
	CapabilityPlayback = "playback"
)

// Playback sync directions. AndroidToMac (phone -> Mac only, view-only) is
// the 0.10.0 default: state shows on the Mac, commands are blocked.
const (
	PlaybackBoth         = "both"
	PlaybackMacToAndroid = "mac_to_android"
	PlaybackAndroidToMac = "android_to_mac"
	PlaybackDisabled     = "disabled"
)

// Mac presentation outputs. InApp renders only inside the app player;
// System renders in-app plus the Control Center Now Playing mirror.
const (
	PlaybackOutputInApp  = "inapp"
	PlaybackOutputSystem = "system"
)

// Playback transport states.
const (
	PlaybackPlaying = "playing"
	PlaybackPaused  = "paused"
	PlaybackStopped = "stopped"
)

// Playback transport commands.
const (
	PlaybackCmdPlay   = "play"
	PlaybackCmdPause  = "pause"
	PlaybackCmdToggle = "toggle"
	PlaybackCmdNext   = "next"
	PlaybackCmdPrev   = "prev"
)

// Playback caps (mirror packages/proto/playback.json).
const (
	MaxPlaybackTitleLen  = 128
	MaxPlaybackArtistLen = 128
	MaxPlaybackAlbumLen  = 128
	MaxPlaybackAppLen    = 64
	// MaxPlaybackArtworkB64Len caps cover art at ~128KB b64 (downscaled
	// ~96KB raw JPEG/PNG so one state stays far under MaxBodyBytes).
	MaxPlaybackArtworkB64Len = 131072
	// PlaybackPositionIntervalMs is deprecated: state is event-driven push
	// (MediaController callbacks) with identity-only dedupe, no position
	// polling. Kept for backward-compatible reference only.
	PlaybackPositionIntervalMs = int64(5000)
	// PlaybackMaxDurationMs caps duration/position at 24h (garbage guard).
	PlaybackMaxDurationMs = int64(24 * 60 * 60 * 1000)
)

// PlaybackStatePayload is the body of a TypePlaybackState envelope.
// UpdatedMs orders snapshots (unix millis, greater wins). ArtworkB64 empty
// means absent; receivers drop invalid artwork fail-soft and keep metadata.
type PlaybackStatePayload struct {
	Nonce       string `json:"nonce"`
	Title       string `json:"title,omitempty"`
	Artist      string `json:"artist,omitempty"`
	Album       string `json:"album,omitempty"`
	PackageName string `json:"package_name,omitempty"`
	App         string `json:"app,omitempty"`
	State       string `json:"state,omitempty"`
	PositionMs  int64  `json:"position_ms,omitempty"`
	DurationMs  int64  `json:"duration_ms,omitempty"`
	UpdatedMs   int64  `json:"updated_ms,omitempty"`
	ArtworkB64  string `json:"artwork_b64,omitempty"`
	ArtworkMime string `json:"artwork_mime,omitempty"`
	Origin      string `json:"origin,omitempty"`
}

// PlaybackCmdPayload is the body of a TypePlaybackCmd envelope.
// Nonce doubles as the idempotency key: receivers drop duplicate nonces.
type PlaybackCmdPayload struct {
	Nonce  string `json:"nonce"`
	Cmd    string `json:"cmd"`
	Origin string `json:"origin,omitempty"`
}

// NormalizePlaybackMode trims, lowercases, and canonicalizes a playback
// direction. Unknown/empty input maps to PlaybackAndroidToMac (phone -> Mac
// only, view-only) so older peers (missing field) interoperate as the
// 0.10.0 default. Pure.
func NormalizePlaybackMode(s string) string {
	n := strings.ToLower(strings.TrimSpace(s))
	switch n {
	case PlaybackBoth, PlaybackMacToAndroid, PlaybackAndroidToMac, PlaybackDisabled:
		return n
	case "":
		return PlaybackAndroidToMac
	default:
		return PlaybackAndroidToMac
	}
}

// IsValidPlaybackMode reports whether s is one of the four canonical modes. Pure.
func IsValidPlaybackMode(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case PlaybackBoth, PlaybackMacToAndroid, PlaybackAndroidToMac, PlaybackDisabled:
		return true
	default:
		return false
	}
}

// PlaybackModeAllowsSend reports whether the given mode allows a send from
// the caller side. State sends originate on android, command sends on mac;
// the mode gates both symmetrically like clipboard (both = either side,
// android_to_mac = android only, mac_to_android = mac only). Pure.
func PlaybackModeAllowsSend(mode, origin string) bool {
	m := NormalizePlaybackMode(mode)
	o := NormalizeOrigin(origin)
	switch m {
	case PlaybackDisabled:
		return false
	case PlaybackMacToAndroid:
		return o == OriginMac
	case PlaybackAndroidToMac:
		return o == OriginAndroid
	default:
		return true
	}
}

// PlaybackModeAllowsReceive reports whether the given mode allows receiving
// (and applying) for an incoming origin. Pure.
func PlaybackModeAllowsReceive(mode, origin string) bool {
	return PlaybackModeAllowsSend(mode, origin)
}

// PlaybackModeAllowsState reports whether phone state may display on the
// Mac under this mode (both + android_to_mac). Pure.
func PlaybackModeAllowsState(mode string) bool {
	m := NormalizePlaybackMode(mode)
	return m == PlaybackBoth || m == PlaybackAndroidToMac
}

// PlaybackModeAllowsCommand reports whether Mac commands may send to the
// phone under this mode (both + mac_to_android). Pure.
func PlaybackModeAllowsCommand(mode string) bool {
	m := NormalizePlaybackMode(mode)
	return m == PlaybackBoth || m == PlaybackMacToAndroid
}

// NormalizePlaybackOutput trims, lowercases, and canonicalizes the Mac
// presentation output. Unknown/empty maps to InApp (safest: no OS side
// effects). Pure.
func NormalizePlaybackOutput(s string) string {
	n := strings.ToLower(strings.TrimSpace(s))
	switch n {
	case PlaybackOutputSystem:
		return PlaybackOutputSystem
	default:
		return PlaybackOutputInApp
	}
}

// IsValidPlaybackOutput reports whether s is a known output. Pure.
func IsValidPlaybackOutput(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case PlaybackOutputInApp, PlaybackOutputSystem:
		return true
	default:
		return false
	}
}

// NormalizePlaybackState canonicalizes a transport state. Unknown/empty
// maps to stopped so receivers never render a phantom playing row. Pure.
func NormalizePlaybackState(s string) string {
	n := strings.ToLower(strings.TrimSpace(s))
	switch n {
	case PlaybackPlaying, PlaybackPaused:
		return n
	default:
		return PlaybackStopped
	}
}

// IsValidPlaybackCmd reports whether s is a known transport command. Pure.
func IsValidPlaybackCmd(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case PlaybackCmdPlay, PlaybackCmdPause, PlaybackCmdToggle, PlaybackCmdNext, PlaybackCmdPrev:
		return true
	default:
		return false
	}
}

// AllowedPlaybackArtworkMimes whitelists downscaled cover art formats.
var AllowedPlaybackArtworkMimes = map[string]bool{
	"image/jpeg": true,
	"image/jpg":  true,
	"image/png":  true,
}

// SanitizePlaybackArtworkMime lowercases and validates artwork MIME. Pure.
func SanitizePlaybackArtworkMime(s string) (string, bool) {
	n := strings.ToLower(strings.TrimSpace(s))
	if n == "image/jpg" {
		n = "image/jpeg"
	}
	if AllowedPlaybackArtworkMimes[n] {
		return n, true
	}
	return "", false
}

// SanitizePlaybackArtwork validates base64 cover art: length cap, base64
// shape, MIME whitelist. Empty input is valid (means absent). Oversize or
// invalid returns ok=false so callers drop artwork fail-soft and keep
// metadata. Pure.
func SanitizePlaybackArtwork(b64, mime string) bool {
	trimmed := strings.TrimSpace(b64)
	if trimmed == "" {
		return true
	}
	if len(trimmed) > MaxPlaybackArtworkB64Len {
		return false
	}
	if _, ok := SanitizePlaybackArtworkMime(mime); !ok {
		return false
	}
	if _, err := base64.StdEncoding.DecodeString(trimmed); err != nil {
		return false
	}
	return true
}

// truncateRunes trims s and caps it at max runes (fail-soft). Pure.
func truncateRunes(s string, max int) string {
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

// SanitizePlaybackState validates a state snapshot: nonce present,
// updated_ms non-negative, position/duration within 0..24h,
// position <= duration when duration known, artwork valid (fail-soft flag
// via second return). Display fields are truncated fail-soft, never
// rejected. Pure.
func SanitizePlaybackState(p PlaybackStatePayload) (PlaybackStatePayload, bool) {
	if strings.TrimSpace(p.Nonce) == "" {
		return PlaybackStatePayload{}, false
	}
	if p.UpdatedMs < 0 || p.PositionMs < 0 || p.DurationMs < 0 {
		return PlaybackStatePayload{}, false
	}
	if p.PositionMs > PlaybackMaxDurationMs || p.DurationMs > PlaybackMaxDurationMs {
		return PlaybackStatePayload{}, false
	}
	if p.DurationMs > 0 && p.PositionMs > p.DurationMs+5000 {
		return PlaybackStatePayload{}, false
	}
	p.Title = truncateRunes(p.Title, MaxPlaybackTitleLen)
	p.Artist = truncateRunes(p.Artist, MaxPlaybackArtistLen)
	p.Album = truncateRunes(p.Album, MaxPlaybackAlbumLen)
	p.App = truncateRunes(p.App, MaxPlaybackAppLen)
	p.PackageName = SanitizePackageName(p.PackageName)
	p.State = NormalizePlaybackState(p.State)
	p.Origin = NormalizeOrigin(p.Origin)
	if strings.TrimSpace(p.ArtworkB64) != "" && !SanitizePlaybackArtwork(p.ArtworkB64, p.ArtworkMime) {
		p.ArtworkB64 = ""
		p.ArtworkMime = ""
	}
	if p.ArtworkB64 == "" {
		p.ArtworkMime = ""
	} else if m, ok := SanitizePlaybackArtworkMime(p.ArtworkMime); ok {
		p.ArtworkMime = m
	}
	return p, true
}

// SanitizePlaybackCmd validates a transport command: nonce + known cmd.
// Origin is normalized ("macos" -> "mac"). Pure.
func SanitizePlaybackCmd(p PlaybackCmdPayload) (PlaybackCmdPayload, bool) {
	if strings.TrimSpace(p.Nonce) == "" {
		return PlaybackCmdPayload{}, false
	}
	cmd := strings.ToLower(strings.TrimSpace(p.Cmd))
	if !IsValidPlaybackCmd(cmd) {
		return PlaybackCmdPayload{}, false
	}
	p.Cmd = cmd
	p.Origin = NormalizeOrigin(p.Origin)
	return p, true
}

// RemotePlaybackWins reports whether an incoming state snapshot should
// replace the local one: strictly greater UpdatedMs wins; ties keep local
// (idempotent redelivery must not flap). Zero UpdatedMs never wins over a
// stamped value. Pure.
func RemotePlaybackWins(localUpdatedMs, remoteUpdatedMs int64) bool {
	if remoteUpdatedMs <= 0 {
		return false
	}
	return remoteUpdatedMs > localUpdatedMs
}

// ShouldSendPlaybackState reports whether next should be transmitted given
// the last transmitted snapshot. Event-driven (no polling): track identity,
// transport state, or duration changes send immediately; bare progress never
// sends (receivers interpolate position locally from position_ms+updated_ms).
// A nil last always sends. nowMs is accepted for signature compatibility and
// ignored. Pure.
func ShouldSendPlaybackState(last *PlaybackStatePayload, next PlaybackStatePayload, nowMs int64) bool {
	if last == nil {
		return true
	}
	if next.Title != last.Title || next.Artist != last.Artist || next.Album != last.Album || next.PackageName != last.PackageName {
		return true
	}
	if NormalizePlaybackState(next.State) != NormalizePlaybackState(last.State) {
		return true
	}
	if next.DurationMs != last.DurationMs {
		return true
	}
	if next.ArtworkB64 != last.ArtworkB64 {
		return true
	}
	return false
}

// PlaybackTrackID returns a stable low-cardinality identity for a snapshot
// (package + title + artist + album) for logging only, never PII bodies.
// Pure.
func PlaybackTrackID(p PlaybackStatePayload) string {
	pkg := SanitizePackageName(p.PackageName)
	if pkg == "" {
		pkg = "unknown-player"
	}
	return pkg
}
