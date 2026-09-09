// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/device/battery_watcher.dart';
import 'package:fuseitall/features/device/device_info_provider.dart';
import 'package:fuseitall/features/pairing/pair_qr.dart';
import 'package:fuseitall/features/ping/ping_page.dart';
import 'package:fuseitall/features/ping/proto_client.dart';
import 'package:fuseitall/net/go_server.dart';
import 'package:fuseitall/net/phone_identity_store.dart';
import 'package:fuseitall/net/phone_websocket.dart';
import 'package:fuseitall/result.dart';

import 'go_server_test.dart' show FakeBridge, FakeKeyValueStorage;

class _NullFacts implements DeviceFactsProvider {
  @override
  Future<DeviceFacts> currentFacts() async => const DeviceFacts();
}

class _StreamBattery implements BatteryReadings {
  _StreamBattery(this.stream);
  final Stream<BatteryReading> stream;
  @override
  Stream<BatteryReading> get readings => stream;
}

/// Connected-socket stand-in: records envelopes instead of dialing.
/// (testWidgets mocks HttpClient, so real loopback sockets are unusable.)
class _FakeWs extends PhoneWebSocket {
  _FakeWs({required super.pairing, required this.sent});
  final List<Map<String, dynamic>> sent;
  @override
  bool get isConnected => true;
  @override
  Future<bool> sendEnvelope(Map<String, dynamic> env) async {
    sent.add(Map<String, dynamic>.from(env));
    return true;
  }
}

PairQR _qr({String host = '192.168.1.10', int port = 18443}) => PairQR(
      v: 1,
      deviceName: 'Test Mac',
      platform: 'mac',
      host: host,
      port: port,
      fingerprint: 'aa' * 32,
      pubkey: 'c3VwZXItc2VjcmV0LWtleQ==',
      token: 'abcdef0123456789abcdef0123456789',
      code: '123456',
    );

Future<void> _emitBattery(Object? event) {
  final data = const StandardMethodCodec().encodeSuccessEnvelope(event);
  return TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
      .handlePlatformMessage('fuseitall/battery', data, (_) {});
}

void main() {
  group('parseBatteryEvent', () {
    test('valid map parses', () {
      expect(
        parseBatteryEvent({'battery_pct': 80, 'charging': false}),
        const BatteryReading(pct: 80, charging: false),
      );
      expect(
        parseBatteryEvent({'battery_pct': 0, 'charging': true}),
        const BatteryReading(pct: 0, charging: true),
      );
      expect(
        parseBatteryEvent({'battery_pct': 100, 'charging': true}),
        const BatteryReading(pct: 100, charging: true),
      );
    });

    test('malformed events are null, never throw', () {
      expect(parseBatteryEvent(null), isNull);
      expect(parseBatteryEvent('junk'), isNull);
      expect(parseBatteryEvent(42), isNull);
      expect(parseBatteryEvent(const []), isNull);
      expect(parseBatteryEvent(const {}), isNull);
      expect(parseBatteryEvent({'charging': true}), isNull);
      expect(parseBatteryEvent({'battery_pct': 50}), isNull);
      expect(parseBatteryEvent({'battery_pct': -1, 'charging': false}), isNull);
      expect(parseBatteryEvent({'battery_pct': 101, 'charging': false}), isNull);
      expect(
          parseBatteryEvent({'battery_pct': '80', 'charging': false}), isNull);
      expect(parseBatteryEvent({'battery_pct': 80, 'charging': 1}), isNull);
      expect(parseBatteryEvent({'battery_pct': 80.0, 'charging': false}),
          isNull);
    });
  });

  group('BatteryReading', () {
    test('value equality', () {
      expect(const BatteryReading(pct: 1, charging: true),
          const BatteryReading(pct: 1, charging: true));
      expect(const BatteryReading(pct: 1, charging: true),
          isNot(const BatteryReading(pct: 2, charging: true)));
      expect(const BatteryReading(pct: 1, charging: true),
          isNot(const BatteryReading(pct: 1, charging: false)));
      expect(const BatteryReading(pct: 1, charging: true).hashCode,
          const BatteryReading(pct: 1, charging: true).hashCode);
    });
  });

  group('BatteryWatcher stream', () {
    TestWidgetsFlutterBinding.ensureInitialized();

    setUp(() {
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(
        const MethodChannel('fuseitall/battery'),
        (call) async => null,
      );
    });

    tearDown(() {
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(
        const MethodChannel('fuseitall/battery'),
        null,
      );
    });

    test('distinct valid readings pass, malformed skipped', () async {
      final seen = <BatteryReading>[];
      final sub = BatteryWatcher().readings.listen(seen.add);
      await _emitBattery({'battery_pct': 80, 'charging': false});
      await _emitBattery({'battery_pct': 80, 'charging': false});
      await _emitBattery({'battery_pct': 79, 'charging': false});
      await _emitBattery({'battery_pct': 200, 'charging': false});
      await _emitBattery('junk');
      await _emitBattery({'battery_pct': 79, 'charging': true});
      await Future<void>.delayed(const Duration(milliseconds: 50));
      await sub.cancel();
      expect(seen, const [
        BatteryReading(pct: 80, charging: false),
        BatteryReading(pct: 79, charging: false),
        BatteryReading(pct: 79, charging: true),
      ]);
    });
  });

  group('PingPage battery pushes', () {
    testWidgets('reading while offline caches instead of dialing',
        (t) async {
      var calls = 0;
      final battery = StreamController<BatteryReading>();
      await t.pumpWidget(
        MaterialApp(
            home: PingPage(
          pairing: _qr(),
          onUnpair: () {},
          phoneServer: PhoneServer(openBridge: () => FakeBridge()),
          identityStore: PhoneIdentityStore(FakeKeyValueStorage()),
          deviceFacts: _NullFacts(),
          batteryWatcher: _StreamBattery(battery.stream),
          pingFn: (pairing,
              {replyPort,
              replyFingerprint,
              facts,
              filesPermission,
              photosPermission}) {
            calls++;
            return Future.value(
              const Ok<Pong>(Pong(nonce: 'n', receivedAt: 0)),
            );
          },
        )),
      );
      await t.pump();
      await t.pump(const Duration(milliseconds: 300));
      final baseline = calls;
      expect(baseline, greaterThan(0));
      battery.add(const BatteryReading(pct: 62, charging: false));
      await t.pump();
      await t.pump(const Duration(milliseconds: 300));
      expect(calls, baseline);
      await battery.close();
    });

    testWidgets('reading over a live websocket sends a battery-only ping',
        (t) async {
      final pairing = _qr();
      final sent = <Map<String, dynamic>>[];
      final battery = StreamController<BatteryReading>();
      var httpPings = 0;
      await t.pumpWidget(
        MaterialApp(
            home: PingPage(
          pairing: pairing,
          onUnpair: () {},
          phoneServer: PhoneServer(openBridge: () => FakeBridge()),
          identityStore: PhoneIdentityStore(FakeKeyValueStorage()),
          deviceFacts: _NullFacts(),
          batteryWatcher: _StreamBattery(battery.stream),
          phoneWebSocket: _FakeWs(pairing: pairing, sent: sent),
          pingFn: (pairing,
              {replyPort,
              replyFingerprint,
              facts,
              filesPermission,
              photosPermission}) {
            httpPings++;
            return Future.value(
              const Ok<Pong>(Pong(nonce: 'n', receivedAt: 0)),
            );
          },
        )),
      );
      await t.pump();
      battery.add(const BatteryReading(pct: 61, charging: true));
      await t.pump();
      await t.pump(const Duration(milliseconds: 100));
      final pushes = sent.where((env) {
        final payload = env['payload'];
        return env['type'] == 'ping' &&
            payload is Map &&
            payload['battery_pct'] == 61;
      }).toList();
      expect(pushes, hasLength(1));
      final payload = pushes.single['payload'] as Map;
      expect(payload['charging'], isTrue);
      // Battery-only: identity keys stay absent so the Mac keeps its own.
      expect(payload.containsKey('device_name'), isFalse);
      expect(payload.containsKey('model'), isFalse);
      // Preferred transport is the live socket: no HTTP fallback dial.
      expect(httpPings, 0);
      await battery.close();
    });
  });
}
