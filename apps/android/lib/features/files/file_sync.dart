// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'file_models.dart';
import 'file_system.dart';

/// Handles inbound /files messages and drives outbound responses/chunks.
/// Thin: no UI, no transport fallback — caller supplies [sendFeature] which
/// already handles TOFU + fallback via PhoneTransport.
class FileSync {
  FileSync({
    required this.fs,
    required this.sendFeature,
    this.chunkSize = kMaxFileChunkRaw,
  });

  final FileSystem fs;
  final Future<void> Function(String type, Map<String, Object?> payload) sendFeature;
  final int chunkSize;

  /// Entry point from PingPage's feat channel. Returns true if handled.
  Future<bool> handleEvent(Map<String, dynamic> envelope) async {
    final type = envelope['type'] as String?;
    final payload = envelope['payload'];
    if (type == null || payload is! Map<String, dynamic>) return false;
    switch (type) {
      case 'file-list':
        await _handleList(payload);
        return true;
      case 'file-mkdir':
        await _handleMkdir(payload);
        return true;
      case 'file-delete':
        await _handleDelete(payload);
        return true;
      case 'file-chunk':
        await _handleChunk(payload);
        return true;
      case 'file-pull-req':
        await _handlePull(payload);
        return true;
      default:
        return false;
    }
  }

  Future<void> _handleList(Map<String, dynamic> p) async {
    final reqId = (p['req_id'] as String?)?.trim() ?? '';
    final path = (p['path'] as String?)?.trim() ?? '';
    if (reqId.isEmpty) return;
    if (!isValidFilePath(path)) {
      await _sendListResp(reqId, [], error: 'invalid path');
      return;
    }
    try {
      final entries = await fs.list(path);
      final jsonEntries = entries.map((e) => e.toJson()).toList();
      await _sendListResp(reqId, jsonEntries);
    } catch (e) {
      final msg = e.toString().toLowerCase();
      if (msg.contains('permission') || msg.contains('denied') || msg.contains('eperm')) {
        await _sendListResp(reqId, [], error: 'All files access needed — open FuseItAll on phone, tap Allow all files (Settings > Apps > FuseItAll).');
      } else {
        await _sendListResp(reqId, [], error: _shortErr(e));
      }
    }
  }

  Future<void> _sendListResp(String reqId, List<Map<String, Object?>> entries, {String? error}) async {
    final payload = <String, Object?>{
      'req_id': reqId,
      'entries': entries,
      if (error != null && error.isNotEmpty) 'error': error,
    };
    try {
      await sendFeature('file-list-resp', payload);
    } catch (_) {}
  }

  Future<void> _handleMkdir(Map<String, dynamic> p) async {
    final path = (p['path'] as String?)?.trim() ?? '';
    if (!isValidFilePath(path) || path.isEmpty) return;
    try {
      await fs.mkdir(path);
    } catch (_) {}
  }

  Future<void> _handleDelete(Map<String, dynamic> p) async {
    final path = (p['path'] as String?)?.trim() ?? '';
    if (!isValidFilePath(path) || path.isEmpty) return;
    try {
      await fs.delete(path);
    } catch (_) {}
  }

  Future<void> _handleChunk(Map<String, dynamic> p) async {
    final transferId = (p['transfer_id'] as String?) ?? '';
    final path = (p['path'] as String?) ?? '';
    final offset = p['offset'] is int ? p['offset'] as int : (p['offset'] is num ? (p['offset'] as num).toInt() : 0);
    final totalSize = p['total_size'] is int ? p['total_size'] as int : 0;
    final chunkIndex = p['chunk_index'] is int ? p['chunk_index'] as int : 0;
    final totalChunks = p['total_chunks'] is int ? p['total_chunks'] as int : 0;
    final dataB64 = (p['data_b64'] as String?) ?? '';
    if (!isValidTransferID(transferId) || !isValidFilePath(path) || path.isEmpty) return;
    if (totalSize < 0 || totalSize > kMaxFileTotalSize) return;
    List<int> raw = const [];
    if (dataB64.isNotEmpty) {
      try {
        raw = base64Decode(dataB64);
      } catch (_) {
        return;
      }
      if (raw.length > kMaxFileChunkRaw) return;
    }
    final isLast = chunkIndex == totalChunks - 1;
    try {
      await fs.writeChunk(path, transferId, offset, totalSize, raw, isLast);
    } catch (_) {}
  }

  Future<void> _handlePull(Map<String, dynamic> p) async {
    final path = (p['path'] as String?) ?? '';
    final transferId = (p['transfer_id'] as String?)?.trim() ?? _newTransferID();
    if (!isValidFilePath(path) || path.isEmpty) return;
    if (!isValidTransferID(transferId)) return;
    int totalSize;
    try {
      totalSize = await fs.size(path);
    } catch (_) {
      return;
    }
    if (totalSize > kMaxFileTotalSize) return;
    int totalChunks = (totalSize + chunkSize - 1) ~/ chunkSize;
    if (totalSize == 0) totalChunks = 1;
    for (int idx = 0; idx < totalChunks; idx++) {
      final offset = idx * chunkSize;
      final len = idx == totalChunks - 1 ? totalSize - offset : chunkSize;
      List<int> chunk;
      try {
        chunk = len == 0 ? const [] : await fs.readChunk(path, offset, len);
      } catch (_) {
        return;
      }
      final b64 = chunk.isEmpty ? '' : base64Encode(chunk);
      final payload = <String, Object?>{
        'transfer_id': transferId,
        'path': path,
        'offset': offset,
        'total_size': totalSize,
        'chunk_index': idx,
        'total_chunks': totalChunks,
        'data_b64': b64,
      };
      if (idx == totalChunks - 1) {
        // Optional sha256 on last chunk omitted for simplicity (could compute).
      }
      try {
        await sendFeature('file-chunk', payload);
      } catch (_) {
        return;
      }
      // Small yield to avoid starving UI.
      await Future<void>.delayed(const Duration(milliseconds: 2));
    }
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
