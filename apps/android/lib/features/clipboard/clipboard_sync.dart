// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Latest-wins clipboard state with echo suppression. Mirrors core
// RemoteClipWins/SanitizeClipText: strictly newer changedAt wins, ties keep
// local, zero stamps never win, over-256KB text is rejected (never
// truncated). Plain text only; contents never reach logs.
class ClipState {
  const ClipState({
    this.text = '',
    this.changedAt = 0,
    this.origin = '',
    this.hasText = false,
    this.pending = false,
  });

  final String text;
  final int changedAt;
  final String origin;
  final bool hasText;
  final bool pending;

  static const maxLen = 256 * 1024;

  static bool validText(String s) => s.length <= maxLen;

  /// Record a local copy: stamps origin android, arms pending. Identical
  /// text is a no-op (pollers re-report the same value). Over-long input
  /// returns null: fail closed without touching state.
  ClipState? setLocal(String next, int nowUnix) {
    if (!validText(next)) return null;
    if (hasText && text == next) return this;
    return ClipState(
      text: next,
      changedAt: nowUnix,
      origin: 'android',
      hasText: true,
      pending: true,
    );
  }

  /// Adopt an incoming push when strictly newer. Returns null when dropped
  /// (stale, echo of our own origin, or invalid). Adopted state disarms
  /// pending so it never bounces back.
  ClipState? applyRemote({
    required String next,
    required int nextChangedAt,
    required String nextOrigin,
  }) {
    if (!validText(next)) return null;
    if (nextChangedAt <= 0 || nextChangedAt <= changedAt) return null;
    final origin = nextOrigin.trim().toLowerCase() == 'macos'
        ? 'mac'
        : nextOrigin.trim().toLowerCase();
    if (origin == 'android') return null;
    return ClipState(
      text: next,
      changedAt: nextChangedAt,
      origin: origin.isEmpty ? 'mac' : origin,
      hasText: true,
      pending: false,
    );
  }

  /// Take the queued push for upload (clears pending). Null when idle.
  ({String text, int changedAt})? takePending() {
    if (!pending || !hasText) return null;
    return (text: text, changedAt: changedAt);
  }

  ClipState clearPending() => ClipState(
        text: text,
        changedAt: changedAt,
        origin: origin,
        hasText: hasText,
      );

  ClipState requeue() => ClipState(
        text: text,
        changedAt: changedAt,
        origin: origin,
        hasText: hasText,
        pending: true,
      );
}

/// Whether an inbound Mac push may apply under [mode]. Pure: off and
/// phone_to_mac block inbound... (mac_to_phone allows Mac origin; two_way
/// allows; echoes of android origin never apply — enforced by applyRemote).
bool shouldAcceptRemoteClip(String mode, String origin) {
  final o = origin.trim().toLowerCase();
  if (o == 'android') return false;
  return mode == 'mac_to_phone' || mode == 'two_way';
}

/// Preview for list rows: first 200 chars. Pure.
String clipPreview(String s) {
  if (s.length <= 200) return s;
  return s.substring(0, 200);
}
