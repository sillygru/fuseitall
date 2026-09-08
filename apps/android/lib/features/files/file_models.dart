// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// File models mirroring packages/proto/files.json and packages/core/files.go.
// Pure helpers: sanitizers and size caps.

const kMaxFilePathLen = 1024;
const kMaxFileNameLen = 255;
const kMaxFilesPerList = 500;
const kMaxFileChunkRaw = 1 << 20; // 1 MiB
const kMaxFileTotalSize = 2 << 30; // 2 GiB
const kMinFileTransferIDLen = 16;
const kMaxFileTransferIDLen = 64;

/// One row in a directory listing.
class FileEntry {
  const FileEntry({
    required this.name,
    this.path = '',
    required this.isDir,
    this.size = 0,
    this.modTime = 0,
    this.mime,
  });

  final String name;
  final String path;
  final bool isDir;
  final int size;
  final int modTime;
  final String? mime;

  Map<String, Object?> toJson() => {
        'name': name,
        if (path.isNotEmpty) 'path': path,
        'is_dir': isDir,
        if (size != 0) 'size': size,
        if (modTime != 0) 'mod_time': modTime,
        if (mime != null && mime!.isNotEmpty) 'mime': mime,
      };

  static FileEntry? fromJson(Map<String, dynamic> j) {
    final name = j['name'];
    final isDir = j['is_dir'];
    if (name is! String || name.isEmpty) return null;
    if (isDir is! bool) return null;
    return FileEntry(
      name: name,
      path: j['path'] is String ? j['path'] as String : '',
      isDir: isDir,
      size: j['size'] is int ? j['size'] as int : 0,
      modTime: j['mod_time'] is int ? j['mod_time'] as int : 0,
      mime: j['mime'] is String ? j['mime'] as String : null,
    );
  }
}

/// Validate a sandboxed rel path. Empty means root.
/// Denylist mirror of core.SanitizeFilePath: NUL, \, control, traversal.
bool isValidFilePath(String s) {
  final trimmed = s.trim();
  if (trimmed.isEmpty) return true;
  if (trimmed.length > kMaxFilePathLen) return false;
  if (trimmed.contains('\\') || trimmed.contains('\x00')) return false;
  if (trimmed.startsWith('/')) return false;
  for (final r in trimmed.runes) {
    if (r == 0 || r < 0x20 || r == 0x7f) return false;
  }
  // Traversal via .. components: check raw trimmed before any Clean.
  final parts = trimmed.split('/');
  for (final p in parts) {
    if (p.isEmpty || p == '.' || p == '..') return false;
    if (p.length > kMaxFileNameLen) return false;
    if (p.contains('\\') || p.contains('\x00')) return false;
    for (final r in p.runes) {
      if (r == 0 || r < 0x20 || r == 0x7f) return false;
    }
  }
  if (trimmed.contains('..')) {
    // Reject any .. substring that would be traversal after Clean,
    // but allow legitimate "..." etc: check components strictly above.
    for (final p in parts) {
      if (p == '..') return false;
    }
  }
  return true;
}

bool isValidFileName(String s) {
  final t = s.trim();
  if (t.isEmpty || t.length > kMaxFileNameLen) return false;
  if (t.contains('/') || t.contains('\\') || t.contains('\x00')) return false;
  if (t == '.' || t == '..') return false;
  for (final r in t.runes) {
    if (r == 0 || r < 0x20 || r == 0x7f) return false;
  }
  return true;
}

bool isValidTransferID(String s) {
  final t = s.trim().toLowerCase();
  if (t.length < kMinFileTransferIDLen || t.length > kMaxFileTransferIDLen) return false;
  if (t.length % 2 != 0) return false;
  return RegExp(r'^[0-9a-f]+$').hasMatch(t);
}

bool isValidFileRename(String from, String to) {
  if (!isValidFilePath(from) || from.trim().isEmpty) return false;
  if (!isValidFilePath(to) || to.trim().isEmpty) return false;
  if (from.trim() == to.trim()) return false;
  return true;
}
