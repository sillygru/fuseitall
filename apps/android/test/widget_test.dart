// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/device/device_info_provider.dart';
import 'package:fuseitall/features/pairing/confirm_fingerprint_page.dart';
import 'package:fuseitall/features/pairing/pair_qr.dart';
import 'package:fuseitall/features/ping/ping_page.dart';
import 'package:fuseitall/features/ping/proto_client.dart';
import 'package:fuseitall/net/go_server.dart';
import 'package:fuseitall/net/phone_identity_store.dart';
import 'package:fuseitall/result.dart';

import 'go_server_test.dart' show FakeBridge, FakeKeyValueStorage;

class _FakeFacts implements DeviceFactsProvider {
  @override
  Future<DeviceFacts> currentFacts() async => const DeviceFacts(
        deviceName: 'Test Phone',
        model: 'T1',
        batteryPct: 55,
        charging: true,
      );
}

class _ThrowingFacts implements DeviceFactsProvider {
  @override
  Future<DeviceFacts> currentFacts() async => throw StateError('nope');
}

/// Hermetic default: real plugins pend under fake-async, so tests that do
/// not care about facts inject this (mirrors the phoneServer/identityStore
/// seam pattern).
class _NullFacts implements DeviceFactsProvider {
  @override
  Future<DeviceFacts> currentFacts() async => const DeviceFacts();
}

PairQR _qr({String code = '123456'}) => PairQR(
      v: 1,
      deviceName: 'Test Mac',
      platform: 'mac',
      host: '192.168.1.10',
      port: 18443,
      fingerprint: 'aa' * 32,
      pubkey: 'c3VwZXItc2VjcmV0LWtleQ==',
      token: 'abcdef0123456789abcdef0123456789',
      code: code,
    );

Widget _wrap(Widget child) => MaterialApp(home: child);

void main() {
  group('ConfirmFingerprintPage (6-digit code)', () {
    testWidgets('correct code confirms', (t) async {
      var confirmed = false;
      await t.pumpWidget(_wrap(ConfirmFingerprintPage(
        pairing: _qr(),
        onConfirmed: () => confirmed = true,
        onBack: () {},
      )));
      await t.enterText(find.byType(TextField), '123456');
      await t.tap(find.text('Confirm and pair'));
      await t.pump();
      expect(confirmed, isTrue);
    });

    testWidgets('wrong code shows retry message, no STOPPED block',
        (t) async {
      await t.pumpWidget(_wrap(ConfirmFingerprintPage(
        pairing: _qr(),
        onConfirmed: () {},
        onBack: () {},
      )));
      await t.enterText(find.byType(TextField), '000000');
      await t.tap(find.text('Confirm and pair'));
      await t.pump();
      expect(find.textContaining("doesn't match"), findsOneWidget);
      expect(find.textContaining('Test Mac'), findsWidgets);
      expect(find.textContaining('STOPPED'), findsNothing);
      expect(find.textContaining('impersonator'), findsNothing);
    });

    testWidgets('empty submit shows inline hint, not the STOPPED block',
        (t) async {
      await t.pumpWidget(_wrap(ConfirmFingerprintPage(
        pairing: _qr(),
        onConfirmed: () {},
        onBack: () {},
      )));
      await t.tap(find.text('Confirm and pair'));
      await t.pump();
      expect(
        find.text('Enter the 6-digit code shown on your Mac'),
        findsOneWidget,
      );
      expect(find.textContaining('STOPPED'), findsNothing);
    });

    testWidgets('missing code shows old-Mac update message', (t) async {
      await t.pumpWidget(_wrap(ConfirmFingerprintPage(
        pairing: _qr(code: ''),
        onConfirmed: () {},
        onBack: () {},
      )));
      expect(
        find.textContaining('This Mac is on an old build'),
        findsOneWidget,
      );
      expect(find.byType(TextField), findsNothing);
    });

    testWidgets('full fingerprint shown read-only, code field is numeric',
        (t) async {
      await t.pumpWidget(_wrap(ConfirmFingerprintPage(
        pairing: _qr(),
        onConfirmed: () {},
        onBack: () {},
      )));
      expect(find.textContaining('aa' * 32), findsOneWidget);
      final field = t.widget<TextField>(find.byType(TextField));
      expect(field.keyboardType, TextInputType.number);
      expect(field.maxLength, 6);
    });

    testWidgets('save failure shows verbatim error, stays on screen',
        (t) async {
      var confirmed = false;
      await t.pumpWidget(_wrap(ConfirmFingerprintPage(
        pairing: _qr(),
        onConfirmed: () => confirmed = true,
        onBack: () {},
        saveError: 'Could not save pairing: keystore locked',
      )));
      expect(
        find.text('Could not save pairing: keystore locked'),
        findsOneWidget,
      );
      expect(confirmed, isFalse);
      expect(find.byType(TextField), findsOneWidget);
    });
  });

  group('PingPage', () {
    testWidgets('shows connected hero, no manual ping button, no banner initially',
        (t) async {
      await t.pumpWidget(
        MaterialApp(
            home: PingPage(
          pairing: _qr(),
          onUnpair: () {},
          phoneServer: PhoneServer(openBridge: () => FakeBridge()),
          identityStore:
              PhoneIdentityStore(FakeKeyValueStorage()),
          deviceFacts: _NullFacts(),
        )),
      );
      expect(find.text('Send ping to Mac'), findsNothing);
      expect(find.text('Latency log'), findsNothing);
      expect(find.textContaining('Update required'), findsNothing);
      // HIG shell: NavigationBar with 3 labeled destinations.
      expect(find.text('Home'), findsOneWidget);
      expect(find.text('Devices'), findsOneWidget);
      expect(find.text('Settings'), findsWidgets);
      expect(find.text('Send Clipboard'), findsOneWidget);
    });

    testWidgets('phone server port is shown once started', (t) async {
      await t.pumpWidget(
        MaterialApp(
            home: PingPage(
          pairing: _qr(),
          onUnpair: () {},
          phoneServer: PhoneServer(openBridge: () => FakeBridge()),
          identityStore:
              PhoneIdentityStore(FakeKeyValueStorage()),
          deviceFacts: _NullFacts(),
        )),
      );
      await t.pump();
      // Port subtitle removed per UX — should not show raw port.
      expect(
        find.text('Phone server listening on port 41233'),
        findsNothing,
      );
    });

    testWidgets('sends reply_port and shows the 426 message verbatim',
        (t) async {
      const msg = 'Update FuseItAll on mac to build >= 7';
      int? seenReplyPort;
      await t.pumpWidget(
        MaterialApp(
            home: PingPage(
          pairing: _qr(),
          onUnpair: () {},
          phoneServer: PhoneServer(openBridge: () => FakeBridge()),
          identityStore:
              PhoneIdentityStore(FakeKeyValueStorage()),
          deviceFacts: _NullFacts(),
          pingFn: (pairing, {replyPort, replyFingerprint, facts}) {
            seenReplyPort = replyPort;
            return Future.value(const Err<Pong>(UpdateRequired(msg)));
          },
        )),
      );
      await t.pump();
      // Announce heartbeat fires on server start — no manual ping button.
      await t.pump(const Duration(milliseconds: 100));
      await t.pump();
      expect(seenReplyPort, 41233);
      // Verbatim: the banner's SelectableText.rich must carry the exact
      // server message (inspected via the span tree, not a text finder).
      final banner =
          t.widget<SelectableText>(find.byKey(const Key('updateBannerText')));
      expect(banner.textSpan!.toPlainText(), contains(msg));
    });

    testWidgets('heartbeat fires pingFn with the phone port', (t) async {
      int calls = 0;
      int? seenReplyPort;
      await t.pumpWidget(
        MaterialApp(
            home: PingPage(
          pairing: _qr(),
          onUnpair: () {},
          phoneServer: PhoneServer(openBridge: () => FakeBridge()),
          identityStore:
              PhoneIdentityStore(FakeKeyValueStorage()),
          deviceFacts: _NullFacts(),
          pingFn: (pairing, {replyPort, replyFingerprint, facts}) {
            calls++;
            seenReplyPort = replyPort;
            return Future.value(
              const Ok<Pong>(Pong(nonce: 'hb', receivedAt: 0)),
            );
          },
          heartbeatInterval: const Duration(milliseconds: 50),
        )),
      );
      await t.pump();
      await t.pump(const Duration(milliseconds: 300));
      expect(calls, greaterThan(0));
      expect(seenReplyPort, 41233);
    });

    testWidgets('heartbeat failures only log, never banner or error block',
        (t) async {
      await t.pumpWidget(
        MaterialApp(
            home: PingPage(
          pairing: _qr(),
          onUnpair: () {},
          phoneServer: PhoneServer(openBridge: () => FakeBridge()),
          identityStore:
              PhoneIdentityStore(FakeKeyValueStorage()),
          deviceFacts: _NullFacts(),
          pingFn: (pairing, {replyPort, replyFingerprint, facts}) =>
              Future.value(
            const Err<Pong>(NetworkFailure('nope')),
          ),
          heartbeatInterval: const Duration(milliseconds: 50),
        )),
      );
      await t.pump();
      await t.pump(const Duration(milliseconds: 300));
      expect(find.byKey(const Key('updateBannerText')), findsNothing);
      expect(find.textContaining('Ping failed'), findsNothing);
    });

    testWidgets('device facts reach pingFn via heartbeat', (t) async {
      DeviceFacts? seenFacts;
      await t.pumpWidget(
        MaterialApp(
            home: PingPage(
          pairing: _qr(),
          onUnpair: () {},
          phoneServer: PhoneServer(openBridge: () => FakeBridge()),
          identityStore:
              PhoneIdentityStore(FakeKeyValueStorage()),
          deviceFacts: _FakeFacts(),
          pingFn: (pairing, {replyPort, replyFingerprint, facts}) {
            seenFacts = facts;
            return Future.value(
              const Ok<Pong>(Pong(nonce: 'n', receivedAt: 0)),
            );
          },
          heartbeatInterval: const Duration(milliseconds: 50),
        )),
      );
      await t.pump();
      await t.pump(const Duration(milliseconds: 100));
      expect(seenFacts?.deviceName, 'Test Phone');
      expect(seenFacts?.model, 'T1');
      expect(seenFacts?.batteryPct, 55);
      expect(seenFacts?.charging, isTrue);
    });

    testWidgets('throwing facts provider never breaks the heartbeat', (t) async {
      var called = false;
      await t.pumpWidget(
        MaterialApp(
            home: PingPage(
          pairing: _qr(),
          onUnpair: () {},
          phoneServer: PhoneServer(openBridge: () => FakeBridge()),
          identityStore:
              PhoneIdentityStore(FakeKeyValueStorage()),
          deviceFacts: _ThrowingFacts(),
          pingFn: (pairing, {replyPort, replyFingerprint, facts}) {
            called = true;
            expect(facts, isNull);
            return Future.value(
              const Ok<Pong>(Pong(nonce: 'n', receivedAt: 0)),
            );
          },
          heartbeatInterval: const Duration(milliseconds: 50),
        )),
      );
      await t.pump();
      await t.pump(const Duration(milliseconds: 100));
      expect(called, isTrue);
    });
  });
}
