// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'dart:io';
import 'dart:async';

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
  Future<void> writeChunk(String relPath, String transferId, int offset, int totalSize, List<int> data, bool isLast);
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
    final targetCanon = _canonical(abs);
    // Separator-aware check (defense in depth per filesystem.md safeJoin).
    final root = rootCanon.endsWith('/') ? rootCanon : '$rootCanon/';
    if (targetCanon != rootCanon && !targetCanon.startsWith(root)) {
      throw FileSystemException('path escapes root', abs);
    }
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
  Future<void> writeChunk(String relPath, String transferId, int offset, int totalSize, List<int> data, bool isLast) async {
    if (!isValidFilePath(relPath)) throw FileSystemException('invalid path', relPath);
    if (!isValidTransferID(transferId)) throw FileSystemException('invalid transfer', transferId);
    final abs = _abs(relPath);
    _ensureWithinRoot(abs);
    final partPath = '$abs.part.$transferId';
    _ensureWithinRoot(partPath);
    await Directory(abs.substring(0, abs.lastIndexOf('/'))).create(recursive: true);
    final part = File(partPath);
    final raf = await part.open(mode: FileMode.write);
    try {
      await raf.setPosition(offset);
      if (data.isNotEmpty) await raf.writeFrom(data);
      if (isLast) await raf.truncate(totalSize);
    } finally {
      await raf.close();
    }
    if (isLast) {
      // Atomic-ish: move part into place.
      final target = File(abs);
      if (await target.exists()) {
        final alt = File('$abs-${transferId.substring(0, 6)}');
        await part.rename(alt.path);
      } else {
        await part.rename(abs);
      }
    }
  }
}
