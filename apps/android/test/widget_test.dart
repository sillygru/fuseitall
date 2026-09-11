// SPDX-License-Identifier: AGPL-3.0-only

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/device/device_info_provider.dart';
import 'package:fuseitall/features/pairing/confirm_fingerprint_page.dart';
import 'package:fuseitall/features/pairing/pair_qr.dart';
import 'package:fuseitall/features/ping/ping_page.dart';
import 'package:fuseitall/features/ping/proto_client.dart';
import 'package:flutter/services.dart';
import 'package:fuseitall/net/go_server.dart';
import 'package:fuseitall/net/phone_identity_store.dart';
import 'package:fuseitall/net/phone_websocket.dart';
import 'package:fuseitall/net/wifi_bind.dart';
import 'package:fuseitall/result.dart';

import 'go_server_test.dart' show FakeBridge, FakeKeyValueStorage;

/// Disconnected-socket stand-in: fastConnect returns null immediately with
/// no stagger timers, so widget tests stay hermetic (no real LAN dials, no
/// pending-timer flakes) and exercise the HTTP pingFn path.
class _DisconnectedWs extends PhoneWebSocket {
  _DisconnectedWs({required super.pairing});
  @override
  Future<String?> fastConnect(List<String> candidates, int port) async => null;
}

/// Wi-Fi bind stand-in: records calls without touching platform channels.
class _FakeWifiBind extends WifiBind {
  _FakeWifiBind() : super(const MethodChannel('test/wifibind'));
  var binds = 0;
  var unbinds = 0;
  var bindResult = true;
  @override
  Future<bool> bindWifi({Duration timeout = const Duration(seconds: 8)}) async {
    binds++;
    return bindResult;
  }

  @override
  Future<void> unbindWifi() async {
    unbinds++;
  }
}

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
          phoneWebSocket: _DisconnectedWs(pairing: _qr()),
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
          phoneWebSocket: _DisconnectedWs(pairing: _qr()),
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
          phoneWebSocket: _DisconnectedWs(pairing: _qr()),
          identityStore:
              PhoneIdentityStore(FakeKeyValueStorage()),
          deviceFacts: _NullFacts(),
          pingFn: (pairing, {replyPort, replyFingerprint, facts, filesPermission, photosPermission}) {
            seenReplyPort = replyPort;
            return Future.value(const Err<Pong>(UpdateRequired(msg)));
          },
        )),
      );
      await t.pump();
      // Presence announce fires on server start — no manual ping button.
      await t.pump(const Duration(milliseconds: 100));
      await t.pump();
      expect(seenReplyPort, 41233);
      // Verbatim: the banner's SelectableText.rich must carry the exact
      // server message (inspected via the span tree, not a text finder).
      final banner =
          t.widget<SelectableText>(find.byKey(const Key('updateBannerText')));
      expect(banner.textSpan!.toPlainText(), contains(msg));
    });

    testWidgets('announce fires pingFn with the phone port', (t) async {
      int calls = 0;
      int? seenReplyPort;
      await t.pumpWidget(
        MaterialApp(
            home: PingPage(
          pairing: _qr(),
          onUnpair: () {},
          phoneServer: PhoneServer(openBridge: () => FakeBridge()),
          phoneWebSocket: _DisconnectedWs(pairing: _qr()),
          identityStore:
              PhoneIdentityStore(FakeKeyValueStorage()),
          deviceFacts: _NullFacts(),
          pingFn: (pairing, {replyPort, replyFingerprint, facts, filesPermission, photosPermission}) {
            calls++;
            seenReplyPort = replyPort;
            return Future.value(
              const Ok<Pong>(Pong(nonce: 'hb', receivedAt: 0)),
            );
          },
        )),
      );
      await t.pump();
      await t.pump(const Duration(milliseconds: 300));
      expect(calls, greaterThan(0));
      expect(seenReplyPort, 41233);
    });

    testWidgets('announce network failure surfaces a reachability card, never an update banner',
        (t) async {
      await t.pumpWidget(
        MaterialApp(
            home: PingPage(
          pairing: _qr(),
          onUnpair: () {},
          phoneServer: PhoneServer(openBridge: () => FakeBridge()),
          phoneWebSocket: _DisconnectedWs(pairing: _qr()),
          identityStore:
              PhoneIdentityStore(FakeKeyValueStorage()),
          deviceFacts: _NullFacts(),
          pingFn: (pairing, {replyPort, replyFingerprint, facts, filesPermission, photosPermission}) =>
              Future.value(
            const Err<Pong>(NetworkFailure('nope')),
          ),
        )),
      );
      await t.pump();
      await t.pump(const Duration(seconds: 2));
      await t.pump();
      // No version banner for a network failure, and no manual "Ping failed"
      // title — but the first-pair reachability card must appear so the
      // phone never sits vaguely offline while the Mac stays on its QR.
      expect(find.byKey(const Key('updateBannerText')), findsNothing);
      expect(find.text('Ping failed.'), findsNothing);
      expect(find.textContaining('reach your Mac'), findsOneWidget);
    });

    testWidgets('errno 113 surfaces the no-route card with the tried host',
        (t) async {
      // Bind declined: straight to the loud card (the bind-then-redial path
      // has its own test below; the real channel never resolves in widget
      // tests, so hermetic tests always inject a fake bind).
      final wifi = _FakeWifiBind()..bindResult = false;
      await t.pumpWidget(
        MaterialApp(
            home: PingPage(
          pairing: _qr(),
          onUnpair: () {},
          phoneServer: PhoneServer(openBridge: () => FakeBridge()),
          phoneWebSocket: _DisconnectedWs(pairing: _qr()),
          wifiBind: wifi,
          identityStore:
              PhoneIdentityStore(FakeKeyValueStorage()),
          deviceFacts: _NullFacts(),
          pingFn: (pairing, {replyPort, replyFingerprint, facts, filesPermission, photosPermission}) =>
              Future.value(
            Err<Pong>(NoRouteFailure(
                'No route to ${pairing.host}:${pairing.port} (errno 113).')),
          ),
        )),
      );
      await t.pump();
      await t.pump(const Duration(seconds: 2));
      await t.pump();
      expect(find.byKey(const Key('updateBannerText')), findsNothing);
      expect(find.text('Ping failed.'), findsNothing);
      expect(find.textContaining('No route to 192.168.1.10:18443'), findsOneWidget);
      expect(find.textContaining('mobile data'), findsOneWidget);
    });

    testWidgets('errno 113 binds Wi-Fi once, then shows the no-route card',
        (t) async {
      final wifi = _FakeWifiBind();
      await t.pumpWidget(
        MaterialApp(
            home: PingPage(
          pairing: _qr(),
          onUnpair: () {},
          phoneServer: PhoneServer(openBridge: () => FakeBridge()),
          phoneWebSocket: _DisconnectedWs(pairing: _qr()),
          wifiBind: wifi,
          identityStore:
              PhoneIdentityStore(FakeKeyValueStorage()),
          deviceFacts: _NullFacts(),
          pingFn: (pairing, {replyPort, replyFingerprint, facts, filesPermission, photosPermission}) =>
              Future.value(
            Err<Pong>(NoRouteFailure(
                'No route to ${pairing.host}:${pairing.port} (errno 113).')),
          ),
        )),
      );
      await t.pump();
      await t.pump(const Duration(seconds: 2));
      await t.pump();
      // One bind attempt for first-pair 113, then the loud card (the redial
      // also fails in this hermetic setup — no real LAN).
      expect(wifi.binds, 1);
      expect(find.textContaining('No route to 192.168.1.10:18443'), findsOneWidget);
    });

    testWidgets('errno 111 surfaces the refused card suggesting a re-scan',
        (t) async {
      await t.pumpWidget(
        MaterialApp(
            home: PingPage(
          pairing: _qr(),
          onUnpair: () {},
          phoneServer: PhoneServer(openBridge: () => FakeBridge()),
          phoneWebSocket: _DisconnectedWs(pairing: _qr()),
          identityStore:
              PhoneIdentityStore(FakeKeyValueStorage()),
          deviceFacts: _NullFacts(),
          pingFn: (pairing, {replyPort, replyFingerprint, facts, filesPermission, photosPermission}) =>
              Future.value(
            Err<Pong>(RefusedFailure(
                'Connection refused by ${pairing.host}:${pairing.port} (errno 111).')),
          ),
        )),
      );
      await t.pump();
      await t.pump(const Duration(seconds: 2));
      await t.pump();
      expect(find.byKey(const Key('updateBannerText')), findsNothing);
      expect(find.textContaining('Connection refused by 192.168.1.10:18443'), findsOneWidget);
      expect(find.textContaining('scan its current QR'), findsOneWidget);
    });

    testWidgets('timeout surfaces the filtered-path card', (t) async {
      // Bind declined: straight to the loud card (see the 113 bind test).
      final wifi = _FakeWifiBind()..bindResult = false;
      await t.pumpWidget(
        MaterialApp(
            home: PingPage(
          pairing: _qr(),
          onUnpair: () {},
          phoneServer: PhoneServer(openBridge: () => FakeBridge()),
          phoneWebSocket: _DisconnectedWs(pairing: _qr()),
          wifiBind: wifi,
          identityStore:
              PhoneIdentityStore(FakeKeyValueStorage()),
          deviceFacts: _NullFacts(),
          pingFn: (pairing, {replyPort, replyFingerprint, facts, filesPermission, photosPermission}) =>
              Future.value(
            const Err<Pong>(TimeoutFailure('Ping timed out after 10s.')),
          ),
        )),
      );
      await t.pump();
      await t.pump(const Duration(seconds: 2));
      await t.pump();
      expect(find.byKey(const Key('updateBannerText')), findsNothing);
      expect(find.textContaining('No answer from 192.168.1.10:18443'), findsOneWidget);
      expect(find.textContaining('client-isolation'), findsOneWidget);
    });

    testWidgets('device facts reach pingFn via announce', (t) async {
      DeviceFacts? seenFacts;
      await t.pumpWidget(
        MaterialApp(
            home: PingPage(
          pairing: _qr(),
          onUnpair: () {},
          phoneServer: PhoneServer(openBridge: () => FakeBridge()),
          phoneWebSocket: _DisconnectedWs(pairing: _qr()),
          identityStore:
              PhoneIdentityStore(FakeKeyValueStorage()),
          deviceFacts: _FakeFacts(),
          pingFn: (pairing, {replyPort, replyFingerprint, facts, filesPermission, photosPermission}) {
            seenFacts = facts;
            return Future.value(
              const Ok<Pong>(Pong(nonce: 'n', receivedAt: 0)),
            );
          },
        )),
      );
      await t.pump();
      await t.pump(const Duration(milliseconds: 100));
      expect(seenFacts?.deviceName, 'Test Phone');
      expect(seenFacts?.model, 'T1');
      expect(seenFacts?.batteryPct, 55);
      expect(seenFacts?.charging, isTrue);
    });

    testWidgets('throwing facts provider never breaks presence announce', (t) async {
      var called = false;
      await t.pumpWidget(
        MaterialApp(
            home: PingPage(
          pairing: _qr(),
          onUnpair: () {},
          phoneServer: PhoneServer(openBridge: () => FakeBridge()),
          phoneWebSocket: _DisconnectedWs(pairing: _qr()),
          identityStore:
              PhoneIdentityStore(FakeKeyValueStorage()),
          deviceFacts: _ThrowingFacts(),
          pingFn: (pairing, {replyPort, replyFingerprint, facts, filesPermission, photosPermission}) {
            called = true;
            expect(facts, isNull);
            return Future.value(
              const Ok<Pong>(Pong(nonce: 'n', receivedAt: 0)),
            );
          },
        )),
      );
      await t.pump();
      await t.pump(const Duration(milliseconds: 100));
      expect(called, isTrue);
    });
  });
}
