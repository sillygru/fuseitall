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
    // Phone-side sends stay on the legacy 1 MiB stride; the receiver below
    // accepts both strides so Mac-side 4 MiB uploads validate.
    this.chunkSize = kLegacyFileChunkRaw,
  });

  final FileSystem fs;
  final Future<void> Function(String type, Map<String, Object?> payload) sendFeature;
  final int chunkSize;

  /// Received chunk indexes per in-flight upload transfer, so a sender retry
  /// can ask file-stat-req and resend only the missing tail. Bounded: the
  /// oldest entry is dropped past the cap. Cleared on commit, cancel, and
  /// sha failure (the stage is gone then, so resume restarts from zero).
  final Map<String, _UploadProgress> _progress = {};
  static const int _maxTrackedUploads = 64;

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
      case 'file-rename':
        await _handleRename(payload);
        return true;
      case 'file-chunk':
        await _handleChunk(payload);
        return true;
      case 'file-pull-req':
        await _handlePull(payload);
        return true;
      case 'file-cancel':
        await _handleCancel(payload);
        return true;
      case 'file-stat-req':
        await _handleStat(payload);
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
        await _sendListResp(
          reqId,
          [],
          error: 'All files access needed — open FuseItAll on phone, tap Allow all files (Settings > Apps > FuseItAll).',
          errorCode: 'permission_denied',
          permission: 'files',
        );
      } else {
        await _sendListResp(reqId, [], error: _shortErr(e));
      }
    }
  }

  Future<void> _sendListResp(String reqId, List<Map<String, Object?>> entries, {String? error, String? errorCode, String? permission}) async {
    final payload = <String, Object?>{
      'req_id': reqId,
      'entries': entries,
      if (error != null && error.isNotEmpty) 'error': error,
      if (errorCode != null && errorCode.isNotEmpty) 'error_code': errorCode,
      if (permission != null && permission.isNotEmpty) 'permission': permission,
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

  Future<void> _handleRename(Map<String, dynamic> p) async {
    final from = (p['from'] as String?)?.trim() ?? '';
    final to = (p['to'] as String?)?.trim() ?? '';
    if (!isValidFileRename(from, to)) return;
    // Same-directory rename only (friendliness, no cross-folder move via rename).
    final fromDir = from.contains('/') ? from.substring(0, from.lastIndexOf('/')) : '';
    final toDir = to.contains('/') ? to.substring(0, to.lastIndexOf('/')) : '';
    if (fromDir != toDir) return;
    try {
      await fs.rename(from, to);
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
    final policy = (p['policy'] as String?) ?? '';
    final sourceMtime = p['source_mtime'] is int ? p['source_mtime'] as int : (p['source_mtime'] is num ? (p['source_mtime'] as num).toInt() : 0);
    final sha256hex = (p['sha256'] as String?) ?? '';
    if (!isValidTransferID(transferId) || !isValidFilePath(path) || path.isEmpty) return;
    if (!isValidFilePolicy(policy)) return;
    if (sourceMtime < 0) return;
    if (totalSize < 0 || totalSize > kMaxFileTotalSize) return;
    // Strict shape mirror of core sanitizeFileChunkLengths: total_chunks must
    // match the legacy 1 MiB or the 4 MiB stride, offsets follow that stride.
    // Fail closed.
    final stride = chunkStrideFor(totalSize, totalChunks);
    if (stride == null) return;
    if (chunkIndex < 0 || chunkIndex >= totalChunks) return;
    if (offset < 0 || offset > totalSize) return;
    if (offset != chunkIndex * stride) return;
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
    final expectedRaw = isLast ? totalSize - offset : stride;
    if (raw.length != expectedRaw) return;
    if (sha256hex.isNotEmpty) {
      if (!isValidSha256(sha256hex)) return;
      if (!isLast) return; // sha rides the last chunk only
    }
    _noteChunk(transferId, totalChunks, chunkIndex);
    try {
      await fs.writeChunk(path, transferId, offset, totalSize, raw, isLast,
          policy: policy, sourceMtime: sourceMtime, expectedSha256: sha256hex);
    } catch (e) {
      // Surface the real reason (sha mismatch, path escapes root, disk full)
      // instead of a generic string so the Mac can show a resumable error
      // and the sender can retry from the phone's missing offset.
      _progress.remove(transferId);
      if (isLast) await _sendAck(transferId, false, _shortErr(e));
      return;
    }
    if (isLast) _progress.remove(transferId);
    // Confirm delivery so the sender can mark the transfer verified instead
    // of sent-and-hoped. Old senders ignore unknown types; the send is
    // best-effort and never fails the commit itself.
    if (isLast) await _sendAck(transferId, true);
  }

  Future<void> _sendAck(String transferId, bool ok, [String? error]) async {
    final e = (error ?? '').trim();
    final payload = <String, Object?>{
      'transfer_id': transferId,
      'ok': ok,
      if (e.isNotEmpty) 'error': e.length > 200 ? e.substring(0, 200) : e,
    };
    try {
      await sendFeature('file-ack', payload);
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

  Future<void> _handleCancel(Map<String, dynamic> p) async {
    final transferId = (p['transfer_id'] as String?) ?? '';
    final path = (p['path'] as String?) ?? '';
    if (!isValidTransferID(transferId)) return;
    _progress.remove(transferId);
    if (path.isEmpty) return;
    try {
      await fs.discardStaged(path, transferId);
    } catch (_) {}
  }

  void _noteChunk(String transferId, int totalChunks, int chunkIndex) {
    final seen = _progress[transferId];
    if (seen != null && seen.totalChunks == totalChunks) {
      seen.received.add(chunkIndex);
      return;
    }
    // New transfer (or a reused id with a new shape): start tracking fresh.
    while (_progress.length >= _maxTrackedUploads) {
      _progress.remove(_progress.keys.first);
    }
    _progress[transferId] = _UploadProgress(totalChunks, {chunkIndex});
  }

  Future<void> _handleStat(Map<String, dynamic> p) async {
    final transferId = (p['transfer_id'] as String?) ?? '';
    if (!isValidTransferID(transferId)) return;
    final seen = _progress[transferId];
    final payload = <String, Object?>{
      'transfer_id': transferId,
      if (seen == null) ...{
        'next_chunk': 0,
        'total_chunks': 1,
        'error': 'unknown transfer',
      } else ...{
        'next_chunk': seen.nextMissing(),
        'total_chunks': seen.totalChunks,
      },
    };
    try {
      await sendFeature('file-stat-resp', payload);
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

/// Received-chunk ledger for one in-flight upload transfer.
class _UploadProgress {
  _UploadProgress(this.totalChunks, this.received);

  final int totalChunks;
  final Set<int> received;

  /// Smallest missing chunk index (== totalChunks when nothing is missing).
  int nextMissing() {
    for (var i = 0; i < totalChunks; i++) {
      if (!received.contains(i)) return i;
    }
    return totalChunks;
  }
}
