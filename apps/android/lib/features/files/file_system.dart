// SPDX-License-Identifier: AGPL-3.0-only

import 'dart:convert';
import 'dart:io';
import 'dart:async';

import 'package:crypto/crypto.dart';

import 'file_models.dart';

/// Sandboxed file system rooted at [rootPath]. All operations are confined
/// to the root via [isValidFilePath] checks and absolute join validation.
/// Mirrors Go's os.Root scoping for the Dart side.
/// Sequential reader for one phone -> Mac pull. Lets the pull loop reuse a
/// single file handle instead of open/seek/close per chunk.
abstract class PullReader {
  Future<List<int>> readNext(int length);
  Future<void> close();
}

abstract class FileSystem {
  Future<List<FileEntry>> list(String relPath);
  Future<void> mkdir(String relPath);
  Future<void> delete(String relPath);
  Future<void> rename(String from, String to);
  Future<int> size(String relPath);
  Future<List<int>> readChunk(String relPath, int offset, int length);
  Future<void> discardStaged(String relPath, String transferId);
  Future<void> writeChunk(String relPath, String transferId, int offset, int totalSize, List<int> data, bool isLast,
      {String policy = '', int sourceMtime = 0, String expectedSha256 = ''});

  /// Opens a sequential pull reader for [relPath]. The default implementation
  /// reuses [readChunk] with a running offset so existing fakes keep working;
  /// [AppFileSystem] overrides it with a single-handle reader.
  Future<PullReader> openPullReader(String relPath) async => _ChunkedPullReader(this, relPath);
}

/// Fallback pull reader built on [readChunk]: correct everywhere, one
/// open/seek/close per chunk. Used by fakes and MethodChannel backends that
/// do not override [FileSystem.openPullReader].
class _ChunkedPullReader implements PullReader {
  _ChunkedPullReader(this._fs, this._rel);

  final FileSystem _fs;
  final String _rel;
  int _offset = 0;

  @override
  Future<List<int>> readNext(int length) async {
    if (length <= 0) return const [];
    final chunk = await _fs.readChunk(_rel, _offset, length);
    _offset += chunk.length;
    return chunk;
  }

  @override
  Future<void> close() async {}
}

/// Single-handle pull reader over one [RandomAccessFile]. Sequential only:
/// the pull loop reads forward, so no seeks are needed after open.
class _RafPullReader implements PullReader {
  _RafPullReader(this._raf);

  final RandomAccessFile _raf;

  @override
  Future<List<int>> readNext(int length) async {
    if (length <= 0) return const [];
    return _raf.read(length);
  }

  @override
  Future<void> close() async {
    try {
      await _raf.close();
    } catch (_) {}
  }
}

/// Collects the single [Digest] from a chunked hash conversion for staged
/// uploads. Mirrors the pull-side sink in file_sync.dart; kept local so the
/// filesystem stays independent of the sync layer.
class _StagingDigestSink implements Sink<Digest> {
  Digest? value;

  @override
  void add(Digest data) {
    value = data;
  }

  @override
  void close() {}
}

/// App-private implementation using dart:io. For external storage a
/// MethodChannel implementation can be swapped in without changing callers.
class AppFileSystem implements FileSystem {
  AppFileSystem(this.rootPath);

  final String rootPath;

  /// Open staging handles keyed by transfer id. One open per in-flight
  /// upload instead of open/seek/write/flush/close per chunk: bursts of
  /// small files stop paying handle churn per chunk. Closed on commit,
  /// cancel, discard, or sha failure; abandoned mid-transfer handles close
  /// on the sender's file-cancel (or app restart clears the in-memory map
  /// and the next recovery sweep drops the stale part).
  final Map<String, RandomAccessFile> _stagingHandles = {};
  static const int _maxOpenStaging = 32;

  /// Incremental sha256 for staged uploads, fed only while chunk offsets
  /// arrive contiguously from zero. Lets the last chunk verify without
  /// re-reading the staged file. Any gap/duplicate marks the transfer dirty
  /// and falls back to the re-read hash.
  final Map<String, ByteConversionSink> _stagingHashIns = {};
  final Map<String, _StagingDigestSink> _stagingHashOuts = {};
  final Map<String, int> _stagingFedBytes = {};
  final Set<String> _stagingHashDirty = {};

  String _abs(String rel) {
    if (rel.trim().isEmpty) return rootPath;
    // rel is already sanitized via isValidFilePath.
    return '$rootPath/$rel';
  }

  String _canonical(String p) {
    try {
      final f = File(p);
      if (f.existsSync()) return f.resolveSymbolicLinksSync();
    } catch (_) {}
    try {
      final d = Directory(p);
      if (d.existsSync()) return d.resolveSymbolicLinksSync();
    } catch (_) {}
    try {
      return File(p).absolute.path;
    } catch (_) {
      return p;
    }
  }

  void _ensureWithinRoot(String abs) {
    final rootCanon = _canonical(rootPath);
    final targetCanon = _canonicalForCheck(abs);
    // Separator-aware check (defense in depth per filesystem.md safeJoin).
    final root = rootCanon.endsWith('/') ? rootCanon : '$rootCanon/';
    if (targetCanon != rootCanon && !targetCanon.startsWith(root)) {
      throw FileSystemException('path escapes root', abs);
    }
  }

  String _canonicalForCheck(String p) {
    final direct = _canonical(p);
    // _canonical resolves existing paths via symlinks. For staged part
    // files that do not exist yet (e.g. symlinked TMPDIR on macOS),
    // resolve the nearest existing ancestor and re-append the remainder
    // so the prefix check compares like-for-like.
    if (File(p).existsSync() || Directory(p).existsSync()) return direct;
    var cursor = p;
    final suffix = <String>[];
    // Walk up at most 8 levels to the nearest existing ancestor.
    for (var i = 0; i < 8; i++) {
      final slash = cursor.lastIndexOf('/');
      if (slash < 0) break;
      suffix.insert(0, cursor.substring(slash + 1));
      cursor = cursor.substring(0, slash);
      if (cursor.isEmpty) break;
      if (File(cursor).existsSync() || Directory(cursor).existsSync()) {
        final resolved = _canonical(cursor);
        if (suffix.isEmpty) return resolved;
        return '$resolved/${suffix.join('/')}';
      }
    }
    return direct;
  }

  @override
  Future<List<FileEntry>> list(String relPath) async {
    if (!isValidFilePath(relPath)) throw FileSystemException('invalid path', relPath);
    final abs = _abs(relPath);
    _ensureWithinRoot(abs);
    final dir = Directory(abs);
    if (!await dir.exists()) throw FileSystemException('not found', relPath);
    final stat = await dir.stat();
    if (stat.type != FileSystemEntityType.directory) {
      throw FileSystemException('not a directory', relPath);
    }
    final out = <FileEntry>[];
    await for (final ent in dir.list(followLinks: false)) {
      final name = ent.path.split('/').last;
      if (!isValidFileName(name)) continue; // denylist: NUL, /, \, control, . / ..
      if (name.contains('\x00')) continue;
      final st = await ent.stat();
      final isDir = st.type == FileSystemEntityType.directory;
      final relChild = relPath.trim().isEmpty ? name : '$relPath/$name';
      out.add(FileEntry(
        name: name,
        path: relChild,
        isDir: isDir,
        size: isDir ? 0 : st.size,
        modTime: st.modified.millisecondsSinceEpoch ~/ 1000,
      ));
      if (out.length >= kMaxFilesPerList) break;
    }
    out.sort((a, b) {
      if (a.isDir != b.isDir) return a.isDir ? -1 : 1;
      return a.name.toLowerCase().compareTo(b.name.toLowerCase());
    });
    return out;
  }

  @override
  Future<void> mkdir(String relPath) async {
    if (!isValidFilePath(relPath) || relPath.trim().isEmpty) {
      throw FileSystemException('invalid path', relPath);
    }
    final abs = _abs(relPath);
    _ensureWithinRoot(abs);
    await Directory(abs).create(recursive: false);
  }

  @override
  Future<void> delete(String relPath) async {
    if (!isValidFilePath(relPath) || relPath.trim().isEmpty) {
      throw FileSystemException('invalid path', relPath);
    }
    final abs = _abs(relPath);
    _ensureWithinRoot(abs);
    final type = await FileSystemEntity.type(abs, followLinks: false);
    if (type == FileSystemEntityType.directory) {
      await Directory(abs).delete(recursive: false);
    } else if (type == FileSystemEntityType.file) {
      await File(abs).delete();
    } else {
      throw FileSystemException('not found', relPath);
    }
  }

  @override
  Future<void> rename(String from, String to) async {
    if (!isValidFilePath(from) || from.trim().isEmpty) {
      throw FileSystemException('invalid path', from);
    }
    if (!isValidFilePath(to) || to.trim().isEmpty) {
      throw FileSystemException('invalid path', to);
    }
    if (from.trim() == to.trim()) throw FileSystemException('same path', from);
    final absFrom = _abs(from);
    final absTo = _abs(to);
    _ensureWithinRoot(absFrom);
    _ensureWithinRoot(absTo);
    final type = await FileSystemEntity.type(absFrom, followLinks: false);
    if (type == FileSystemEntityType.notFound) {
      throw FileSystemException('not found', from);
    }
    if (await FileSystemEntity.type(absTo, followLinks: false) != FileSystemEntityType.notFound) {
      throw FileSystemException('exists', to);
    }
    // Ensure parent exists.
    final parent = absTo.substring(0, absTo.lastIndexOf('/'));
    if (parent.isNotEmpty) {
      _ensureWithinRoot(parent);
    }
    if (type == FileSystemEntityType.directory) {
      await Directory(absFrom).rename(absTo);
    } else {
      await File(absFrom).rename(absTo);
    }
  }

  @override
  Future<int> size(String relPath) async {
    if (!isValidFilePath(relPath)) throw FileSystemException('invalid path', relPath);
    final abs = _abs(relPath);
    _ensureWithinRoot(abs);
    final f = File(abs);
    if (!await f.exists()) throw FileSystemException('not found', relPath);
    return (await f.stat()).size;
  }

  @override
  Future<List<int>> readChunk(String relPath, int offset, int length) async {
    if (!isValidFilePath(relPath)) throw FileSystemException('invalid path', relPath);
    final abs = _abs(relPath);
    _ensureWithinRoot(abs);
    final f = File(abs);
    final raf = await f.open(mode: FileMode.read);
    try {
      await raf.setPosition(offset);
      return await raf.read(length);
    } finally {
      await raf.close();
    }
  }

  /// Single-handle pull reader: one open for the whole transfer instead of
  /// open/seek/close per chunk. Closed by the caller in a finally.
  @override
  Future<PullReader> openPullReader(String relPath) async {
    if (!isValidFilePath(relPath)) throw FileSystemException('invalid path', relPath);
    final abs = _abs(relPath);
    _ensureWithinRoot(abs);
    final raf = await File(abs).open(mode: FileMode.read);
    return _RafPullReader(raf);
  }

  @override
  Future<void> writeChunk(String relPath, String transferId, int offset, int totalSize, List<int> data, bool isLast,
      {String policy = '', int sourceMtime = 0, String expectedSha256 = ''}) async {
    if (!isValidFilePath(relPath)) throw FileSystemException('invalid path', relPath);
    if (!isValidTransferID(transferId)) throw FileSystemException('invalid transfer', transferId);
    if (!isValidFilePolicy(policy)) throw FileSystemException('invalid policy', policy);
    if (expectedSha256.isNotEmpty && !isValidSha256(expectedSha256)) {
      throw FileSystemException('invalid sha', relPath);
    }
    final abs = _abs(relPath);
    final partPath = '$abs.part.$transferId';
    // One-time setup per transfer (paths validated, recovery swept, parent
    // created, handle opened): later chunks reuse the open handle and skip
    // straight to hashing and writing. The handle entry proves setup ran —
    // it is cleared on commit, cancel, discard, and write failure, so a
    // resumed transfer re-runs setup exactly once. File() is a cheap
    // handle (no I/O); the part file itself is only touched inside setup
    // and the commit below.
    final part = File(partPath);
    var raf = _stagingHandles[transferId];
    if (raf == null) {
      _ensureWithinRoot(abs);
      _ensureWithinRoot(partPath);
      // Crash recovery scoped to the target directory; the caller already
      // confines abs to the sandbox root, so a root-level upload never
      // scans outside it. Runs per transfer (not cached): the scan only
      // stats matching `.part.*`/`.replaced.*` siblings, so bursts stay
      // cheap while stale backups can never survive beside a live target.
      await _recoverReplaced(abs);
      await Directory(abs.substring(0, abs.lastIndexOf('/'))).create(recursive: true);
      // FileMode.append creates when missing and never truncates, so
      // earlier chunks of a multi-chunk upload survive; setPosition seeks
      // to the chunk offset before each write.
      while (_stagingHandles.length >= _maxOpenStaging) {
        final oldest = _stagingHandles.keys.first;
        await _closeStaging(oldest);
      }
      if (!await part.exists()) await part.create(recursive: true);
      raf = await part.open(mode: FileMode.append);
      _stagingHandles[transferId] = raf;
      _stagingFedBytes[transferId] = 0;
      final out = _StagingDigestSink();
      try {
        _stagingHashIns[transferId] = sha256.startChunkedConversion(out);
      } catch (_) {
        _stagingHashDirty.add(transferId);
      }
      _stagingHashOuts[transferId] = out;
    }
    // Feed the incremental hash only while offsets are contiguous; any
    // gap or duplicate retires the fast path and the last chunk falls back
    // to hashing the staged file.
    final fed = _stagingFedBytes[transferId] ?? 0;
    if (!_stagingHashDirty.contains(transferId) && offset == fed) {
      final sink = _stagingHashIns[transferId];
      if (sink != null) {
        try {
          if (data.isNotEmpty) sink.add(data);
          _stagingFedBytes[transferId] = fed + data.length;
        } catch (_) {
          _stagingHashDirty.add(transferId);
        }
      } else {
        _stagingHashDirty.add(transferId);
      }
    } else if (offset != fed) {
      _stagingHashDirty.add(transferId);
    }
    try {
      await raf.setPosition(offset);
      if (data.isNotEmpty) await raf.writeFrom(data);
      if (isLast) await raf.truncate(totalSize);
      await raf.flush();
    } catch (e) {
      await _closeStaging(transferId);
      rethrow;
    }
    if (isLast) {
      // Integrity before policy: a holed or corrupted stage never commits.
      // Fast path first (incremental hash); dirty or hash-less transfers
      // fall back to re-reading the staged file.
      if (expectedSha256.isNotEmpty) {
        final actual = await _hashStagedFast(part, transferId);
        if (actual != expectedSha256.toLowerCase()) {
          await _closeStaging(transferId);
          try {
            await part.delete();
          } catch (_) {}
          throw FileSystemException('sha mismatch', relPath);
        }
      } else {
        await _closeStaging(transferId);
      }
      final target = File(abs);
      if (!await target.exists()) {
        await part.rename(abs);
        return;
      }
      // Target exists: honor the sender policy (TOCTOU-safe, decided here
      // on the final chunk from staged bytes, never by deleting first).
      if (policy == kFilePolicyOverwrite) {
        await _replaceWithBackup(part, target, transferId);
        return;
      }
      if (policy == kFilePolicyIfNewer) {
        final st = await target.stat();
        final targetMtime = st.modified.millisecondsSinceEpoch ~/ 1000;
        if (isSourceNewer(sourceMtime, targetMtime, totalSize, st.size)) {
          await _replaceWithBackup(part, target, transferId);
        } else {
          await part.delete();
        }
        return;
      }
      // Legacy keep-both: suffix a copy, never overwrite.
      final alt = File('$abs-${transferId.substring(0, 6)}');
      await part.rename(alt.path);
    }
  }

  /// Closes a staging handle and drops its incremental hash state. Safe to
  /// call for unknown ids (no-op). Never throws.
  Future<void> _closeStaging(String transferId) async {
    final sink = _stagingHashIns.remove(transferId);
    _stagingHashOuts.remove(transferId);
    _stagingFedBytes.remove(transferId);
    _stagingHashDirty.remove(transferId);
    final raf = _stagingHandles.remove(transferId);
    if (sink != null) {
      try {
        sink.close();
      } catch (_) {}
    }
    if (raf != null) {
      try {
        await raf.close();
      } catch (_) {}
    }
  }

  /// Returns the staged file's sha256, preferring the incremental digest fed
  /// during writes. Falls back to re-reading the staged file when chunks
  /// arrived out of order or hashing failed mid-stream.
  Future<String> _hashStagedFast(File part, String transferId) async {
    if (!_stagingHashDirty.contains(transferId)) {
      final sink = _stagingHashIns[transferId];
      final out = _stagingHashOuts[transferId];
      if (sink != null && out != null) {
        String? fast;
        try {
          sink.close();
          fast = out.value?.toString();
        } catch (_) {
          fast = null;
        }
        if (fast != null) {
          await _closeStaging(transferId);
          return fast;
        }
      }
    }
    try {
      final digest = await sha256.bind(part.openRead()).first;
      return digest.toString();
    } finally {
      await _closeStaging(transferId);
    }
  }

  @override
  Future<void> discardStaged(String relPath, String transferId) async {
    if (!isValidFilePath(relPath) || relPath.trim().isEmpty) return;
    if (!isValidTransferID(transferId)) return;
    await _closeStaging(transferId);
    final abs = _abs(relPath);
    try {
      _ensureWithinRoot(abs);
    } catch (_) {
      return;
    }
    final partPath = '$abs.part.$transferId';
    try {
      _ensureWithinRoot(partPath);
    } catch (_) {
      return;
    }
    try {
      await File(partPath).delete();
    } catch (_) {}
  }

  /// Replace [target] with staged [part] via a backup rename so a crash
  /// between the two renames stays recoverable (see [_recoverReplaced]).
  /// On failure the backup is restored and the error rethrown.
  Future<void> _replaceWithBackup(File part, File target, String transferId) async {
    final backup = File('${target.path}.replaced.$transferId');
    if (await backup.exists()) {
      try {
        await backup.delete();
      } catch (_) {}
    }
    await target.rename(backup.path);
    try {
      await part.rename(target.path);
    } catch (_) {
      try {
        await backup.rename(target.path);
      } catch (_) {}
      rethrow;
    }
    try {
      await backup.delete();
    } catch (_) {}
  }

  /// Recover interrupted replaces beside [abs] without scanning storage:
  /// only the target directory is listed. A missing target with a *fresh*
  /// backup restores it (recent crash mid-replace); stale backups are
  /// dropped, never resurrected over a deliberate delete. Stale `.part.*`
  /// siblings from dead transfers are dropped too; live ones are fresh and
  /// left alone so concurrent transfers cannot harm each other.
  Future<void> _recoverReplaced(String abs) async {
    final slash = abs.lastIndexOf('/');
    final dirPath = slash >= 0 ? abs.substring(0, slash) : '.';
    final base = slash >= 0 ? abs.substring(slash + 1) : abs;
    List<FileSystemEntity> kids;
    try {
      kids = await Directory(dirPath).list(followLinks: false).toList();
    } catch (_) {
      return;
    }
    final now = DateTime.now();
    bool fresh(File f) {
      try {
        return now.difference(f.lastModifiedSync()) < kStalePartAge;
      } catch (_) {
        return false;
      }
    }
    final backups = <File>[];
    final staleParts = <File>[];
    for (final ent in kids) {
      if (ent is! File) continue;
      final name = ent.path.split('/').last;
      if (name.startsWith('$base.replaced.') && !fresh(ent)) backups.add(ent);
      if (name.startsWith('$base.part.') && !fresh(ent)) staleParts.add(ent);
    }
    // Fresh backups/parts belong to live transfers; leave them alone.
    var targetMissing = true;
    try {
      targetMissing = !await File(abs).exists();
    } catch (_) {}
    final freshBackups = <File>[];
    for (final ent in kids) {
      if (ent is! File) continue;
      final name = ent.path.split('/').last;
      if (name.startsWith('$base.replaced.') && fresh(ent)) freshBackups.add(ent);
    }
    if (targetMissing && freshBackups.isNotEmpty) {
      try {
        await freshBackups.first.rename(abs);
      } catch (_) {}
      freshBackups.removeAt(0);
    }
    for (final f in [...backups, ...staleParts, ...freshBackups]) {
      try {
        await f.delete();
      } catch (_) {}
    }
  }
}
