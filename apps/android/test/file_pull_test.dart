// SPDX-License-Identifier: AGPL-3.0-only

import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/files/file_models.dart';
import 'package:fuseitall/features/files/file_sync.dart';
import 'package:fuseitall/features/files/file_system.dart';

void main() {
  group('pull stride negotiation', () {
    test('peerSupportsLargeChunks needs caps and build >= 11', () {
      expect(peerSupportsLargeChunks([kFilesLargeChunkCapability], 11), isTrue);
      expect(peerSupportsLargeChunks([kFilesLargeChunkCapability], 10), isFalse);
      expect(peerSupportsLargeChunks(['files'], 11), isFalse);
      expect(peerSupportsLargeChunks([kFilesLargeChunkCapability], 0), isFalse);
      expect(peerSupportsLargeChunks([], 11), isFalse);
    });

    test('chunkSizeForPeer mirrors core.FileChunkSizeForPeer', () {
      expect(chunkSizeForPeer([kFilesLargeChunkCapability], 11), kMaxFileChunkRaw);
      expect(chunkSizeForPeer([kFilesLargeChunkCapability], 10), kLegacyFileChunkRaw);
      expect(chunkSizeForPeer([], 11), kLegacyFileChunkRaw);
    });
  });

  group('FileSync._handlePull via handleEvent', () {
    late Directory tmp;
    setUp(() {
      tmp = Directory.systemTemp.createTempSync('fuse-pull-');
    });
    tearDown(() {
      try {
        tmp.deleteSync(recursive: true);
      } catch (_) {}
    });

    Future<void> seed(String rel, List<int> bytes) async {
      final f = File('${tmp.path}/$rel');
      await f.parent.create(recursive: true);
      await f.writeAsBytes(bytes);
    }

    Map<String, dynamic> pullEnv(String transferId, String path,
        {List<String> caps = const [], int build = 0}) {
      return {
        'type': 'file-pull-req',
        'capabilities': caps,
        'sender': {'platform': 'darwin', 'app_build': build, 'min_peer_build': 1},
        'payload': {'transfer_id': transferId, 'path': path},
      };
    }

    /// Pulls now schedule in the background (up to 4 concurrent) so the
    /// event channel never blocks behind a large file. Tests wait for the
    /// expected chunk count with a timeout instead of assuming synchronous
    /// sends.
    Future<void> waitForChunks(List<Map<String, Object?>> sent, int count) async {
      final deadline = DateTime.now().add(const Duration(seconds: 10));
      while (DateTime.now().isBefore(deadline)) {
        if (sent.where((m) => m['type'] == 'file-chunk').length >= count) return;
        await Future<void>.delayed(const Duration(milliseconds: 10));
      }
    }

    test('large peer sends fewer chunks than legacy for the same file', () async {
      final bytes = List<int>.filled(kLegacyFileChunkRaw + 1, 0x41);
      await seed('mid.bin', bytes);
      final largeSent = <Map<String, Object?>>[];
      await FileSync(
        fs: AppFileSystem(tmp.path),
        sendFeature: (type, payload) async {
          largeSent.add({'type': type, ...payload});
        },
      ).handleEvent(pullEnv('abcdef0123456789', 'mid.bin',
          caps: [kFilesLargeChunkCapability], build: 11));
      await waitForChunks(largeSent, 1);
      final largeChunks = largeSent.where((m) => m['type'] == 'file-chunk').toList();
      expect(largeChunks, hasLength(1));
      expect(largeChunks.single['total_chunks'], 1);
      expect(largeChunks.single['offset'], 0);

      final legacySent = <Map<String, Object?>>[];
      await FileSync(
        fs: AppFileSystem(tmp.path),
        sendFeature: (type, payload) async {
          legacySent.add({'type': type, ...payload});
        },
      ).handleEvent(pullEnv('1234567890abcdef', 'mid.bin'));
      await waitForChunks(legacySent, 2);
      final legacyChunks = legacySent.where((m) => m['type'] == 'file-chunk').toList();
      expect(legacyChunks, hasLength(2));
      expect(legacyChunks[0]['offset'], 0);
      expect(legacyChunks[1]['offset'], kLegacyFileChunkRaw);
    });

    test('last chunk carries a valid sha256 of the file', () async {
      final bytes = 'hello pull'.codeUnits;
      await seed('sha.txt', bytes);
      final sent = <Map<String, Object?>>[];
      await FileSync(
        fs: AppFileSystem(tmp.path),
        sendFeature: (type, payload) async {
          sent.add({'type': type, ...payload});
        },
      ).handleEvent(pullEnv('abcdef0123456789', 'sha.txt',
          caps: [kFilesLargeChunkCapability], build: 11));
      await waitForChunks(sent, 1);
      final chunks = sent.where((m) => m['type'] == 'file-chunk').toList();
      expect(chunks, hasLength(1));
      final sha = chunks.single['sha256'] as String?;
      expect(sha, isNotNull);
      expect(isValidSha256(sha!), isTrue);
      expect(sha.toLowerCase(), sha256.convert(bytes).toString());
    });

    test('injected tiny stride is honored (test seam)', () async {
      await seed('tiny.txt', List<int>.filled(12, 0x42));
      final sent = <Map<String, Object?>>[];
      await FileSync(
        fs: AppFileSystem(tmp.path),
        sendFeature: (type, payload) async {
          sent.add({'type': type, ...payload});
        },
        chunkSize: 5,
      ).handleEvent(pullEnv('abcdef0123456789', 'tiny.txt'));
      await waitForChunks(sent, 3);
      final chunks = sent.where((m) => m['type'] == 'file-chunk').toList();
      expect(chunks, hasLength(3));
      expect(chunks.map((m) => m['offset']), [0, 5, 10]);
    });

    test('file-cancel before pull aborts the transfer', () async {
      await seed('cancelled.txt', 'data'.codeUnits);
      final sent = <Map<String, Object?>>[];
      final sync = FileSync(
        fs: AppFileSystem(tmp.path),
        sendFeature: (type, payload) async {
          sent.add({'type': type, ...payload});
        },
      );
      await sync.handleEvent({
        'type': 'file-cancel',
        'payload': {'transfer_id': 'abcdef0123456789', 'path': 'cancelled.txt'},
      });
      await sync.handleEvent(pullEnv('abcdef0123456789', 'cancelled.txt',
          caps: [kFilesLargeChunkCapability], build: 11));
      await Future<void>.delayed(const Duration(milliseconds: 200));
      expect(sent.where((m) => m['type'] == 'file-chunk'), isEmpty);
    });

    test('transient send failure retries once then succeeds', () async {
      await seed('retry.txt', 'retry me'.codeUnits);
      final sent = <Map<String, Object?>>[];
      var calls = 0;
      await FileSync(
        fs: AppFileSystem(tmp.path),
        sendFeature: (type, payload) async {
          calls++;
          if (calls == 1) throw Exception('ws flap');
          sent.add({'type': type, ...payload});
        },
      ).handleEvent(pullEnv('abcdef0123456789', 'retry.txt',
          caps: [kFilesLargeChunkCapability], build: 11));
      await waitForChunks(sent, 1);
      expect(sent.where((m) => m['type'] == 'file-chunk'), hasLength(1));
      expect(calls, 2);
    });

    test('persistent send failure aborts without chunks', () async {      await seed('dead.txt', 'dead'.codeUnits);
      final sent = <Map<String, Object?>>[];
      await FileSync(
        fs: AppFileSystem(tmp.path),
        sendFeature: (type, payload) async {
          throw Exception('offline');
        },
      ).handleEvent(pullEnv('abcdef0123456789', 'dead.txt',
          caps: [kFilesLargeChunkCapability], build: 11));
      await Future<void>.delayed(const Duration(milliseconds: 500));
      expect(sent, isEmpty);
    });

    test('concurrent pulls both complete without blocking the channel', () async {
      await seed('one.txt', 'first file'.codeUnits);
      await seed('two.txt', 'second file'.codeUnits);
      final sent = <Map<String, Object?>>[];
      final sync = FileSync(
        fs: AppFileSystem(tmp.path),
        sendFeature: (type, payload) async {
          sent.add({'type': type, ...payload});
        },
      );
      // Fire both pulls without awaiting between them: the second must not
      // wait behind the first.
      final first = sync.handleEvent(pullEnv('aaaaaaaaaaaaaaaa', 'one.txt',
          caps: [kFilesLargeChunkCapability], build: 11));
      final second = sync.handleEvent(pullEnv('bbbbbbbbbbbbbbbb', 'two.txt',
          caps: [kFilesLargeChunkCapability], build: 11));
      await first;
      await second;
      await waitForChunks(sent, 2);
      final chunks = sent.where((m) => m['type'] == 'file-chunk').toList();
      expect(chunks, hasLength(2));
      expect(chunks.map((m) => m['transfer_id']).toSet(),
          {'aaaaaaaaaaaaaaaa', 'bbbbbbbbbbbbbbbb'});
    });
  });

  group('PullReader', () {
    late Directory tmp;
    setUp(() {
      tmp = Directory.systemTemp.createTempSync('fuse-reader-');
    });
    tearDown(() {
      try {
        tmp.deleteSync(recursive: true);
      } catch (_) {}
    });

    test('single handle reads sequentially', () async {
      await File('${tmp.path}/seq.txt').writeAsString('hello world');
      final fs = AppFileSystem(tmp.path);
      final reader = await fs.openPullReader('seq.txt');
      try {
        expect(await reader.readNext(5), 'hello'.codeUnits);
        expect(await reader.readNext(6), ' world'.codeUnits);
        expect(await reader.readNext(0), isEmpty);
      } finally {
        await reader.close();
      }
    });

    test('fallback reader serves fakes via readChunk', () async {
      final fs = _MemFileSystem({'f.txt': 'abcdef'.codeUnits});
      final reader = await fs.openPullReader('f.txt');
      try {
        expect(await reader.readNext(2), 'ab'.codeUnits);
        expect(await reader.readNext(4), 'cdef'.codeUnits);
      } finally {
        await reader.close();
      }
    });
  });
}

class _MemFileSystem extends FileSystem {
  _MemFileSystem(this.files);

  final Map<String, List<int>> files;

  @override
  Future<List<FileEntry>> list(String relPath) => throw FileSystemException('no', relPath);
  @override
  Future<void> mkdir(String relPath) => throw FileSystemException('no', relPath);
  @override
  Future<void> delete(String relPath) => throw FileSystemException('no', relPath);
  @override
  Future<void> rename(String from, String to) => throw FileSystemException('no', from);
  @override
  Future<int> size(String relPath) async => files[relPath]?.length ?? (throw FileSystemException('not found', relPath));
  @override
  Future<List<int>> readChunk(String relPath, int offset, int length) async {
    final data = files[relPath] ?? (throw FileSystemException('not found', relPath));
    if (offset >= data.length || length <= 0) return const [];
    final end = (offset + length).clamp(0, data.length);
    return data.sublist(offset, end);
  }

  @override
  Future<void> discardStaged(String relPath, String transferId) async {}
  @override
  Future<void> writeChunk(String relPath, String transferId, int offset, int totalSize, List<int> data, bool isLast,
          {String policy = '', int sourceMtime = 0, String expectedSha256 = ''}) async {}
}
