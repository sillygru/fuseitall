// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'package:crypto/crypto.dart';

import 'file_models.dart';
import 'file_system.dart';

/// Handles inbound /files messages and drives outbound responses/chunks.
/// Thin: no UI, no transport fallback — caller supplies [sendFeature] which
/// already handles TOFU + fallback via PhoneTransport.
class FileSync {
  FileSync({
    required this.fs,
    required this.sendFeature,
    // Fallback stride when the pull request carries no peer version (old
    // callers, tests). Live pulls negotiate per envelope via
    // [chunkSizeForPeer]; a non-standard injected value is honored as-is so
    // tests can use tiny strides. The receive path below accepts both
    // strides regardless.
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

  /// Pull transfers aborted by a Mac file-cancel. Checked per chunk so a
  /// cancelled download stops sending promptly instead of draining the file.
  /// Bounded like [_progress]; entries clear when their pull ends.
  final Set<String> _cancelledPulls = {};
  static const int _maxTrackedCancels = 64;

  /// Bounded concurrent pulls (phone -> Mac). Each pull streams chunks over
  /// the single WebSocket; the Mac reassembles by transfer_id+offset, so
  /// interleaved pulls are safe. Four streams saturate a LAN for music
  /// folders while bounding read-ahead memory. Extra pull requests queue
  /// instead of stacking unbounded readers.
  static const int _maxConcurrentPulls = 4;
  int _activePulls = 0;
  final List<_PendingPull> _pullQueue = [];

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
        _schedulePull(payload, peerCaps: _peerCaps(envelope), peerBuild: _peerBuild(envelope));
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

  /// Bounded concurrent chunk commits across transfers. The Mac streams up
  /// to 8 files at once over one socket; without this, every chunk would
  /// await the previous file's disk flush+ack. Chunks of one transfer stay
  /// strictly FIFO (per-id chain, preserving the offset==fed fast path);
  /// different transfers overlap kernel I/O. CPU (base64/json) stays
  /// single-isolate serial — this hides disk latency, not CPU.
  static const int _maxConcurrentChunkTransfers = 8;
  static const int _maxQueuedChunks = 64;
  final Map<String, Future<void>> _chunkTails = {};
  final List<_QueuedChunk> _chunkQueue = [];

  /// Cancel generations for in-flight uploads. A file-cancel bumps the
  /// generation; commits from older generations discard their stage and
  /// stay silent instead of acking bytes the sender already abandoned.
  final Map<String, int> _uploadEpoch = {};

  /// Committed uploads (transfer id -> total chunks), so late duplicates
  /// racing an ack are dropped instead of staging orphan parts nobody will
  /// commit, and stat answers "nothing missing" instead of "unknown" (which
  /// would make the sender pointlessly restart a finished transfer).
  /// Bounded like [_progress]; shapes are compared so a reused id with a
  /// new shape still starts fresh.
  final Map<String, int> _doneUploads = {};
  static const int _maxDoneUploads = 64;

  /// Schedules one chunk commit without blocking other transfers. Returns
  /// when this chunk committed (or was safely dropped/cancelled), so
  /// awaiting callers keep today's per-chunk semantics while concurrent
  /// callers overlap. Never throws: malformed chunks are dropped, commit
  /// failures nack via the normal path, and queue overflow drops self-heal
  /// through the sender's stat/resume (the ledger only ever records
  /// committed bytes).
  Future<void> _handleChunk(Map<String, dynamic> p) async {
    final c = _validateChunk(p);
    if (c == null) return;
    final doneTotal = _doneUploads[c.transferId];
    if (doneTotal != null) {
      // Already committed: same shape means a duplicate racing the ack
      // (drop it); a new shape under a reused id starts fresh.
      if (doneTotal == c.totalChunks) return;
      _doneUploads.remove(c.transferId);
    }
    final epoch = _uploadEpoch[c.transferId] ?? 0;
    final tail = _chunkTails[c.transferId];
    if (tail != null) {
      final next = tail.then((_) => _commitChunk(c, epoch));
      _chunkTails[c.transferId] = next;
      return next;
    }
    if (_chunkTails.length < _maxConcurrentChunkTransfers) {
      final done = Completer<void>();
      _chunkTails[c.transferId] = _runChunkChain(c.transferId, c, epoch, done);
      return done.future;
    }
    if (_chunkQueue.length >= _maxQueuedChunks) return;
    final done = Completer<void>();
    _chunkQueue.add(_QueuedChunk(c, epoch, done));
    return done.future;
  }

  /// Runs one transfer's FIFO chain, then drains its queued chunks in order.
  Future<void> _runChunkChain(String id, _ValidatedChunk first, int epoch, Completer<void> firstDone) async {
    try {
      await _commitAndComplete(first, epoch, firstDone);
      for (;;) {
        final i = _chunkQueue.indexWhere((q) => q.chunk.transferId == id);
        if (i < 0) break;
        final q = _chunkQueue.removeAt(i);
        await _commitAndComplete(q.chunk, q.epoch, q.done);
      }
    } finally {
      _chunkTails.remove(id);
      _pumpChunkQueue();
    }
  }

  Future<void> _commitAndComplete(_ValidatedChunk c, int epoch, Completer<void> done) async {
    try {
      await _commitChunk(c, epoch);
    } finally {
      if (!done.isCompleted) done.complete();
    }
  }

  void _pumpChunkQueue() {
    while (_chunkTails.length < _maxConcurrentChunkTransfers && _chunkQueue.isNotEmpty) {
      final i = _chunkQueue.indexWhere((q) => !_chunkTails.containsKey(q.chunk.transferId));
      if (i < 0) break;
      final q = _chunkQueue.removeAt(i);
      if ((_uploadEpoch[q.chunk.transferId] ?? 0) != q.epoch) {
        if (!q.done.isCompleted) q.done.complete();
        continue;
      }
      final id = q.chunk.transferId;
      _chunkTails[id] = _runChunkChain(id, q.chunk, q.epoch, q.done);
    }
  }

  /// Pure-ish validation for one chunk envelope: shapes, stride math, and a
  /// single base64 decode. Synchronous (no awaits) so scheduling stays
  /// atomic with the stat ledger. Null means drop fail-closed.
  _ValidatedChunk? _validateChunk(Map<String, dynamic> p) {
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
    if (!isValidTransferID(transferId) || !isValidFilePath(path) || path.isEmpty) return null;
    if (!isValidFilePolicy(policy)) return null;
    if (sourceMtime < 0) return null;
    if (totalSize < 0 || totalSize > kMaxFileTotalSize) return null;
    // Strict shape mirror of core sanitizeFileChunkLengths: total_chunks must
    // match the legacy 1 MiB or the 4 MiB stride, offsets follow that stride.
    // Fail closed.
    final stride = chunkStrideFor(totalSize, totalChunks);
    if (stride == null) return null;
    if (chunkIndex < 0 || chunkIndex >= totalChunks) return null;
    if (offset < 0 || offset > totalSize) return null;
    if (offset != chunkIndex * stride) return null;
    List<int> raw = const [];
    if (dataB64.isNotEmpty) {
      try {
        raw = base64Decode(dataB64);
      } catch (_) {
        return null;
      }
      if (raw.length > kMaxFileChunkRaw) return null;
    }
    final isLast = chunkIndex == totalChunks - 1;
    final expectedRaw = isLast ? totalSize - offset : stride;
    if (raw.length != expectedRaw) return null;
    if (sha256hex.isNotEmpty) {
      if (!isValidSha256(sha256hex)) return null;
      if (!isLast) return null; // sha rides the last chunk only
    }
    return _ValidatedChunk(transferId, path, offset, totalSize, raw, isLast,
        policy, sourceMtime, sha256hex, totalChunks, chunkIndex);
  }

  /// Commits one validated chunk: ledger, staged write, delivery ack. Ledger
  /// records committed bytes only, so stat/resume answers stay truthful even
  /// when queued chunks are dropped or cancelled mid-flight. Never throws.
  Future<void> _commitChunk(_ValidatedChunk c, int epoch) async {
    if ((_uploadEpoch[c.transferId] ?? 0) != epoch) return;
    _noteChunk(c.transferId, c.totalChunks, c.chunkIndex);
    try {
      await fs.writeChunk(c.path, c.transferId, c.offset, c.totalSize, c.raw, c.isLast,
          policy: c.policy, sourceMtime: c.sourceMtime, expectedSha256: c.sha256hex);
    } catch (e) {
      // Surface the real reason (sha mismatch, path escapes root, disk full)
      // instead of a generic string so the Mac can show a resumable error
      // and the sender can retry from the phone's missing offset.
      _progress.remove(c.transferId);
      if (c.isLast) await _sendAck(c.transferId, false, _shortErr(e));
      return;
    }
    if ((_uploadEpoch[c.transferId] ?? 0) != epoch) {
      // Cancelled mid-write: drop the resurrected stage, stay silent.
      _progress.remove(c.transferId);
      try {
        await fs.discardStaged(c.path, c.transferId);
      } catch (_) {}
      return;
    }
    if (c.isLast) _progress.remove(c.transferId);
    // Record the commit before acking so duplicates racing the ack drop
    // instead of staging orphan parts, and stat keeps answering "complete".
    if (c.isLast) _noteDone(c.transferId, c.totalChunks);
    // Confirm delivery so the sender can mark the transfer verified instead
    // of sent-and-hoped. Old senders ignore unknown types; the send is
    // best-effort and never fails the commit itself.
    if (c.isLast) await _sendAck(c.transferId, true);
  }

  void _noteDone(String transferId, int totalChunks) {
    while (_doneUploads.length >= _maxDoneUploads) {
      _doneUploads.remove(_doneUploads.keys.first);
    }
    _doneUploads[transferId] = totalChunks;
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

  /// Peer capabilities stamped on the inbound envelope (Mac file-pull-req
  /// envelopes carry the Mac's featureCaps, including files-large-chunk).
  List<String> _peerCaps(Map<String, dynamic> envelope) {
    final caps = envelope['capabilities'];
    if (caps is! List) return const [];
    return caps.whereType<String>().toList();
  }

  /// Peer build stamped on the inbound envelope sender. 0 when absent (old
  /// peers): fail closed to the legacy stride.
  int _peerBuild(Map<String, dynamic> envelope) {
    final sender = envelope['sender'];
    if (sender is! Map) return 0;
    final build = sender['app_build'];
    if (build is int) return build;
    if (build is num) return build.toInt();
    return 0;
  }

  /// Stride for one pull: the negotiated size, unless tests injected a
  /// non-standard stride (neither legacy nor max), which is honored as-is.
  int _pullStride(List<String> peerCaps, int peerBuild) {
    if (chunkSize != kLegacyFileChunkRaw && chunkSize != kMaxFileChunkRaw && chunkSize > 0) {
      return chunkSize;
    }
    return chunkSizeForPeer(peerCaps, peerBuild);
  }

  /// One pull chunk send with a single retry for transient WS flaps. The
  /// transport already fans out over hosts; this only covers a blip
  /// mid-transfer. Throws after the retry so the pull aborts fail-closed.
  Future<void> _sendPullChunk(Map<String, Object?> payload) async {
    for (var attempt = 0; attempt < 2; attempt++) {
      if (attempt > 0) await Future<void>.delayed(Duration(milliseconds: 200 * attempt * attempt));
      try {
        await sendFeature('file-chunk', payload);
        return;
      } catch (_) {
        if (attempt == 1) rethrow;
      }
    }
  }

  /// Schedules one pull without blocking the event channel: the caller
  /// returns immediately so later envelopes (including cancels and other
  /// pulls) keep flowing. Push-only, no timers or polling — queued pulls
  /// drain as active ones finish.
  void _schedulePull(Map<String, dynamic> p, {List<String> peerCaps = const [], int peerBuild = 0}) {
    final path = (p['path'] as String?) ?? '';
    final transferId = (p['transfer_id'] as String?)?.trim() ?? _newTransferID();
    if (!isValidFilePath(path) || path.isEmpty) return;
    if (!isValidTransferID(transferId)) return;
    if (_cancelledPulls.remove(transferId)) return;
    // Drop queued duplicates behind a live or queued pull with the same id.
    for (final q in _pullQueue) {
      if (q.transferId == transferId) return;
    }
    if (_activePulls >= _maxConcurrentPulls) {
      while (_pullQueue.length >= _maxConcurrentPulls * 4) {
        _pullQueue.removeAt(0);
      }
      _pullQueue.add(_PendingPull(path, transferId, peerCaps, peerBuild));
      return;
    }
    _activePulls++;
    unawaited(_runPull(path, transferId, peerCaps, peerBuild));
  }

  Future<void> _runPull(String path, String transferId, List<String> peerCaps, int peerBuild) async {
    try {
      await _handlePull(_PendingPull(path, transferId, peerCaps, peerBuild).toPayload(), peerCaps: peerCaps, peerBuild: peerBuild);
    } finally {
      _activePulls--;
      _drainPullQueue();
    }
  }

  void _drainPullQueue() {
    while (_activePulls < _maxConcurrentPulls && _pullQueue.isNotEmpty) {
      final next = _pullQueue.removeAt(0);
      if (_cancelledPulls.remove(next.transferId)) continue;
      _activePulls++;
      unawaited(_runPull(next.path, next.transferId, next.peerCaps, next.peerBuild));
    }
  }
  Future<void> _handlePull(Map<String, dynamic> p, {List<String> peerCaps = const [], int peerBuild = 0}) async {
    final path = (p['path'] as String?) ?? '';
    final transferId = (p['transfer_id'] as String?)?.trim() ?? _newTransferID();
    if (!isValidFilePath(path) || path.isEmpty) return;
    if (!isValidTransferID(transferId)) return;
    if (_cancelledPulls.remove(transferId)) return;
    final stride = _pullStride(peerCaps, peerBuild);
    int totalSize;
    try {
      totalSize = await fs.size(path);
    } catch (_) {
      return;
    }
    if (totalSize < 0 || totalSize > kMaxFileTotalSize) return;
    final totalChunks = totalChunksForSize(totalSize, stride);
    PullReader reader;
    try {
      reader = await fs.openPullReader(path);
    } catch (_) {
      return;
    }
    final hashOut = _SingleDigestSink();
    final hashIn = sha256.startChunkedConversion(hashOut);
    var hashOpen = true;
    try {
      for (int idx = 0; idx < totalChunks; idx++) {
        if (_cancelledPulls.contains(transferId)) return;
        final offset = idx * stride;
        final len = idx == totalChunks - 1 ? totalSize - offset : stride;
        List<int> chunk;
        try {
          chunk = await reader.readNext(len);
        } catch (_) {
          return;
        }
        // Short reads mean the file changed under us: fail closed rather
        // than shipping a holed chunk the Mac would reject anyway.
        if (chunk.length != len) return;
        if (hashOpen) {
          try {
            if (chunk.isNotEmpty) hashIn.add(chunk);
          } catch (_) {}
        }
        final payload = <String, Object?>{
          'transfer_id': transferId,
          'path': path,
          'offset': offset,
          'total_size': totalSize,
          'chunk_index': idx,
          'total_chunks': totalChunks,
          'data_b64': chunk.isEmpty ? '' : base64Encode(chunk),
        };
        if (idx == totalChunks - 1 && hashOpen) {
          final sha = _closeHash(hashIn, hashOut);
          hashOpen = false;
          if (sha != null) payload['sha256'] = sha;
        }
        try {
          await _sendPullChunk(payload);
        } catch (_) {
          return;
        }
        // Yield to the UI loop without the old 2ms-per-chunk tax.
        await Future<void>.delayed(Duration.zero);
      }
    } finally {
      _cancelledPulls.remove(transferId);
      if (hashOpen) {
        try {
          hashIn.close();
        } catch (_) {}
      }
      try {
        await reader.close();
      } catch (_) {}
    }
  }

  /// Closes the incremental pull hash and returns its hex, or null when
  /// hashing failed (the Mac then falls back to its size check).
  String? _closeHash(ByteConversionSink hashIn, _SingleDigestSink hashOut) {
    try {
      hashIn.close();
      return hashOut.value?.toString();
    } catch (_) {
      return null;
    }
  }

  Future<void> _handleCancel(Map<String, dynamic> p) async {
    final transferId = (p['transfer_id'] as String?) ?? '';
    final path = (p['path'] as String?) ?? '';
    if (!isValidTransferID(transferId)) return;
    _progress.remove(transferId);
    // Retire queued chunks (completing their waiters) and invalidate
    // in-flight commits via the epoch: post-write checks discard instead
    // of acking.
    _uploadEpoch[transferId] = ((_uploadEpoch[transferId] ?? 0) + 1);
    for (final q in _chunkQueue.where((q) => q.chunk.transferId == transferId).toList()) {
      if (!q.done.isCompleted) q.done.complete();
    }
    _pullQueue.removeWhere((q) => q.transferId == transferId);
    _notePullCancel(transferId);
    if (path.isEmpty) return;
    try {
      await fs.discardStaged(path, transferId);
    } catch (_) {}
  }

  /// Remembers a pull abort so an in-flight [_handlePull] loop stops before
  /// its next chunk. Bounded: the oldest entry drops past the cap.
  void _notePullCancel(String transferId) {
    if (_cancelledPulls.contains(transferId)) return;
    while (_cancelledPulls.length >= _maxTrackedCancels) {
      _cancelledPulls.remove(_cancelledPulls.first);
    }
    _cancelledPulls.add(transferId);
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
    final doneTotal = _doneUploads[transferId];
    final seen = _progress[transferId];
    final payload = <String, Object?>{
      'transfer_id': transferId,
      if (doneTotal != null && seen == null) ...{
        // Committed earlier (the ack may have been lost): nothing missing.
        'next_chunk': doneTotal,
        'total_chunks': doneTotal,
      } else if (seen == null) ...{
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

/// One queued phone -> Mac pull. Carries the negotiated peer stride inputs
/// so queued pulls send the same chunk shape as immediate ones.
class _PendingPull {
  _PendingPull(this.path, this.transferId, this.peerCaps, this.peerBuild);

  final String path;
  final String transferId;
  final List<String> peerCaps;
  final int peerBuild;

  Map<String, dynamic> toPayload() => {'path': path, 'transfer_id': transferId};
}

/// One validated upload chunk with its bytes already decoded, ready to
/// commit. Validation is synchronous; the commit is async and chained
/// per transfer.
class _ValidatedChunk {
  _ValidatedChunk(
    this.transferId,
    this.path,
    this.offset,
    this.totalSize,
    this.raw,
    this.isLast,
    this.policy,
    this.sourceMtime,
    this.sha256hex,
    this.totalChunks,
    this.chunkIndex,
  );

  final String transferId;
  final String path;
  final int offset;
  final int totalSize;
  final List<int> raw;
  final bool isLast;
  final String policy;
  final int sourceMtime;
  final String sha256hex;
  final int totalChunks;
  final int chunkIndex;
}

/// One queued chunk: validated bytes plus the cancel generation it was
/// scheduled under and the waiter its scheduling caller awaits.
class _QueuedChunk {
  _QueuedChunk(this.chunk, this.epoch, this.done);

  final _ValidatedChunk chunk;
  final int epoch;
  final Completer<void> done;
}

/// Collects the single [Digest] from a chunked hash conversion. The crypto
/// package no longer exports a general accumulator, and a pull needs exactly
/// one digest, so this tiny sink is all the pull hash requires.
class _SingleDigestSink implements Sink<Digest> {
  Digest? value;

  @override
  void add(Digest data) {
    value = data;
  }

  @override
  void close() {}
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
