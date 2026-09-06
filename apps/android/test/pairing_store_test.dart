// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/pairing/pair_qr.dart';
import 'package:fuseitall/features/pairing/pairing_store.dart';

final _fp = 'aa' * 32;

PairQR _qr() => PairQR(
      v: 1,
      deviceName: 'Test Mac',
      platform: 'mac',
      host: '192.168.1.10',
      port: 18443,
      fingerprint: _fp,
      pubkey: 'c3VwZXItc2VjcmV0LWtleQ==',
      token: 'abcdef0123456789abcdef0123456789',
      code: '123456',
    );

class FakeStorage implements KeyValueStorage {
  final map = <String, String>{};
  var failReads = false;

  @override
  Future<String?> read(String key) async {
    if (failReads) throw StateError('keystore locked');
    return map[key];
  }

  @override
  Future<void> write(String key, String value) async {
    map[key] = value;
  }

  @override
  Future<void> delete(String key) async {
    map.remove(key);
  }
}

class ThrowingStorage implements KeyValueStorage {
  @override
  Future<String?> read(String key) async => throw StateError('locked');

  @override
  Future<void> write(String key, String value) async {}

  @override
  Future<void> delete(String key) async {}
}

void main() {
  group('PairingStore', () {
    test('round-trip: save then load returns the pairing', () async {
      final storage = FakeStorage();
      final store = PairingStore(storage);
      await store.save(_qr());
      final loaded = await store.load();
      expect(loaded, isNotNull);
      expect(loaded!.token, _qr().token);
      expect(loaded.code, '123456');
      expect(loaded.deviceName, 'Test Mac');
    });

    test('empty store loads null (scan screen)', () async {
      final store = PairingStore(FakeStorage());
      expect(await store.load(), isNull);
    });

    test('clear wipes the pairing (unpair)', () async {
      final storage = FakeStorage();
      final store = PairingStore(storage);
      await store.save(_qr());
      await store.clear();
      expect(await store.load(), isNull);
    });

    test('corrupt JSON falls back to null, never throws', () async {
      final storage = FakeStorage();
      storage.map[kPairingStorageKey] = 'not-json{{{';
      expect(await PairingStore(storage).load(), isNull);
    });

    test('wrong-shape JSON falls back to null, never throws', () async {
      final storage = FakeStorage();
      storage.map[kPairingStorageKey] = jsonEncode({'v': 2, 'nope': true});
      expect(await PairingStore(storage).load(), isNull);
    });

    test('secure-storage read failure falls back to null', () async {
      expect(await PairingStore(ThrowingStorage()).load(), isNull);
    });
  });
}
