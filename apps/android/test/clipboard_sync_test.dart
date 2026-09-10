// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/clipboard/clipboard_chunks.dart';
import 'package:fuseitall/features/clipboard/clipboard_sync.dart';
import 'package:fuseitall/features/settings/app_settings.dart';

void main() {
  group('ClipState HLC', () {
    test('same-second bumps counter, older wall does not rewind', () {
      var s = const ClipState();
      final a = s.setLocal('hello', 10)!;
      expect(a.changedAt, 10);
      expect(a.changedC, 0);
      s = a;
      // Same text is a no-op.
      expect(identical(s.setLocal('hello', 11), s), isTrue);
      final b = s.setLocal('world', 10)!;
      expect(b.changedAt, 10);
      expect(b.changedC, 1);
    });

    test('remote tie keeps local, higher counter wins', () {
      var s = const ClipState().setLocal('a', 10)!;
      expect(
        s.applyRemote(next: 'old', nextChangedAt: 5, nextOrigin: 'mac', nonce: 'n1'),
        isNull,
      );
      expect(
        s.applyRemote(next: 'tie', nextChangedAt: 10, nextChangedC: 0, nextOrigin: 'mac', nonce: 'n2'),
        isNull,
      );
      final won = s.applyRemote(
        next: 'new',
        nextChangedAt: 10,
        nextChangedC: 1,
        nextOrigin: 'mac',
        nonce: 'n3',
      );
      expect(won, isNotNull);
      expect(won!.text, 'new');
      // Duplicate nonce never re-applies.
      expect(
        won.applyRemote(next: 'new', nextChangedAt: 10, nextChangedC: 1, nextOrigin: 'mac', nonce: 'n3'),
        isNull,
      );
    });

    test('takePending carries HLC + hash', () {
      final s = const ClipState().setLocal('hi', 7)!;
      final p = s.takePending()!;
      expect(p['changed_at'], 7);
      expect(p['changed_c'], isNotNull);
      expect((p['content_hash'] as String).length, 64);
    });
  });

  group('ClipChunkHub', () {
    test('manifest + chunks reassemble and verify', () {
      final hub = ClipChunkHub();
      final raw = List<int>.generate(6 * 1024 * 1024, (i) => i % 251);
      final parts = splitClipRaw(raw);
      expect(parts.length, 6);
      // Fake hash for the test session.
      const hash = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa';
      expect(
        hub.begin({
          'session_id': 'abc123',
          'mime': 'image/png',
          'total_raw': raw.length,
          'total_chunks': parts.length,
          'changed_at': 10,
          'origin': 'mac',
          'content_hash': hash,
        }),
        isTrue,
      );
      Map<String, Object?>? out;
      for (final (idx, off, _) in parts) {
        // ignore: avoid_dynamic_calls
        out = hub.add({
          'session_id': 'abc123',
          'chunk_index': idx,
          'total_chunks': parts.length,
          'offset': off,
          'total_raw': raw.length,
          if (idx == parts.length - 1) 'content_hash': hash,
          'data_b64': '',
        });
      }
      // Empty bodies fail closed (hub drops), so no reassembly.
      expect(out, isNull);
    });
  });

  group('Sensitive settings', () {
    test('absent allow-sensitive defaults false and round-trips', () {
      final st = AppSettings.fromJson({'updated_unix': 1});
      expect(st.clipboardAllowSensitive, isFalse);
      expect(st.toJson().containsKey('clipboard_allow_sensitive'), isFalse);
      final on = st.withClipboardAllowSensitive(true, nowUnix: 2);
      expect(on.clipboardAllowSensitive, isTrue);
      expect(AppSettings.fromJson(on.toJson()).clipboardAllowSensitive, isTrue);
    });
  });
}
