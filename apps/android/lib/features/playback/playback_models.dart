// SPDX-License-Identifier: AGPL-3.0-only

// Playback models mirroring packages/proto/playback.json and core playback
// sanitize rules. Phone is the state source in 0.10.0. Pure, no platform.
import 'dart:convert';
class PlaybackState {
  const PlaybackState({
    required this.title,
    required this.artist,
    required this.album,
    required this.packageName,
    required this.app,
    required this.state,
    required this.positionMs,
    required this.durationMs,
    required this.updatedMs,
    this.artworkB64 = '',
    this.artworkMime = '',
    this.origin = 'android',
  });

  final String title;
  final String artist;
  final String album;
  final String packageName;
  final String app;
  final String state;
  final int positionMs;
  final int durationMs;
  final int updatedMs;
  final String artworkB64;
  final String artworkMime;
  final String origin;

  static const playing = 'playing';
  static const paused = 'paused';
  static const stopped = 'stopped';
  static const maxFieldLen = 128;
  static const maxArtworkB64Len = 131072;
  // Deprecated: event-driven push needs no position interval. Kept for
  // signature/test compatibility only.
  static const positionIntervalMs = 5000;

  static String normalizeState(String s) {
    final n = s.trim().toLowerCase();
    if (n == playing || n == paused) return n;
    return stopped;
  }

  static String truncate(String s, int max) {
    final t = s.trim();
    if (t.runes.length <= max) return t;
    return String.fromCharCodes(t.runes.take(max)).trim();
  }

  bool get isPlaying => normalizeState(state) == playing;

  /// Fail-soft decode: absent state means stopped, negative times clamp,
  /// oversize artwork drops but metadata survives. Returns null only when
  /// updatedMs is negative (fail closed, like core).
  static PlaybackState? fromJson(Map<String, dynamic> json) {
    final upd = json['updated_ms'];
    if (upd is int && upd < 0) return null;
    var art = '';
    var mime = '';
    final rawArt = json['artwork_b64'];
    final rawMime = json['artwork_mime'];
    if (rawArt is String && rawArt.trim().isNotEmpty) {
      final t = rawArt.trim();
      final m = rawMime is String ? rawMime.trim().toLowerCase() : '';
      final mm = m == 'image/jpg' ? 'image/jpeg' : m;
      if (t.length <= maxArtworkB64Len &&
          (mm == 'image/jpeg' || mm == 'image/png')) {
        try {
          base64Decode(t);
          art = t;
          mime = mm;
        } catch (_) {
          // Invalid base64 drops fail-soft.
        }
      }
    }
    int ms(Object? v) => v is int && v >= 0 ? v : 0;
    String str(Object? v, int max) =>
        v is String ? truncate(v, max) : '';
    final dur = ms(json['duration_ms']);
    var pos = ms(json['position_ms']);
    if (dur > 0 && pos > dur + 5000) return null;
    if (pos > 24 * 60 * 60 * 1000 || dur > 24 * 60 * 60 * 1000) return null;
    return PlaybackState(
      title: str(json['title'], maxFieldLen),
      artist: str(json['artist'], maxFieldLen),
      album: str(json['album'], maxFieldLen),
      packageName: (json['package_name'] is String
              ? (json['package_name'] as String).trim()
              : '')
          .substring(
              0,
              (json['package_name'] is String
                      ? (json['package_name'] as String).trim().length
                      : 0) >
                  128
              ? 128
              : (json['package_name'] is String
                  ? (json['package_name'] as String).trim().length
                  : 0)),
      app: str(json['app'], 64),
      state: json['state'] is String
          ? normalizeState(json['state'] as String)
          : stopped,
      positionMs: pos,
      durationMs: dur,
      updatedMs: upd is int && upd >= 0 ? upd : 0,
      artworkB64: art,
      artworkMime: mime,
      origin: json['origin'] is String
          ? (json['origin'] as String).trim().toLowerCase() == 'macos'
              ? 'mac'
              : (json['origin'] as String).trim().toLowerCase()
          : 'android',
    );
  }

  Map<String, Object?> toJson() => {
        'title': title,
        'artist': artist,
        'album': album,
        'package_name': packageName,
        'app': app,
        'state': normalizeState(state),
        'position_ms': positionMs,
        'duration_ms': durationMs,
        'updated_ms': updatedMs,
        if (artworkB64.isNotEmpty) 'artwork_b64': artworkB64,
        if (artworkB64.isNotEmpty) 'artwork_mime': artworkMime,
        'origin': origin,
      };

  /// Latest-wins: strictly greater updatedMs wins, ties keep local.
  static bool remoteWins(int localMs, int remoteMs) {
    if (remoteMs <= 0) return false;
    return remoteMs > localMs;
  }

  /// Event-driven dedupe (no polling): track identity, transport state,
  /// duration, or artwork changes send immediately; bare progress never
  /// sends (the Mac interpolates from position_ms+updated_ms). Pure.
  /// [nowMs] is accepted for signature compatibility and ignored.
  bool shouldSendAfter(PlaybackState? last, int nowMs) {
    if (last == null) return true;
    if (title != last.title ||
        artist != last.artist ||
        album != last.album ||
        packageName != last.packageName) {
      return true;
    }
    if (normalizeState(state) != normalizeState(last.state)) return true;
    if (durationMs != last.durationMs) return true;
    if (artworkB64 != last.artworkB64) return true;
    return false;
  }
}

/// Transport command validation. Pure.
class PlaybackCmd {
  static const valid = {'play', 'pause', 'toggle', 'next', 'prev'};

  static bool isValid(String cmd) =>
      valid.contains(cmd.trim().toLowerCase());
}
