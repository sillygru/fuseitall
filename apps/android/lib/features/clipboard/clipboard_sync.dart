// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Latest-wins clipboard state with echo suppression. Mirrors core
// RemoteClipWins/SanitizeClipText/SanitizeClipImage: strictly newer changedAt
// wins, ties keep local, zero stamps never win, over-256KB text or over-5MiB
// image rejected. Contents never reach logs.
import 'dart:convert';

class ClipState {
  const ClipState({
    this.text = '',
    this.changedAt = 0,
    this.origin = '',
    this.hasText = false,
    this.pending = false,
    this.kind = 'text',
    this.mime = '',
    this.imageB64 = '',
    this.filename = '',
  });

  final String text;
  final int changedAt;
  final String origin;
  final bool hasText;
  final bool pending;
  final String kind;
  final String mime;
  final String imageB64;
  final String filename;

  static const maxLen = 256 * 1024;
  static const maxImageB64Len = 7100000;
  static const maxImageRaw = 5 * 1024 * 1024;
  static const autoImageRaw = 5 * 1024 * 1024;

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

  /// Record a local text copy: stamps origin android, arms pending. Identical
  /// text is a no-op. Over-long input returns null: fail closed.
  ClipState? setLocal(String next, int nowUnix) {
    if (!validText(next)) return null;
    if (hasText && kind == 'text' && text == next) return this;
    return ClipState(
      text: next,
      changedAt: nowUnix,
      origin: 'android',
      hasText: true,
      pending: true,
      kind: 'text',
    );
  }

  /// Record a local image copy. B64 must be valid base64 + whitelisted mime.
  ClipState? setLocalImage(String b64, String mimeStr, int nowUnix) => setLocalImageWithFilename(b64, mimeStr, '', nowUnix);

  ClipState? setLocalImageWithFilename(String b64, String mimeStr, String filename, int nowUnix) {
    final m = normalizeMime(mimeStr);
    if (m == null || !validImage(b64, m)) return null;
    final fn = sanitizeFilename(filename) ?? '';
    if (filename.trim().isNotEmpty && fn.isEmpty && sanitizeFilename(filename) == null) {
      // Bad filename dropped, keep image.
    }
    if (hasText && kind == 'image' && imageB64 == b64 && mime == m && this.filename == fn) return this;
    return ClipState(
      changedAt: nowUnix,
      origin: 'android',
      hasText: true,
      pending: true,
      kind: 'image',
      mime: m,
      imageB64: b64,
      filename: fn,
    );
  }

  /// Adopt an incoming push when strictly newer. Returns null when dropped
  /// (stale, echo of our own origin, or invalid). Adopted state disarms
  /// pending so it never bounces back.
  ClipState? applyRemote({
    required String next,
    required int nextChangedAt,
    required String nextOrigin,
    String nextKind = 'text',
    String nextMime = '',
    String nextImageB64 = '',
    String nextFilename = '',
  }) {
    if (nextChangedAt <= 0 || nextChangedAt <= changedAt) return null;
    final origin = nextOrigin.trim().toLowerCase() == 'macos'
        ? 'mac'
        : nextOrigin.trim().toLowerCase();
    if (origin == 'android') return null;
    final kind = normalizeKind(nextKind);
    if (kind == 'image') {
      final m = normalizeMime(nextMime);
      if (m == null || !validImage(nextImageB64, m)) return null;
      final fn = sanitizeFilename(nextFilename) ?? '';
      return ClipState(
        changedAt: nextChangedAt,
        origin: origin.isEmpty ? 'mac' : origin,
        hasText: true,
        pending: false,
        kind: 'image',
        mime: m,
        imageB64: nextImageB64,
        filename: fn,
      );
    }
    if (!validText(next)) return null;
    return ClipState(
      text: next,
      changedAt: nextChangedAt,
      origin: origin.isEmpty ? 'mac' : origin,
      hasText: true,
      pending: false,
      kind: 'text',
    );
  }

  /// Take the queued push for upload (clears pending). Null when idle.
  Map<String, Object?>? takePending() {
    if (!pending || !hasText) return null;
    if (kind == 'image') {
      final map = <String, Object?>{
        'kind': 'image',
        'mime': mime,
        'image_b64': imageB64,
        'changed_at': changedAt,
        'origin': 'android',
      };
      if (filename.isNotEmpty) map['filename'] = filename;
      return map;
    }
    return {'kind': 'text', 'text': text, 'changed_at': changedAt, 'origin': 'android'};
  }

  ClipState clearPending() => ClipState(
        text: text,
        changedAt: changedAt,
        origin: origin,
        hasText: hasText,
        kind: kind,
        mime: mime,
        imageB64: imageB64,
        filename: filename,
      );

  ClipState requeue() => ClipState(
        text: text,
        changedAt: changedAt,
        origin: origin,
        hasText: hasText,
        pending: true,
        kind: kind,
        mime: mime,
        imageB64: imageB64,
        filename: filename,
      );
}

/// Preview for list rows: first 200 chars for text. Pure.
String clipPreview(String s) {
  if (s.length <= 200) return s;
  return s.substring(0, 200);
}
