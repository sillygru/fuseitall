// SPDX-License-Identifier: AGPL-3.0-only

// Latest-wins clipboard state with echo suppression. Mirrors core
// RemoteClipWinsEx/SanitizeClipText/SanitizeClipImage: strictly greater
// (changedAt, changedC) wins, ties keep local, zero stamps never win,
// over-256KB text or over-5MiB inline image rejected (large images ride the
// chunk lane). Contents never reach logs.
import 'dart:convert';

import 'package:crypto/crypto.dart';

class ClipState {
  const ClipState({
    this.text = '',
    this.changedAt = 0,
    this.changedC = 0,
    this.origin = '',
    this.hasText = false,
    this.pending = false,
    this.kind = 'text',
    this.mime = '',
    this.imageB64 = '',
    this.filename = '',
    this.contentHash = '',
    this.sensitive = false,
    this.seenNonces = const {},
  });

  final String text;
  final int changedAt;
  final int changedC;
  final String origin;
  final bool hasText;
  final bool pending;
  final String kind;
  final String mime;
  final String imageB64;
  final String filename;
  final String contentHash;
  final bool sensitive;
  final Set<String> seenNonces;

  static const maxLen = 256 * 1024;
  static const maxImageB64Len = 7100000;
  static const maxImageRaw = 5 * 1024 * 1024;
  static const autoImageRaw = 5 * 1024 * 1024;
  static const chunkRaw = 1024 * 1024;
  static const maxTotalRaw = 25 * 1024 * 1024;

  static const allowedMimes = {
    'image/png',
    'image/jpeg',
    'image/jpg',
    'image/webp',
    'image/gif',
    'image/tiff',
    'image/heic',
    'image/heif',
  };

  static bool validText(String s) => s.length <= maxLen;

  static String normalizeKind(String s) {
    final n = s.trim().toLowerCase();
    if (n == 'image') return 'image';
    return 'text';
  }

  static String? normalizeMime(String s) {
    var n = s.trim().toLowerCase();
    if (n == 'image/jpg') n = 'image/jpeg';
    return allowedMimes.contains(n) ? n : null;
  }

  static String? sanitizeFilename(String s) {
    var trimmed = s.trim();
    if (trimmed.isEmpty) return null;
    // Basename only, reject dirs/traversal.
    var base = trimmed.split('/').last.split('\\').last.trim();
    if (base.isEmpty || base == '.' || base == '..') return null;
    if (base.contains('/') || base.contains('\\')) return null;
    if (base.length > 255) return null;
    final safe = RegExp(r'^[A-Za-z0-9._-]+$');
    if (!safe.hasMatch(base)) return null;
    return base;
  }

  static String sanitizeFilenameOrEmpty(String s) => sanitizeFilename(s) ?? '';

  static String clipExt(String mime) {
    switch (mime.toLowerCase().trim()) {
      case 'image/jpeg':
        return '.jpg';
      case 'image/png':
        return '.png';
      case 'image/webp':
        return '.webp';
      case 'image/gif':
        return '.gif';
      case 'image/tiff':
        return '.tiff';
      case 'image/heic':
      case 'image/heif':
        return '.heic';
      default:
        return '.png';
    }
  }

  static String sniffMime(List<int> raw) {
    if (raw.length >= 8 && raw[0] == 0x89 && raw[1] == 0x50 && raw[2] == 0x4E && raw[3] == 0x47) return 'image/png';
    if (raw.length >= 3 && raw[0] == 0xFF && raw[1] == 0xD8 && raw[2] == 0xFF) return 'image/jpeg';
    if (raw.length >= 6 && raw[0] == 0x47 && raw[1] == 0x49 && raw[2] == 0x46) return 'image/gif';
    if (raw.length >= 12 && raw[0] == 0x52 && raw[1] == 0x49 && raw[2] == 0x46 && raw[3] == 0x46 && raw[8] == 0x57 && raw[9] == 0x45 && raw[10] == 0x42 && raw[11] == 0x50) return 'image/webp';
    if (raw.length >= 12 && raw[4] == 0x66 && raw[5] == 0x74 && raw[6] == 0x79 && raw[7] == 0x70) return 'image/heic';
    if (raw.length >= 4) {
      if (raw[0] == 0x49 && raw[1] == 0x49 && raw[2] == 0x2A && raw[3] == 0x00) return 'image/tiff';
      if (raw[0] == 0x4D && raw[1] == 0x4D && raw[2] == 0x00 && raw[3] == 0x2A) return 'image/tiff';
    }
    return '';
  }

  static bool validMagic(String mime, List<int> raw) {
    final m = mime.toLowerCase().trim() == 'image/jpg' ? 'image/jpeg' : mime.toLowerCase().trim();
    switch (m) {
      case 'image/png':
        return raw.length >= 8 && raw[0] == 0x89 && raw[1] == 0x50 && raw[2] == 0x4E && raw[3] == 0x47;
      case 'image/jpeg':
        return raw.length >= 3 && raw[0] == 0xFF && raw[1] == 0xD8 && raw[2] == 0xFF;
      case 'image/gif':
        return raw.length >= 6 && raw[0] == 0x47 && raw[1] == 0x49 && raw[2] == 0x46;
      case 'image/webp':
        return raw.length >= 12 && raw[0] == 0x52 && raw[1] == 0x49 && raw[2] == 0x46 && raw[3] == 0x46 && raw[8] == 0x57 && raw[9] == 0x45 && raw[10] == 0x42 && raw[11] == 0x50;
      case 'image/heic':
      case 'image/heif':
        return raw.length >= 12 && raw[4] == 0x66 && raw[5] == 0x74 && raw[6] == 0x79 && raw[7] == 0x70;
      case 'image/tiff':
        if (raw.length < 4) return false;
        return (raw[0] == 0x49 && raw[1] == 0x49 && raw[2] == 0x2A && raw[3] == 0x00) || (raw[0] == 0x4D && raw[1] == 0x4D && raw[2] == 0x00 && raw[3] == 0x2A);
      default:
        return false;
    }
  }

  static bool validImage(String b64, String mime) {
    if (b64.isEmpty || b64.length > maxImageB64Len) return false;
    if (normalizeMime(mime) == null) return false;
    try {
      final raw = base64Decode(b64);
      if (raw.isEmpty || raw.length > maxImageRaw) return false;
      if (!validMagic(mime, raw)) return false;
      return true;
    } catch (_) {
      return false;
    }
  }

  bool get isImage => kind == 'image';

  static String hashText(String s) => sha256.convert(utf8.encode(s)).toString();

  static String? hashImageB64(String b64) {
    try {
      return sha256.convert(base64Decode(b64)).toString();
    } catch (_) {
      return null;
    }
  }

  static int nextC(int localL, int localC, int wall) {
    if (wall > localL) return 0;
    return localC + 1;
  }

  static int promotedL(int localL, int wall) => wall > localL ? wall : localL;

  /// Record a local text copy: HLC-stamped origin android, arms pending.
  /// Identical text is a no-op. Over-long input returns null: fail closed.
  ClipState? setLocal(String next, int nowUnix, {bool sensitive = false}) {
    if (!validText(next)) return null;
    if (hasText && kind == 'text' && text == next) return this;
    final l = promotedL(changedAt, nowUnix);
    final c = nextC(changedAt, changedC, nowUnix);
    return ClipState(
      text: next,
      changedAt: l,
      changedC: c,
      origin: 'android',
      hasText: true,
      pending: true,
      kind: 'text',
      contentHash: hashText(next),
      sensitive: sensitive,
      seenNonces: seenNonces,
    );
  }

  /// Record a local image copy. B64 must be valid base64 + whitelisted mime.
  ClipState? setLocalImage(String b64, String mimeStr, int nowUnix, {bool sensitive = false}) =>
      setLocalImageWithFilename(b64, mimeStr, '', nowUnix, sensitive: sensitive);

  ClipState? setLocalImageWithFilename(String b64, String mimeStr, String filename, int nowUnix, {bool sensitive = false}) {
    final m = normalizeMime(mimeStr);
    if (m == null || !validImage(b64, m)) return null;
    final fn = sanitizeFilename(filename) ?? '';
    if (hasText && kind == 'image' && imageB64 == b64 && mime == m && this.filename == fn) return this;
    final l = promotedL(changedAt, nowUnix);
    final c = nextC(changedAt, changedC, nowUnix);
    return ClipState(
      changedAt: l,
      changedC: c,
      origin: 'android',
      hasText: true,
      pending: true,
      kind: 'image',
      mime: m,
      imageB64: b64,
      filename: fn,
      contentHash: hashImageB64(b64) ?? '',
      sensitive: sensitive,
      seenNonces: seenNonces,
    );
  }

  /// Adopt an incoming push when strictly greater (HLC). Returns null when
  /// dropped (stale/tie, echo of our own origin, duplicate nonce/hash, or
  /// invalid). Adopted state disarms pending so it never bounces back.
  ClipState? applyRemote({
    required String next,
    required int nextChangedAt,
    required String nextOrigin,
    String nextKind = 'text',
    String nextMime = '',
    String nextImageB64 = '',
    String nextFilename = '',
    int nextChangedC = 0,
    String nextHash = '',
    String nonce = '',
    bool nextSensitive = false,
  }) {
    if (nextChangedAt <= 0) return null;
    if (nextChangedAt < changedAt) return null;
    if (nextChangedAt == changedAt && nextChangedC <= changedC) return null;
    if (nonce.isNotEmpty && seenNonces.contains(nonce)) return null;
    final origin = nextOrigin.trim().toLowerCase() == 'macos'
        ? 'mac'
        : nextOrigin.trim().toLowerCase();
    if (origin == 'android') return null;
    if (nextHash.isNotEmpty && nextHash == contentHash && hasText) return null;
    final kind = normalizeKind(nextKind);
    final seen = nonce.isEmpty ? seenNonces : {...seenNonces, nonce};
    final capped = seen.length > 256 ? seen.skip(seen.length - 256).toSet() : seen;
    if (kind == 'image') {
      final m = normalizeMime(nextMime);
      if (m == null || !validImage(nextImageB64, m)) return null;
      final fn = sanitizeFilename(nextFilename) ?? '';
      return ClipState(
        changedAt: nextChangedAt,
        changedC: nextChangedC,
        origin: origin.isEmpty ? 'mac' : origin,
        hasText: true,
        pending: false,
        kind: 'image',
        mime: m,
        imageB64: nextImageB64,
        filename: fn,
        contentHash: nextHash.isNotEmpty ? nextHash : (hashImageB64(nextImageB64) ?? ''),
        sensitive: nextSensitive,
        seenNonces: capped,
      );
    }
    if (!validText(next)) return null;
    return ClipState(
      text: next,
      changedAt: nextChangedAt,
      changedC: nextChangedC,
      origin: origin.isEmpty ? 'mac' : origin,
      hasText: true,
      pending: false,
      kind: 'text',
      contentHash: nextHash.isNotEmpty ? nextHash : hashText(next),
      sensitive: nextSensitive,
      seenNonces: capped,
    );
  }

  /// Take the queued push for upload (clears pending). Null when idle.
  /// Carries HLC + hash + sensitivity so the receiver dedupes.
  Map<String, Object?>? takePending() {
    if (!pending || !hasText) return null;
    if (kind == 'image') {
      final map = <String, Object?>{
        'kind': 'image',
        'mime': mime,
        'image_b64': imageB64,
        'changed_at': changedAt,
        'changed_c': changedC,
        'origin': 'android',
      };
      if (filename.isNotEmpty) map['filename'] = filename;
      if (contentHash.isNotEmpty) map['content_hash'] = contentHash;
      if (sensitive) map['sensitive'] = true;
      return map;
    }
    final map = <String, Object?>{
      'kind': 'text',
      'text': text,
      'changed_at': changedAt,
      'changed_c': changedC,
      'origin': 'android',
    };
    if (contentHash.isNotEmpty) map['content_hash'] = contentHash;
    if (sensitive) map['sensitive'] = true;
    return map;
  }

  ClipState clearPending() => ClipState(
        text: text,
        changedAt: changedAt,
        changedC: changedC,
        origin: origin,
        hasText: hasText,
        kind: kind,
        mime: mime,
        imageB64: imageB64,
        filename: filename,
        contentHash: contentHash,
        sensitive: sensitive,
        seenNonces: seenNonces,
      );

  ClipState requeue() => ClipState(
        text: text,
        changedAt: changedAt,
        changedC: changedC,
        origin: origin,
        hasText: hasText,
        pending: true,
        kind: kind,
        mime: mime,
        imageB64: imageB64,
        filename: filename,
        contentHash: contentHash,
        sensitive: sensitive,
        seenNonces: seenNonces,
      );
}

/// Preview for list rows: first 200 chars for text. Pure.
String clipPreview(String s) {
  if (s.length <= 200) return s;
  return s.substring(0, 200);
}
