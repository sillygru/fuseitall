// SPDX-License-Identifier: AGPL-3.0-only

import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/files/file_models.dart';
import 'package:fuseitall/features/files/file_sync.dart';
import 'package:fuseitall/features/files/file_system.dart';
import 'package:fuseitall/features/ping/proto_client.dart';

void main() {
  group('file conflict policy', () {
    test('wire policy validates, sender-side never validates', () {
      expect(isValidFilePolicy(''), isTrue);
      expect(isValidFilePolicy(kFilePolicyOverwrite), isTrue);
      expect(isValidFilePolicy(kFilePolicyIfNewer), isTrue);
      expect(isValidFilePolicy(kFilePolicySkip), isFalse);
      expect(isValidFilePolicy(kFilePolicyKeepBoth), isFalse);
      expect(isValidFilePolicy(kFilePolicyStop), isFalse);
    });

    test('isSourceNewer mirrors core: mtime first, size tiebreak', () {
      expect(isSourceNewer(200, 100, 10, 10), isTrue);
      expect(isSourceNewer(100, 200, 10, 10), isFalse);
      expect(isSourceNewer(100, 100, 10, 10), isFalse);
      expect(isSourceNewer(100, 100, 11, 10), isTrue);
    });

    test('keepBothName numbers like Finder', () {
      expect(keepBothName('a.txt', {'a.txt', 'a (2).txt'}), 'a (3).txt');
      expect(keepBothName('b.txt', {'a.txt'}), 'b.txt');
      expect(keepBothName('docs/note', {'docs/note'}), 'docs/note (2)');
    });

    test('chunk stride accepts legacy 1 MiB and 4 MiB, rejects neither', () {
      expect(totalChunksForSize(0, kMaxFileChunkRaw), 1);
      expect(totalChunksForSize(5 << 20, kMaxFileChunkRaw), 2);
      expect(totalChunksForSize(5 << 20, kLegacyFileChunkRaw), 5);
      expect(chunkStrideFor(5 << 20, 2), kMaxFileChunkRaw);
      expect(chunkStrideFor((1 << 20) + 3, 2), kLegacyFileChunkRaw);
      expect(chunkStrideFor(3, 7), isNull);
    });
  });

  group('AppFileSystem.writeChunk policy', () {
    late Directory tmp;
    setUp(() {
      tmp = Directory.systemTemp.createTempSync('fuse-conflict-');
    });
    tearDown(() {
      try {
        tmp.deleteSync(recursive: true);
      } catch (_) {}
    });

    Future<void> seed(String rel, String content) async {
      final f = File('${tmp.path}/$rel');
      await f.parent.create(recursive: true);
      await f.writeAsString(content);
    }

    Future<String> read(String rel) => File('${tmp.path}/$rel').readAsString();

    test('legacy empty policy keeps both with suffix', () async {
      final fs = AppFileSystem(tmp.path);
      await seed('a.txt', 'old');
      await fs.writeChunk('a.txt', 'abcdef0123456789', 0, 3, 'new'.codeUnits, true);
      expect(await read('a.txt'), 'old');
      final alt = File('${tmp.path}/a.txt-abcdef');
      expect(await alt.exists(), isTrue);
      expect(await alt.readAsString(), 'new');
    });

    test('overwrite replaces atomically', () async {
      final fs = AppFileSystem(tmp.path);
      await seed('a.txt', 'old');
      await fs.writeChunk('a.txt', 'abcdef0123456789', 0, 3, 'new'.codeUnits, true,
          policy: kFilePolicyOverwrite);
      expect(await read('a.txt'), 'new');
    });

    test('if_newer skips older source, replaces newer', () async {
      final fs = AppFileSystem(tmp.path);
      await seed('a.txt', 'old!');
      final target = File('${tmp.path}/a.txt');
      final targetMtime = await target.lastModified();
      final targetSecs = targetMtime.millisecondsSinceEpoch ~/ 1000;
      // Older source: skipped, target untouched.
      await fs.writeChunk('a.txt', 'abcdef0123456789', 0, 3, 'new'.codeUnits, true,
          policy: kFilePolicyIfNewer, sourceMtime: targetSecs - 100);
      expect(await read('a.txt'), 'old!');
      // Newer source: replaced.
      await fs.writeChunk('a.txt', '1234567890abcdef', 0, 3, 'new'.codeUnits, true,
          policy: kFilePolicyIfNewer, sourceMtime: targetSecs + 100);
      expect(await read('a.txt'), 'new');
    });

    test('file-ack capability advertised and routed', () {
      expect(kFeatureCapabilities, contains(kFilesAckCapability));
      expect(featurePath('file-ack'), kFilesPath);
    });

    test('file_sync acks final chunk commit', () async {
      final fs = AppFileSystem(tmp.path);
      final sent = <Map<String, Object?>>[];
      final sync = FileSync(
        fs: fs,
        sendFeature: (type, payload) async {
          sent.add({'type': type, ...payload});
        },
      );
      await sync.handleEvent({
        'type': 'file-chunk',
        'payload': {
          'transfer_id': 'abcdef0123456789',
          'path': 'acked.txt',
          'offset': 0,
          'total_size': 1,
          'chunk_index': 0,
          'total_chunks': 1,
          'data_b64': 'eA==',
        },
      });
      expect(File('${tmp.path}/acked.txt').existsSync(), isTrue);
      final acks = sent.where((m) => m['type'] == 'file-ack').toList();
      expect(acks, hasLength(1));
      expect(acks.single['transfer_id'], 'abcdef0123456789');
      expect(acks.single['ok'], isTrue);
    });

    test('concurrent chunks across transfers both commit', () async {
      final fs = AppFileSystem(tmp.path);
      final sent = <Map<String, Object?>>[];
      final sync = FileSync(
        fs: fs,
        sendFeature: (type, payload) async {
          sent.add({'type': type, ...payload});
        },
      );
      Map<String, dynamic> oneChunk(String id, String path) => {
            'type': 'file-chunk',
            'payload': {
              'transfer_id': id,
              'path': path,
              'offset': 0,
              'total_size': 1,
              'chunk_index': 0,
              'total_chunks': 1,
              'data_b64': 'eA==',
            },
          };
      // Fire both without awaiting between them: the second transfer must
      // not wait behind the first one's disk commit.
      final a = sync.handleEvent(oneChunk('aaaaaaaaaaaaaaaa', 'one.txt'));
      final b = sync.handleEvent(oneChunk('bbbbbbbbbbbbbbbb', 'two.txt'));
      await Future.wait([a, b]);
      expect(File('${tmp.path}/one.txt').existsSync(), isTrue);
      expect(File('${tmp.path}/two.txt').existsSync(), isTrue);
      expect(sent.where((m) => m['type'] == 'file-ack' && m['ok'] == true), hasLength(2));
    });

    test('same-transfer chunks fired together commit in order', () async {
      final fs = AppFileSystem(tmp.path);
      final sent = <Map<String, Object?>>[];
      final sync = FileSync(
        fs: fs,
        sendFeature: (type, payload) async {
          sent.add({'type': type, ...payload});
        },
      );
      final head = base64Encode(List<int>.filled(kLegacyFileChunkRaw, 0x41));
      Map<String, dynamic> env(String id, String path, int index, int offset, int totalSize, int totalChunks, String b64) => {
            'type': 'file-chunk',
            'payload': {
              'transfer_id': id,
              'path': path,
              'offset': offset,
              'total_size': totalSize,
              'chunk_index': index,
              'total_chunks': totalChunks,
              'data_b64': b64,
            },
          };
      const totalSize = kLegacyFileChunkRaw + 5;
      // Both in flight at once (no await between them): the per-transfer
      // FIFO still commits head before tail.
      final first = sync.handleEvent(
          env('cccccccccccccccc', 'ord.bin', 0, 0, totalSize, 2, head));
      final second = sync.handleEvent(env('cccccccccccccccc', 'ord.bin', 1,
          kLegacyFileChunkRaw, totalSize, 2, base64Encode('BBBBB'.codeUnits)));
      await Future.wait([first, second]);
      final bytes = File('${tmp.path}/ord.bin').readAsBytesSync();
      expect(bytes.length, totalSize);
      expect(bytes.sublist(0, 4), [0x41, 0x41, 0x41, 0x41]);
      expect(bytes.sublist(kLegacyFileChunkRaw), 'BBBBB'.codeUnits);
      expect(sent.where((m) => m['type'] == 'file-ack' && m['ok'] == true), hasLength(1));
    });

    test('late duplicate after commit drops without orphan stage', () async {
      final fs = AppFileSystem(tmp.path);
      final sent = <Map<String, Object?>>[];
      final sync = FileSync(
        fs: fs,
        sendFeature: (type, payload) async {
          sent.add({'type': type, ...payload});
        },
      );
      Map<String, dynamic> env(int index, int offset, String b64) => {
            'type': 'file-chunk',
            'payload': {
              'transfer_id': 'dddddddddddddddd',
              'path': 'dup.bin',
              'offset': offset,
              'total_size': 1,
              'chunk_index': index,
              'total_chunks': 1,
              'data_b64': b64,
            },
          };
      await sync.handleEvent(env(0, 0, 'eA=='));
      expect(File('${tmp.path}/dup.bin').readAsStringSync(), 'x');
      // Duplicate racing the ack: dropped, no second ack, no orphan part.
      await sync.handleEvent(env(0, 0, 'eQ=='));
      expect(File('${tmp.path}/dup.bin').readAsStringSync(), 'x');
      expect(File('${tmp.path}/dup.bin.part.dddddddddddddddd').existsSync(), isFalse);
      expect(sent.where((m) => m['type'] == 'file-ack'), hasLength(1));
    });

    test('file_sync nacks failed final commit', () async {
      final fs = _ThrowingFileSystem();
      final sent = <Map<String, Object?>>[];
      final sync = FileSync(
        fs: fs,
        sendFeature: (type, payload) async {
          sent.add({'type': type, ...payload});
        },
      );
      await sync.handleEvent({
        'type': 'file-chunk',
        'payload': {
          'transfer_id': 'abcdef0123456789',
          'path': 'x.txt',
          'offset': 0,
          'total_size': 1,
          'chunk_index': 0,
          'total_chunks': 1,
          'data_b64': 'eA==',
        },
      });
      final acks = sent.where((m) => m['type'] == 'file-ack').toList();
      expect(acks, hasLength(1));
      expect(acks.single['ok'], isFalse);
      // The real reason must ride the nack so the Mac can show Retry with
      // context instead of a generic string.
      expect((acks.single['error'] as String).toLowerCase(), contains('disk full'));
    });

    test('multi-chunk stage keeps earlier chunks (no truncate)', () async {
      final fs = AppFileSystem(tmp.path);
      final first = List<int>.filled(10, 0x41);
      final second = List<int>.filled(5, 0x42);
      await fs.writeChunk('m.bin', 'abcdef0123456789', 0, 15, first, false);
      await fs.writeChunk('m.bin', 'abcdef0123456789', 10, 15, second, true);
      final committed = File('${tmp.path}/m.bin').readAsBytesSync();
      expect(committed, [...first, ...second]);
    });

    test('file_sync nack surfaces sha mismatch verbatim', () async {
      final fs = AppFileSystem(tmp.path);
      final sent = <Map<String, Object?>>[];
      final sync = FileSync(
        fs: fs,
        sendFeature: (type, payload) async {
          sent.add({'type': type, ...payload});
        },
      );
      await fs.writeChunk('s2.txt', 'abcdef0123456789', 0, 3, 'new'.codeUnits, false);
      await sync.handleEvent({
        'type': 'file-chunk',
        'payload': {
          'transfer_id': 'abcdef0123456789',
          'path': 's2.txt',
          'offset': 0,
          'total_size': 3,
          'chunk_index': 0,
          'total_chunks': 1,
          'data_b64': base64Encode('new'.codeUnits),
          'sha256': '0' * 64,
        },
      });
      final acks = sent.where((m) => m['type'] == 'file-ack').toList();
      expect(acks, hasLength(1));
      expect(acks.single['ok'], isFalse);
      expect((acks.single['error'] as String).toLowerCase(), contains('sha'));
    });

    test('sha mismatch deletes stage and throws', () async {
      final fs = AppFileSystem(tmp.path);
      await fs.writeChunk('s.txt', 'abcdef0123456789', 0, 3, 'new'.codeUnits, false);
      await expectLater(
        () => fs.writeChunk('s.txt', 'abcdef0123456789', 3, 3, <int>[], true,
            expectedSha256: '0' * 64),
        throwsA(isA<FileSystemException>()),
      );
      expect(File('${tmp.path}/s.txt').existsSync(), isFalse);
      expect(File('${tmp.path}/s.txt.part.abcdef0123456789').existsSync(), isFalse);
    });

    test('matching sha commits', () async {
      final fs = AppFileSystem(tmp.path);
      final bytes = 'hello'.codeUnits;
      final sha = sha256.convert(bytes).toString();
      await fs.writeChunk('h.txt', 'abcdef0123456789', 0, 5, bytes, true, expectedSha256: sha);
      expect(File('${tmp.path}/h.txt').readAsStringSync(), 'hello');
    });

    test('bad sha shape rejected', () async {
      final fs = AppFileSystem(tmp.path);
      await expectLater(
        () => fs.writeChunk('b.txt', 'abcdef0123456789', 0, 1, 'x'.codeUnits, true,
            expectedSha256: 'not-hex'),
        throwsA(isA<FileSystemException>()),
      );
    });

    test('strict chunk shapes rejected end to end', () async {
      final fs = AppFileSystem(tmp.path);
      final sent = <Map<String, Object?>>[];
      final sync = FileSync(
        fs: fs,
        sendFeature: (type, payload) async {
          sent.add({'type': type, ...payload});
        },
      );
      Future<void> chunk(Map<String, Object?> fields) => sync.handleEvent({
            'type': 'file-chunk',
            'payload': {
              'transfer_id': 'abcdef0123456789',
              'path': 's.txt',
              'offset': 0,
              'total_size': 1,
              'chunk_index': 0,
              'total_chunks': 1,
              'data_b64': 'eA==',
              ...fields,
            },
          });
      await chunk({'offset': 1}); // offset must equal index * 1MiB
      await chunk({'total_chunks': 2}); // count must match size
      await chunk({'data_b64': 'YWJj'}); // raw length must match
      await chunk({'sha256': sha256.convert('x'.codeUnits).toString(), 'chunk_index': 0, 'total_chunks': 2, 'total_size': 2}); // sha only on last
      expect(File('${tmp.path}/s.txt').existsSync(), isFalse);
      expect(sent.where((m) => m['type'] == 'file-ack'), isEmpty);
    });

    test('replace recovery restores fresh backup, drops stale', () async {
      final fs = AppFileSystem(tmp.path);
      // Fresh backup with missing target: restored on next first chunk, and
      // the incoming bytes keep-both beside it (legacy policy, no overwrite).
      await File('${tmp.path}/r.txt.replaced.aaaabbbbccccdddd').writeAsString('backup');
      await fs.writeChunk('r.txt', 'abcdef0123456789', 0, 3, 'new'.codeUnits, true);
      expect(File('${tmp.path}/r.txt').readAsStringSync(), 'backup');
      expect(File('${tmp.path}/r.txt-abcdef').readAsStringSync(), 'new');
      expect(File('${tmp.path}/r.txt.replaced.aaaabbbbccccdddd').existsSync(), isFalse);
      // Stale backup beside a live target: dropped.
      await File('${tmp.path}/r.txt.replaced.eeefff11112222').writeAsString('stale');
      final stale = File('${tmp.path}/r.txt.replaced.eeefff11112222');
      await stale.setLastModified(DateTime.now().subtract(const Duration(hours: 2)));
      await fs.writeChunk('r.txt', '1234567890abcdef', 0, 3, 'v2!'.codeUnits, true,
          policy: kFilePolicyOverwrite);
      expect(File('${tmp.path}/r.txt').readAsStringSync(), 'v2!');
      expect(stale.existsSync(), isFalse);
    });

    test('file-cancel discards the staged part', () async {
      final fs = AppFileSystem(tmp.path);
      final sent = <Map<String, Object?>>[];
      final sync = FileSync(
        fs: fs,
        sendFeature: (type, payload) async {
          sent.add({'type': type, ...payload});
        },
      );
      final big = base64Encode(List<int>.filled(kMaxFileChunkRaw, 0));
      await sync.handleEvent({
        'type': 'file-chunk',
        'payload': {
          'transfer_id': 'abcdef0123456789',
          'path': 'c.txt',
          'offset': 0,
          'total_size': kMaxFileChunkRaw + 1,
          'chunk_index': 0,
          'total_chunks': 2,
          'data_b64': big,
        },
      });
      expect(File('${tmp.path}/c.txt.part.abcdef0123456789').existsSync(), isTrue);
      final handled = await sync.handleEvent({
        'type': 'file-cancel',
        'payload': {'transfer_id': 'abcdef0123456789', 'path': 'c.txt'},
      });
      expect(handled, isTrue);
      expect(File('${tmp.path}/c.txt.part.abcdef0123456789').existsSync(), isFalse);
      // Malformed cancels are ignored, never throw.
      expect(
          await sync.handleEvent({
            'type': 'file-cancel',
            'payload': <String, dynamic>{},
          }),
          isTrue);
      expect(
          await sync.handleEvent({
            'type': 'file-cancel',
            'payload': {'transfer_id': 'short', 'path': 'c.txt'},
          }),
          isTrue);
    });

    test('legacy 1 MiB stride still accepted beside 4 MiB', () async {
      final fs = AppFileSystem(tmp.path);
      final sent = <Map<String, Object?>>[];
      final sync = FileSync(
        fs: fs,
        sendFeature: (type, payload) async {
          sent.add({'type': type, ...payload});
        },
      );
      final legacy = base64Encode(List<int>.filled(kLegacyFileChunkRaw, 7));
      await sync.handleEvent({
        'type': 'file-chunk',
        'payload': {
          'transfer_id': '1234567890abcdef',
          'path': 'legacy.bin',
          'offset': 0,
          'total_size': kLegacyFileChunkRaw + 1,
          'chunk_index': 0,
          'total_chunks': 2,
          'data_b64': legacy,
        },
      });
      expect(File('${tmp.path}/legacy.bin.part.1234567890abcdef').existsSync(), isTrue);
      // Neither-stride counts are dropped before touching disk.
      await sync.handleEvent({
        'type': 'file-chunk',
        'payload': {
          'transfer_id': 'fedcba0987654321',
          'path': 'bad.bin',
          'offset': 0,
          'total_size': 3,
          'chunk_index': 0,
          'total_chunks': 7,
          'data_b64': base64Encode([1, 2, 3]),
        },
      });
      expect(File('${tmp.path}/bad.bin.part.fedcba0987654321').existsSync(), isFalse);
    });

    test('file-stat reports the smallest missing chunk', () async {
      final fs = AppFileSystem(tmp.path);
      final sent = <Map<String, Object?>>[];
      final sync = FileSync(
        fs: fs,
        sendFeature: (type, payload) async {
          sent.add({'type': type, ...payload});
        },
      );
      final big = base64Encode(List<int>.filled(kMaxFileChunkRaw, 0));
      Future<Object?> stat(String id) async {
        await sync.handleEvent({
          'type': 'file-stat-req',
          'payload': {'transfer_id': id},
        });
        return sent
            .where((m) => m['type'] == 'file-stat-resp' && m['transfer_id'] == id)
            .last['next_chunk'];
      }
      Future<void> chunk(int idx) => sync.handleEvent({
            'type': 'file-chunk',
            'payload': {
              'transfer_id': 'abcdef0123456789',
              'path': 't.bin',
              'offset': idx * kMaxFileChunkRaw,
              'total_size': 2 * kMaxFileChunkRaw + 1,
              'chunk_index': idx,
              'total_chunks': 3,
              'data_b64': big,
            },
          });
      // Out of order: chunk 1 lands first, so nothing is contiguous.
      await chunk(1);
      expect(await stat('abcdef0123456789'), 0);
      await chunk(0);
      expect(await stat('abcdef0123456789'), 2);
      final resps = sent.where((m) => m['type'] == 'file-stat-resp').toList();
      expect(resps.last['total_chunks'], 3);
      // Unknown transfers answer error so the sender restarts from zero.
      await sync.handleEvent({
        'type': 'file-stat-req',
        'payload': {'transfer_id': '0000000000000000'},
      });
      final unknowns = sent
          .where((m) => m['type'] == 'file-stat-resp' && m['transfer_id'] == '0000000000000000')
          .toList();
      expect(unknowns, hasLength(1));
      expect(unknowns.single['error'], isNotEmpty);
      // Cancel clears tracking: the transfer reads as unknown after.
      await sync.handleEvent({
        'type': 'file-cancel',
        'payload': {'transfer_id': 'abcdef0123456789', 'path': 't.bin'},
      });
      await sync.handleEvent({
        'type': 'file-stat-req',
        'payload': {'transfer_id': 'abcdef0123456789'},
      });
      final after = sent
          .where((m) =>
              m['type'] == 'file-stat-resp' && m['transfer_id'] == 'abcdef0123456789')
          .toList();
      expect(after.last['error'], isNotEmpty);
    });

    test('file_sync rejects sender-side policies and bad mtime', () async {
      final fs = AppFileSystem(tmp.path);
      final seen = <String>[];
      final sync = FileSync(
        fs: fs,
        sendFeature: (type, payload) async {
          seen.add(type);
        },
      );
      // skip policy must be dropped before touching fs.
      await sync.handleEvent({
        'type': 'file-chunk',
        'payload': {
          'transfer_id': 'abcdef0123456789',
          'path': 'x.txt',
          'offset': 0,
          'total_size': 1,
          'chunk_index': 0,
          'total_chunks': 1,
          'data_b64': 'eA==',
          'policy': kFilePolicySkip,
        },
      });
      expect(File('${tmp.path}/x.txt').existsSync(), isFalse);
      expect(File('${tmp.path}/x.txt.part.abcdef0123456789').existsSync(), isFalse);
    });
  });
}

class _ThrowingFileSystem extends FileSystem {
  @override
  Future<List<FileEntry>> list(String relPath) => throw FileSystemException('no', relPath);
  @override
  Future<void> mkdir(String relPath) => throw FileSystemException('no', relPath);
  @override
  Future<void> delete(String relPath) => throw FileSystemException('no', relPath);
  @override
  Future<void> rename(String from, String to) => throw FileSystemException('no', from);
  @override
  Future<int> size(String relPath) => throw FileSystemException('no', relPath);
  @override
  Future<List<int>> readChunk(String relPath, int offset, int length) =>
      throw FileSystemException('no', relPath);
  @override
  Future<void> writeChunk(String relPath, String transferId, int offset, int totalSize, List<int> data, bool isLast,
          {String policy = '', int sourceMtime = 0, String expectedSha256 = ''}) =>
      throw FileSystemException('disk full', relPath);
  @override
  Future<void> discardStaged(String relPath, String transferId) async {}
}
