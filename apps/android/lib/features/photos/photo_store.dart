// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'dart:async';
import 'dart:convert';

import 'package:flutter/services.dart';

import 'photo_models.dart';

/// Thin adapter to the native photo library (MediaStore). No business logic:
/// callers supply cursor/limit and receive entries; thumb/delete/pull are
/// delegated to the platform.
abstract class PhotoStore {
  /// List photos paged by cursor (empty = first page). Returns (entries, nextCursor).
  Future<({List<PhotoEntry> entries, String nextCursor})> list({
    required String cursor,
    required int limit,
  });

  /// Fetch one thumbnail as base64 (size is long edge px, clamped server-side).
  Future<({String mime, String dataB64})> thumb(String photoId, int thumbSize);

  /// Get total size of full-res photo bytes.
  Future<int> size(String photoId);

  /// Read chunk of full-res bytes.
  Future<Uint8List> readChunk(String photoId, int offset, int len);

  /// Delete batch: returns per-item results (partial success is normal).
  Future<List<PhotoDeleteItem>> delete(List<String> photoIds);
}

class PhotoDeleteItem {
  final String photoId;
  final bool ok;
  final String? error;
  final String? errorCode;
  const PhotoDeleteItem({required this.photoId, required this.ok, this.error, this.errorCode});
  Map<String, Object?> toJson() => {
        'photo_id': photoId,
        'ok': ok,
        if (error != null) 'error': error,
        if (errorCode != null) 'error_code': errorCode,
      };
}

/// MethodChannel implementation: talks to MainActivity's fuseitall/photos channel.
/// Testable via injected channel.
class MethodChannelPhotoStore implements PhotoStore {
  MethodChannelPhotoStore({MethodChannel? channel}) : _channel = channel ?? const MethodChannel('fuseitall/photos');

  final MethodChannel _channel;

  @override
  Future<({List<PhotoEntry> entries, String nextCursor})> list({required String cursor, required int limit}) async {
    final res = await _channel.invokeMethod<Map<dynamic, dynamic>>('queryPhotos', {
      'cursor': cursor,
      'limit': limit,
    });
    if (res == null) return (entries: <PhotoEntry>[], nextCursor: '');
    final entriesRaw = res['entries'] as List? ?? [];
    final entries = <PhotoEntry>[];
    for (final e in entriesRaw) {
      if (e is Map) {
        final m = Map<String, dynamic>.fromEntries(
          e.entries.map((en) => MapEntry(en.key.toString(), en.value)),
        );
        final pe = PhotoEntry.fromJson(m);
        if (pe != null) entries.add(pe);
      }
    }
    final nextCursor = (res['next_cursor'] as String?) ?? '';
    return (entries: entries, nextCursor: nextCursor);
  }

  @override
  Future<({String mime, String dataB64})> thumb(String photoId, int thumbSize) async {
    final res = await _channel.invokeMethod<Map<dynamic, dynamic>>('getThumb', {
      'photo_id': photoId,
      'thumb_size': thumbSize,
    });
    if (res == null) return (mime: '', dataB64: '');
    return (mime: (res['mime'] as String?) ?? '', dataB64: (res['data_b64'] as String?) ?? '');
  }

  @override
  Future<int> size(String photoId) async {
    final v = await _channel.invokeMethod<int>('getPhotoSize', {'photo_id': photoId});
    return v ?? 0;
  }

  @override
  Future<Uint8List> readChunk(String photoId, int offset, int len) async {
    final b64 = await _channel.invokeMethod<String>('readPhotoChunk', {
      'photo_id': photoId,
      'offset': offset,
      'len': len,
    });
    if (b64 == null || b64.isEmpty) return Uint8List(0);
    return base64Decode(b64);
  }

  @override
  Future<List<PhotoDeleteItem>> delete(List<String> photoIds) async {
    final res = await _channel.invokeMethod<Map<dynamic, dynamic>>('deletePhotos', {'photo_ids': photoIds});
    if (res == null) return photoIds.map((id) => PhotoDeleteItem(photoId: id, ok: false, error: 'unknown', errorCode: 'internal')).toList();
    final resultsRaw = res['results'] as List? ?? [];
    final out = <PhotoDeleteItem>[];
    for (final r in resultsRaw) {
      if (r is Map) {
        final m = r.map((k, v) => MapEntry(k.toString(), v));
        out.add(PhotoDeleteItem(
          photoId: (m['photo_id'] as String?) ?? '',
          ok: (m['ok'] as bool?) ?? false,
          error: m['error'] as String?,
          errorCode: m['error_code'] as String?,
        ));
      }
    }
    return out;
  }
}
