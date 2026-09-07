// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/net/go_server.dart';
import 'package:fuseitall/net/phone_identity_store.dart';
import 'package:fuseitall/features/pairing/pairing_store.dart';

// Fake bridge: no .so needed on the host VM. Tests the Dart lifecycle logic
// (validation, port parsing, poll fan-out, stop) against the real PhoneServer.
class FakeBridge implements BridgeHandle {
  FakeBridge({
    this.startResult = _kDefaultStart,
    this.startWithCertResult,
    this.certPemResult = 'cert-pem',
    this.keyPemResult = 'key-pem',
  });

  static const _kDefaultStart =
      '41233:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa';

  final String? startResult;
  final String? startWithCertResult;
  final String? certPemResult;
  final String? keyPemResult;
  final List<String> startedTokens = [];
  final List<String> startedWithCertTokens = [];
  final List<String> queued = [];
  var stopCalls = 0;

  @override
  String? start(String token, int port) {
    startedTokens.add(token);
    return startResult;
  }

  @override
  String? startWithCert(
      String token, int port, String certPem, String keyPem) {
    startedWithCertTokens.add(token);
    return startWithCertResult;
  }

  @override
  String? certPem() => certPemResult;

  @override
  String? keyPem() => keyPemResult;

  @override
  String? poll() => queued.isEmpty ? null : queued.removeAt(0);

  final List<String> featQueued = [];

  @override
  String? pollEvent() => featQueued.isEmpty ? null : featQueued.removeAt(0);

  @override
  int stop() => ++stopCalls;
}

class FakeKeyValueStorage implements KeyValueStorage {
  final Map<String, String> values = {};
  var failRead = false;

  @override
  Future<String?> read(String key) async {
    if (failRead) throw StateError('keystore locked');
    return values[key];
  }

  @override
  Future<void> write(String key, String value) async {
    values[key] = value;
  }

  @override
  Future<void> delete(String key) async {
    values.remove(key);
  }
}

void main() {
  group('PhoneServer validation', () {
    test('empty token throws ArgumentError without touching the bridge', () async {
      var opened = false;
      final server = PhoneServer(openBridge: () {
        opened = true;
        return FakeBridge();
      });
      await expectLater(
        server.startPhoneServer(token: ''),
        throwsA(isA<ArgumentError>()),
      );
      expect(opened, isFalse);
    });

    test('bridge returning null fails closed with StateError', () async {
      final server = PhoneServer(openBridge: () => FakeBridge(startResult: null));
      await expectLater(
        server.startPhoneServer(token: 'tok'),
        throwsA(isA<StateError>()),
      );
      expect(server.port, isNull);
    });

    test('malformed bridge address fails closed with StateError', () async {
      final server = PhoneServer(openBridge: () => FakeBridge(startResult: 'garbage'));
      await expectLater(
        server.startPhoneServer(token: 'tok'),
        throwsA(isA<StateError>()),
      );
      expect(server.port, isNull);
    });

    test('double start throws StateError', () async {
      final server = PhoneServer(openBridge: () => FakeBridge());
      await server.startPhoneServer(token: 'tok');
      await expectLater(
        server.startPhoneServer(token: 'tok'),
        throwsA(isA<StateError>()),
      );
      await server.stopPhoneServer();
    });
  });

  group('PhoneServer lifecycle (fake bridge)', () {
    test('start returns the ephemeral port and records the token', () async {
      final bridge = FakeBridge();
      final server = PhoneServer(openBridge: () => bridge);
      final port = await server.startPhoneServer(token: 'pair-token');
      expect(port, 41233);
      expect(server.port, 41233);
      expect(bridge.startedTokens, ['pair-token']);
      await server.stopPhoneServer();
    });

    test('onPing emits one event per accepted ping nonce', () async {
      final bridge = FakeBridge();
      final server = PhoneServer(openBridge: () => bridge);
      await server.startPhoneServer(token: 'tok');
      bridge.queued.addAll(['nonce-a', 'nonce-b']);
      await expectLater(
        server.onPing,
        emitsInOrder(['nonce-a', 'nonce-b']),
      ).timeout(const Duration(seconds: 5));
      await server.stopPhoneServer();
    });

    test('stop is idempotent and clears the port', () async {
      final bridge = FakeBridge();
      final server = PhoneServer(openBridge: () => bridge);
      await server.startPhoneServer(token: 'tok');
      await server.stopPhoneServer();
      await server.stopPhoneServer();
      expect(server.port, isNull);
      expect(bridge.stopCalls, 1);
    });
  });

  group('PhoneServer stable identity', () {
    test('reuses stored PEMs via startWithCert', () async {
      final storage = FakeKeyValueStorage();
      final ids = PhoneIdentityStore(storage);
      await ids.save(
          const PhoneIdentity(certPem: 'c', keyPem: 'k'));
      final bridge = FakeBridge(
        startWithCertResult:
            '43333:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
      );
      final server = PhoneServer(openBridge: () => bridge);
      final port =
          await server.startPhoneServer(token: 'tok', identityStore: ids);
      expect(port, 43333);
      expect(bridge.startedWithCertTokens, ['tok']);
      expect(bridge.startedTokens, isEmpty);
      expect(server.fingerprint, 'b' * 64);
      await server.stopPhoneServer();
    });

    test('corrupt stored PEMs fall back to mint and persist', () async {
      final storage = FakeKeyValueStorage();
      final ids = PhoneIdentityStore(storage);
      await ids.save(
          const PhoneIdentity(certPem: 'bad', keyPem: 'bad'));
      // startWithCertResult null = bridge rejected the PEMs.
      final bridge = FakeBridge(startWithCertResult: null);
      final server = PhoneServer(openBridge: () => bridge);
      final port =
          await server.startPhoneServer(token: 'tok', identityStore: ids);
      expect(port, 41233);
      expect(bridge.startedTokens, ['tok']);
      final saved = await ids.load();
      expect(saved?.certPem, 'cert-pem');
      expect(saved?.keyPem, 'key-pem');
      await server.stopPhoneServer();
    });

    test('no stored identity mints and persists', () async {
      final storage = FakeKeyValueStorage();
      final ids = PhoneIdentityStore(storage);
      final bridge = FakeBridge();
      final server = PhoneServer(openBridge: () => bridge);
      await server.startPhoneServer(token: 'tok', identityStore: ids);
      expect(bridge.startedTokens, ['tok']);
      final saved = await ids.load();
      expect(saved?.certPem, 'cert-pem');
      await server.stopPhoneServer();
    });
  });

  group('PhoneIdentityStore', () {
    test('load returns null when absent, round-trips when saved', () async {
      final ids = PhoneIdentityStore(FakeKeyValueStorage());
      expect(await ids.load(), isNull);
      await ids.save(
          const PhoneIdentity(certPem: 'c', keyPem: 'k'));
      final loaded = await ids.load();
      expect(loaded?.certPem, 'c');
      expect(loaded?.keyPem, 'k');
    });

    test('load returns null on corrupt JSON or read failure', () async {
      final storage = FakeKeyValueStorage();
      storage.values['fuseitall_phone_identity'] = '{bad';
      expect(await PhoneIdentityStore(storage).load(), isNull);
      storage.failRead = true;
      expect(await PhoneIdentityStore(storage).load(), isNull);
    });
  });
}
