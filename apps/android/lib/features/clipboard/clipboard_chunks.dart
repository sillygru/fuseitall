// SPDX-License-Identifier: AGPL-3.0-only

// Chunked large-image helpers (mirrors core.ClipManifest/ClipChunk).
// Inline pushes stay single-message (<=5 MiB raw); large images (>5 MiB,
// <=25 MiB) ride manifest + 1 MiB chunks, sha256-verified. In-memory only.
import 'dart:convert';

import 'package:crypto/crypto.dart';
import 'package:flutter/services.dart';

class ClipChunkSession {
  ClipChunkSession({
    required this.sessionId,
    required this.mime,
    required this.filename,
    required this.totalRaw,
    required this.totalChunks,
    required this.changedAt,
    required this.changedC,
    required this.origin,
    required this.contentHash,
    required this.sensitive,
  });

  final String sessionId;
  final String mime;
  final String filename;
  final int totalRaw;
  final int totalChunks;
  final int changedAt;
  final int changedC;
  final String origin;
  final String contentHash;
  final bool sensitive;
  final Map<int, List<int>> parts = {};
}

class ClipChunkHub {
  final Map<String, ClipChunkSession> _sessions = {};

  bool begin(Map<String, dynamic> manifest) {
    final sid = '${manifest['session_id'] ?? ''}'.trim();
    if (sid.isEmpty || sid.length > 64) return false;
    if (_sessions.containsKey(sid)) return false;
    final totalRaw = (manifest['total_raw'] as num?)?.toInt() ?? 0;
    final totalChunks = (manifest['total_chunks'] as num?)?.toInt() ?? 0;
    if (totalRaw <= 5 * 1024 * 1024 || totalRaw > 25 * 1024 * 1024) return false;
    if (totalChunks < 2 || totalChunks > 32) return false;
    _sessions[sid] = ClipChunkSession(
      sessionId: sid,
      mime: '${manifest['mime'] ?? ''}',
      filename: '${manifest['filename'] ?? ''}',
      totalRaw: totalRaw,
      totalChunks: totalChunks,
      changedAt: (manifest['changed_at'] as num?)?.toInt() ?? 0,
      changedC: (manifest['changed_c'] as num?)?.toInt() ?? 0,
      origin: '${manifest['origin'] ?? ''}',
      contentHash: '${manifest['content_hash'] ?? ''}',
      sensitive: manifest['sensitive'] == true,
    );
    if (_sessions.length > 8) {
      _sessions.remove(_sessions.keys.first);
    }
    return true;
  }

  /// Stores one chunk; returns the reassembled payload map when complete and
  /// verified, else null. Corrupt sessions drop fail-closed.
  Map<String, Object?>? add(Map<String, dynamic> chunk) {
    final sid = '${chunk['session_id'] ?? ''}'.trim();
    final sess = _sessions[sid];
    if (sess == null) return null;
    final idx = (chunk['chunk_index'] as num?)?.toInt() ?? -1;
    final total = (chunk['total_chunks'] as num?)?.toInt() ?? 0;
    if (idx < 0 || idx >= sess.totalChunks || total != sess.totalChunks) {
      _sessions.remove(sid);
      return null;
    }
    if (sess.parts.containsKey(idx)) return null;
    List<int> raw;
    try {
      raw = base64Decode('${chunk['data_b64'] ?? ''}');
    } catch (_) {
      return null;
    }
    if (raw.isEmpty || raw.length > 1024 * 1024) return null;
    sess.parts[idx] = raw;
    if (sess.parts.length != sess.totalChunks) return null;
    final assembled = <int>[];
    for (var i = 0; i < sess.totalChunks; i++) {
      final part = sess.parts[i];
      if (part == null) {
        _sessions.remove(sid);
        return null;
      }
      assembled.addAll(part);
    }
    _sessions.remove(sid);
    if (assembled.length != sess.totalRaw) return null;
    final hash = sha256.convert(assembled).toString();
    if (hash != sess.contentHash.toLowerCase()) return null;
    return {
      'kind': 'image',
      'mime': sess.mime,
      'image_b64': base64Encode(assembled),
      'filename': sess.filename,
      'changed_at': sess.changedAt,
      'changed_c': sess.changedC,
      'origin': sess.origin,
      'content_hash': sess.contentHash,
      if (sess.sensitive) 'sensitive': true,
    };
  }
}

/// Splits raw bytes into 1 MiB chunks with offsets. Pure.
List<(int index, int offset, List<int> bytes)> splitClipRaw(List<int> raw) {
  const stride = 1024 * 1024;
  final out = <(int, int, List<int>)>[];
  var idx = 0;
  for (var off = 0; off < raw.length; off += stride, idx++) {
    var end = off + stride;
    if (end > raw.length) end = raw.length;
    out.add((idx, off, raw.sublist(off, end)));
  }
  return out;
}

/// Reads a large clipboard image via chunked MethodChannel (stays under
/// Binder ~1 MiB per call). Returns (bytes, mime, filename, sensitive) or
/// null when no image. Never throws.
Future<({List<int> bytes, String mime, String filename, bool sensitive})?> readLargeClipboardImage() async {
  try {
    const ch = MethodChannel('fuseitall/clipboard');
    final meta = await ch.invokeMethod('readLargeImageMeta');
    if (meta is! Map) return null;
    final total = (meta['total'] as num?)?.toInt() ?? 0;
    if (total <= 0 || total > 25 * 1024 * 1024) return null;
    final mime = '${meta['mime'] ?? 'image/png'}';
    final filename = '${meta['filename'] ?? ''}';
    final sensitive = meta['sensitive'] == true;
    final out = <int>[];
    var offset = 0;
    while (offset < total) {
      final want = (total - offset).clamp(0, 1024 * 1024);
      final b64 = await ch.invokeMethod('readLargeImageChunk', {'offset': offset, 'len': want});
      if (b64 is! String || b64.isEmpty) return null;
      List<int> part;
      try {
        part = base64Decode(b64);
      } catch (_) {
        return null;
      }
      if (part.isEmpty) return null;
      out.addAll(part);
      offset += part.length;
      if (part.length < want) break;
    }
    if (out.isEmpty || out.length != total) return null;
    return (bytes: out, mime: mime, filename: filename, sensitive: sensitive);
  } catch (_) {
    return null;
  }
}

/// Writes bytes to the system clipboard via chunked MethodChannel (stays
/// under Binder). Returns true on success. Never throws.
Future<bool> writeLargeClipboardImage(List<int> bytes, String mime, String filename, {bool sensitive = false}) async {
  try {
    const ch = MethodChannel('fuseitall/clipboard');
    await ch.invokeMethod('beginLargeImageWrite', {
      'mime': mime,
      'filename': filename,
      'total': bytes.length,
      'sensitive': sensitive,
    });
    const stride = 1024 * 1024;
    for (var off = 0; off < bytes.length; off += stride) {
      var end = off + stride;
      if (end > bytes.length) end = bytes.length;
      await ch.invokeMethod('appendLargeImageChunk', {
        'data_b64': base64Encode(bytes.sublist(off, end)),
      });
    }
    await ch.invokeMethod('finishLargeImageWrite');
    return true;
  } catch (_) {
    return false;
  }
}
