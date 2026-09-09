// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'dart:io';
import 'dart:async';

import 'package:crypto/crypto.dart';

import 'file_models.dart';

/// Sandboxed file system rooted at [rootPath]. All operations are confined
/// to the root via [isValidFilePath] checks and absolute join validation.
/// Mirrors Go's os.Root scoping for the Dart side.
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
}

/// App-private implementation using dart:io. For external storage a
/// MethodChannel implementation can be swapped in without changing callers.
class AppFileSystem implements FileSystem {
  AppFileSystem(this.rootPath);

  final String rootPath;

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
    _ensureWithinRoot(abs);
    final partPath = '$abs.part.$transferId';
    _ensureWithinRoot(partPath);
    // Crash recovery only needs to run once per transfer (first chunk).
    // Scope it to the target directory; the caller already confines abs to
    // the sandbox root, so a root-level upload never scans outside it.
    if (offset == 0) await _recoverReplaced(abs);
    await Directory(abs.substring(0, abs.lastIndexOf('/'))).create(recursive: true);
    final part = File(partPath);
    // Random-access append: FileMode.write truncates on every open, which
    // wipes earlier chunks of a multi-chunk upload. append creates when
    // missing and never truncates; setPosition then seeks to the chunk
    // offset before writing.
    if (!await part.exists()) await part.create(recursive: true);
    final raf = await part.open(mode: FileMode.append);
    try {
      await raf.setPosition(offset);
      if (data.isNotEmpty) await raf.writeFrom(data);
      if (isLast) await raf.truncate(totalSize);
      await raf.flush();
    } finally {
      await raf.close();
    }
    if (isLast) {
      // Integrity before policy: a holed or corrupted stage never commits.
      if (expectedSha256.isNotEmpty) {
        final actual = await _hashStaged(part);
        if (actual != expectedSha256.toLowerCase()) {
          try {
            await part.delete();
          } catch (_) {}
          throw FileSystemException('sha mismatch', relPath);
        }
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

  Future<String> _hashStaged(File part) async {
    final digest = await sha256.bind(part.openRead()).first;
    return digest.toString();
  }

  @override
  Future<void> discardStaged(String relPath, String transferId) async {
    if (!isValidFilePath(relPath) || relPath.trim().isEmpty) return;
    if (!isValidTransferID(transferId)) return;
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
