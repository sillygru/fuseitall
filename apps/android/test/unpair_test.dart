// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/connection/mac_locator.dart';
import 'package:fuseitall/features/device/device_info_provider.dart';
import 'package:fuseitall/features/pairing/pair_qr.dart';
import 'package:fuseitall/features/pairing/scan_qr_page.dart';
import 'package:fuseitall/features/ping/ping_page.dart';
import 'package:fuseitall/features/ping/proto_client.dart';
import 'package:fuseitall/net/go_server.dart';
import 'package:fuseitall/net/phone_identity_store.dart';
import 'package:fuseitall/result.dart';

import 'go_server_test.dart' show FakeBridge, FakeKeyValueStorage;

class _NullFacts implements DeviceFactsProvider {
  @override
  Future<DeviceFacts> currentFacts() async => const DeviceFacts();
}

PairQR _qr() => PairQR(
  v: 1,
  deviceName: 'Test Mac',
  platform: 'mac',
  host: '192.168.1.10',
  port: 18443,
  fingerprint: 'aa' * 32,
  pubkey: 'c3VwZXItc2VjcmV0LWtleQ==',
  token: 'abcdef0123456789abcdef0123456789',
  code: '123456',
);

Widget _wrap(Widget child) => MaterialApp(home: child);

void main() {
  // PingPage touches the real permissions channel (status refresh,
  // link-service start/stop); in tests it resolves to null (disabled).
  setUp(() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(
      const MethodChannel('fuseitall/permissions'),
      (call) async => null,
    );
  });

  group('mutual unpair', () {
    testWidgets('403 on announce revokes with a re-pair notice', (t) async {
      String? revoked;
      await t.pumpWidget(
        _wrap(
          PingPage(
            pairing: _qr(),
            onUnpair: () {},
            onRevoked: (msg) => revoked = msg,
            phoneServer: PhoneServer(openBridge: () => FakeBridge()),
            identityStore: PhoneIdentityStore(FakeKeyValueStorage()),
            deviceFacts: _NullFacts(),
            locator: MacLocator(FakeKeyValueStorage()),
            pingFn: (pairing, {replyPort, replyFingerprint, facts, filesPermission, photosPermission}) =>
                Future.value(
                  const Err<Pong>(AuthFailure('Mac rejected the token (403).')),
                ),
          ),
        ),
      );
      await t.pump();
      await t.pump(const Duration(milliseconds: 100));
      await t.pump();
      expect(revoked, contains('new QR'));
    });

    testWidgets('unpair via settings sends goodbye before wiping', (t) async {
      var unpaired = false;
      String? sentType;
      await t.pumpWidget(
        _wrap(
          PingPage(
            pairing: _qr(),
            onUnpair: () => unpaired = true,
            phoneServer: PhoneServer(openBridge: () => FakeBridge()),
            identityStore: PhoneIdentityStore(FakeKeyValueStorage()),
            deviceFacts: _NullFacts(),
            locator: MacLocator(FakeKeyValueStorage()),
            featureFn: (pairing, type, payload) {
              sentType = type;
              return Future.value(const Ok<String>('ack'));
            },
          ),
        ),
      );
      await t.pump();
      // HIG shell: Settings is a NavigationBar/Rail/Destination, not a gear tooltip.
      await t.tap(find.text('Settings').first);
      await t.pumpAndSettle();
      // Settings is a scrollable list; Unpair sits at the end.
      await t.scrollUntilVisible(find.text('Unpair Mac'), 300);
      await t.pumpAndSettle();
      expect(find.text('Unpair Mac'), findsOneWidget);
      await t.tap(find.text('Unpair Mac'));
      await t.pumpAndSettle();
      await t.tap(find.text('Unpair').last);
      await t.pumpAndSettle();
      expect(sentType, 'unpair');
      expect(unpaired, isTrue);
    });

    testWidgets('scan screen shows the pairing-changed notice', (t) async {
      await t.pumpWidget(
        _wrap(
          ScanQrPage(
            notice: 'This Mac unpaired FuseItAll. Scan its new QR.',
            onScanned: (_) {},
          ),
        ),
      );
      expect(find.byKey(const Key('pairingNotice')), findsOneWidget);
      expect(find.textContaining('Scan its new QR'), findsOneWidget);
    });

    test('unpair routes to /unpair', () {
      expect(featurePath('unpair'), '/unpair');
    });
  });
}
