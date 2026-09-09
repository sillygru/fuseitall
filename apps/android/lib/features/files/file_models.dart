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
// Largest raw chunk a receiver accepts (4 MiB, ~5.6 MiB base64 under the
// 8 MiB body cap). Senders use it only when the peer advertises
// files-large-chunk; otherwise kLegacyFileChunkRaw.
const kMaxFileChunkRaw = 4 << 20;
// Original 1 MiB stride. Old peers send and accept only this size; new
// receivers accept both strides. Phone-side sends stay on legacy for now.
const kLegacyFileChunkRaw = 1 << 20;
const kMaxFileTotalSize = 8 << 30; // 8 GiB, mirrors core MaxFileTotalSize
const kMinFileTransferIDLen = 16;
const kMaxFileTransferIDLen = 64;

/// Staged `.part.*` files and `.replaced.*` backups older than this with no
/// live transfer are treated as crash leftovers and swept on next use.
const kStalePartAge = Duration(minutes: 30);

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

final RegExp _sha256Hex = RegExp(r'^[0-9a-fA-F]{64}$');

/// Validate a hex sha256 (64 hex chars), mirroring core's sha shape check.
bool isValidSha256(String s) => _sha256Hex.hasMatch(s);

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

/// Wire conflict policies for file-chunk. Only overwrite and if_newer ride
/// the wire; skip/keep_both/stop are sender-side only.
const kFilePolicyOverwrite = 'overwrite';
const kFilePolicyIfNewer = 'if_newer';
const kFilePolicySkip = 'skip';
const kFilePolicyKeepBoth = 'keep_both';
const kFilePolicyStop = 'stop';

/// Validate a wire policy. Empty means legacy keep-both.
bool isValidFilePolicy(String s) {
  return s.isEmpty || s == kFilePolicyOverwrite || s == kFilePolicyIfNewer;
}

/// How many chunks of [chunkSize] cover [totalSize] (one for empty files).
int totalChunksForSize(int totalSize, int chunkSize) {
  if (chunkSize <= 0) chunkSize = kLegacyFileChunkRaw;
  var n = (totalSize + chunkSize - 1) ~/ chunkSize;
  if (totalSize == 0) n = 1;
  return n;
}

/// The raw stride a transfer uses, derived from total_size/total_chunks
/// matching the legacy 1 MiB or the 4 MiB size. Null when neither matches.
/// Single-chunk transfers report legacy (stride is irrelevant at offset 0).
int? chunkStrideFor(int totalSize, int totalChunks) {
  if (totalChunks < 1) return null;
  final legacy = totalChunksForSize(totalSize, kLegacyFileChunkRaw);
  final max = totalChunksForSize(totalSize, kMaxFileChunkRaw);
  if (totalChunks == legacy) return kLegacyFileChunkRaw;
  if (totalChunks == max) return kMaxFileChunkRaw;
  return null;
}

/// Mirror of core.IsSourceNewer: mtime seconds first, size tiebreak.
/// Equal mtime plus equal size counts as same (skip); equal mtime plus
/// different size counts as newer. Pure.
bool isSourceNewer(int sourceMtime, int targetMtime, int sourceSize, int targetSize) {
  if (sourceMtime != targetMtime) return sourceMtime > targetMtime;
  return sourceSize != targetSize;
}

/// Mirror of core.KeepBothName: Finder-style numbering for a colliding
/// remote path given existing names. Pure.
String keepBothName(String remotePath, Set<String> existing) {
  if (!existing.contains(remotePath)) return remotePath;
  String base = remotePath;
  String ext = '';
  final slash = remotePath.lastIndexOf('/');
  final file = slash >= 0 ? remotePath.substring(slash + 1) : remotePath;
  final dir = slash >= 0 ? remotePath.substring(0, slash) : '';
  final dot = file.lastIndexOf('.');
  if (dot > 0) {
    ext = file.substring(dot);
    base = '${dir.isEmpty ? '' : '$dir/'}${file.substring(0, dot)}';
  }
  var i = 2;
  while (true) {
    final candidate = '$base ($i)$ext';
    if (!existing.contains(candidate)) return candidate;
    i++;
  }
}
