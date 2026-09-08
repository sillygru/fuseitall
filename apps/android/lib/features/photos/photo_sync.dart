// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'dart:async';
import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';

import 'photo_models.dart';
import 'photo_store.dart';

/// Handles inbound /photos messages and drives outbound responses/chunks.
/// Isolated from file domain: own req_id space, own transfer IDs, own staging.
class PhotoSync {
  PhotoSync({
    required this.store,
    required this.sendFeature,
    this.chunkSize = kMaxPhotoChunkRaw,
  });

  final PhotoStore store;
  final Future<void> Function(String type, Map<String, Object?> payload) sendFeature;
  final int chunkSize;

  Future<bool> handleEvent(Map<String, dynamic> envelope) async {
    final type = envelope['type'] as String?;
    final payload = envelope['payload'];
    if (type == null || payload is! Map<String, dynamic>) return false;
    switch (type) {
      case 'photo-list':
        await _handleList(payload);
        return true;
      case 'photo-thumb-req':
        await _handleThumb(payload);
        return true;
      case 'photo-pull-req':
        await _handlePull(payload);
        return true;
      case 'photo-chunk':
        // Inbound chunks are Mac->phone uploads (not used v1); ack only.
        return true;
      case 'photo-delete':
        await _handleDelete(payload);
        return true;
      default:
        return false;
    }
  }

  Future<void> _handleList(Map<String, dynamic> p) async {
    final reqId = (p['req_id'] as String?)?.trim() ?? '';
    final cursor = (p['cursor'] as String?)?.trim() ?? '';
    var limit = p['limit'] is int ? p['limit'] as int : kDefaultPhotoLimit;
    if (limit < 1 || limit > kMaxPhotosPerList) limit = kDefaultPhotoLimit;
    if (!isValidPhotoReqID(reqId) || !isValidPhotoCursor(cursor)) {
      await _sendListResp(reqId, [], '', error: 'invalid arg', errorCode: 'invalid_arg', permission: 'photos');
      return;
    }
    try {
      final res = await store.list(cursor: cursor, limit: limit);
      final jsonEntries = res.entries.map((e) => e.toJson()).toList();
      await _sendListResp(reqId, jsonEntries, res.nextCursor);
    } catch (e) {
      final msg = e.toString().toLowerCase();
      if (msg.contains('permission') || msg.contains('denied') || msg.contains('eperm') || msg.contains('securityexception')) {
        await _sendListResp(
          reqId,
          [],
          '',
          error: 'Photos access needed — open FuseItAll on phone, tap Allow photos (Settings > Apps > FuseItAll > Permissions > Photos).',
          errorCode: 'permission_denied',
          permission: 'photos',
        );
      } else {
        await _sendListResp(reqId, [], '', error: _shortErr(e), errorCode: 'internal', permission: '');
      }
    }
  }

  Future<void> _sendListResp(String reqId, List<Map<String, Object?>> entries, String nextCursor, {String? error, String? errorCode, String? permission}) async {
    final payload = <String, Object?>{
      'req_id': reqId,
      'entries': entries,
      'next_cursor': nextCursor,
      if (error != null && error.isNotEmpty) 'error': error,
      if (errorCode != null && errorCode.isNotEmpty) 'error_code': errorCode,
      if (permission != null && permission.isNotEmpty) 'permission': permission,
    };
    try {
      await sendFeature('photo-list-resp', payload);
    } catch (_) {}
  }

  Future<void> _handleThumb(Map<String, dynamic> p) async {
    final reqId = (p['req_id'] as String?)?.trim() ?? '';
    final photoId = (p['photo_id'] as String?)?.trim() ?? '';
    final thumbSize = p['thumb_size'] is int ? p['thumb_size'] as int : 256;
    if (!isValidPhotoReqID(reqId) || !isValidPhotoID(photoId)) return;
    final clampedSize = thumbSize.clamp(kMinPhotoThumbSize, kMaxPhotoThumbSize);
    try {
      final res = await store.thumb(photoId, clampedSize);
      final payload = <String, Object?>{
        'req_id': reqId,
        'photo_id': photoId,
        'mime': res.mime,
        'data_b64': res.dataB64,
      };
      await sendFeature('photo-thumb-resp', payload);
    } catch (e) {
      final msg = e.toString().toLowerCase();
      final isPerm = msg.contains('permission') || msg.contains('denied') || msg.contains('securityexception');
      await sendFeature('photo-thumb-resp', {
        'req_id': reqId,
        'photo_id': photoId,
        if (isPerm) 'error': 'Photos access needed — open FuseItAll on phone, tap Allow photos.',
        if (isPerm) 'error_code': 'permission_denied',
        if (isPerm) 'permission': 'photos',
        if (!isPerm) 'error': _shortErr(e),
      });
    }
  }

  Future<void> _handlePull(Map<String, dynamic> p) async {
    final photoId = (p['photo_id'] as String?)?.trim() ?? '';
    final transferId = (p['transfer_id'] as String?)?.trim() ?? _newTransferID();
    if (!isValidPhotoID(photoId) || !isValidPhotoTransferID(transferId)) return;
    int totalSize;
    try {
      totalSize = await store.size(photoId);
    } catch (e) {
      final msg = e.toString().toLowerCase();
      if (msg.contains('permission') || msg.contains('denied')) {
        // No direct error lane for pull; caller will timeout. Future: send photo-error.
      }
      return;
    }
    if (totalSize < 0 || totalSize > kMaxPhotoChunkRaw * 2048) return; // 2 GiB via file cap reuse
    int totalChunks = (totalSize + chunkSize - 1) ~/ chunkSize;
    if (totalSize == 0) totalChunks = 1;
    for (int idx = 0; idx < totalChunks; idx++) {
      final offset = idx * chunkSize;
      final len = idx == totalChunks - 1 ? totalSize - offset : chunkSize;
      Uint8List chunk;
      try {
        chunk = len == 0 ? Uint8List(0) : await store.readChunk(photoId, offset, len);
      } catch (_) {
        return;
      }
      final b64 = chunk.isEmpty ? '' : base64Encode(chunk);
      final payload = <String, Object?>{
        'transfer_id': transferId,
        'photo_id': photoId,
        'offset': offset,
        'total_size': totalSize,
        'chunk_index': idx,
        'total_chunks': totalChunks,
        'data_b64': b64,
      };
      try {
        await sendFeature('photo-chunk', payload);
      } catch (_) {
        return;
      }
      await Future<void>.delayed(const Duration(milliseconds: 2));
    }
  }

  Future<void> _handleDelete(Map<String, dynamic> p) async {
    final reqId = (p['req_id'] as String?)?.trim() ?? '';
    final idsRaw = p['photo_ids'];
    final ids = <String>[];
    if (idsRaw is List) {
      for (final v in idsRaw) {
        if (v is String && isValidPhotoID(v)) ids.add(v.trim());
      }
    }
    if (!isValidPhotoReqID(reqId) || ids.isEmpty || ids.length > kMaxPhotoDeleteBatch) {
      await _sendDeleteResp(reqId, [], error: 'invalid arg', errorCode: 'invalid_arg');
      return;
    }
    try {
      final results = await store.delete(ids);
      final jsonResults = results.map((r) => r.toJson()).toList();
      await _sendDeleteResp(reqId, jsonResults);
    } catch (e) {
      final msg = e.toString().toLowerCase();
      final isPerm = msg.contains('permission') || msg.contains('denied') || msg.contains('securityexception');
      await _sendDeleteResp(
        reqId,
        ids.map((id) => {'photo_id': id, 'ok': false, 'error': _shortErr(e), 'error_code': isPerm ? 'permission_denied' : 'internal'}).toList(),
        error: isPerm ? 'Photos access needed — allow photos.' : _shortErr(e),
        errorCode: isPerm ? 'permission_denied' : 'internal',
        permission: isPerm ? 'photos' : '',
      );
    }
  }

  Future<void> _sendDeleteResp(String reqId, List<Map<String, Object?>> results, {String? error, String? errorCode, String? permission}) async {
    final payload = <String, Object?>{
      'req_id': reqId,
      'results': results,
      if (error != null && error.isNotEmpty) 'error': error,
      if (errorCode != null && errorCode.isNotEmpty) 'error_code': errorCode,
      if (permission != null && permission.isNotEmpty) 'permission': permission,
    };
    try {
      await sendFeature('photo-delete-resp', payload);
    } catch (_) {}
  }

  String _newTransferID() {
    final rnd = Random.secure();
    final bytes = List<int>.generate(16, (_) => rnd.nextInt(256));
    return bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
  }

  String _shortErr(Object e) {
    final m = e.toString();
    if (m.length > 200) return m.substring(0, 200);
    return m;
  }
}
