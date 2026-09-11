// SPDX-License-Identifier: AGPL-3.0-only

// ignore_for_file: unused_element_parameter

import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/connection/mac_locator.dart';
import 'package:fuseitall/features/device/device_info_provider.dart';
import 'package:fuseitall/features/pairing/pair_qr.dart';
import 'package:fuseitall/features/ping/proto_client.dart';
import 'package:fuseitall/net/phone_transport.dart';
import 'package:fuseitall/result.dart';

import 'go_server_test.dart' show FakeKeyValueStorage;

const _qr = PairQR(
  v: 1,
  deviceName: 'Test Mac',
  platform: 'macos',
  host: '192.168.1.2',
  port: 18789,
  fingerprint: 'aa',
  pubkey: 'c3VwZXItc2VjcmV0LWtleQ==',
  token: 'abcdef0123456789abcdef0123456789',
  code: '123456',
  candidates: ['192.168.1.3'],
);

Future<Result<Pong>> _ok(
  PairQR pairing, {
  int? replyPort,
  String? replyFingerprint,
  DeviceFacts? facts,
  String? filesPermission,
  String? photosPermission,
}) =>
    Future.value(const Ok(Pong(nonce: 'n', receivedAt: 0)));

Future<Result<String>> _okFeature(
  PairQR pairing,
  String type,
  Map<String, Object?> payload,
) =>
    Future.value(const Ok('ok'));

void main() {
  group('pingWithFallback beacon order', () {
    test('beacon sender IPs lead the QR primary', () async {
      final tried = <String>[];
      final transport = PhoneTransport(
        base: _qr,
        locator: MacLocator(FakeKeyValueStorage()),
        pingFn: (
          pairing, {
          replyPort,
          replyFingerprint,
          facts,
          filesPermission,
          photosPermission,
        }) {
          tried.add(pairing.host);
          return _ok(pairing);
        },
        featureFn: _okFeature,
      );
      final (:result, :winner) = await transport.pingWithFallback(
        rememberedHosts: const ['192.168.1.9'],
        beaconHosts: const ['10.0.0.5'],
      );
      expect(result, isA<Ok<Pong>>());
      expect(winner, '10.0.0.5');
      expect(tried.first, '10.0.0.5');
    });

    test('without beacons the QR primary still leads', () async {
      final tried = <String>[];
      final transport = PhoneTransport(
        base: _qr,
        locator: MacLocator(FakeKeyValueStorage()),
        pingFn: (
          pairing, {
          replyPort,
          replyFingerprint,
          facts,
          filesPermission,
          photosPermission,
        }) {
          tried.add(pairing.host);
          return _ok(pairing);
        },
        featureFn: _okFeature,
      );
      final (:result, :winner) = await transport.pingWithFallback(
        rememberedHosts: const ['192.168.1.9'],
      );
      expect(result, isA<Ok<Pong>>());
      expect(winner, '192.168.1.2');
      expect(tried, ['192.168.1.2']);
    });

    test('duplicate beacon hosts are tried once', () async {
      final tried = <String>[];
      final transport = PhoneTransport(
        base: _qr,
        locator: MacLocator(FakeKeyValueStorage()),
        pingFn: (
          pairing, {
          replyPort,
          replyFingerprint,
          facts,
          filesPermission,
          photosPermission,
        }) {
          tried.add(pairing.host);
          if (pairing.host == '192.168.1.2') {
            return Future.value(
              const Err<Pong>(NetworkFailure('refused')),
            );
          }
          return _ok(pairing);
        },
        featureFn: _okFeature,
      );
      final (:result, :winner) = await transport.pingWithFallback(
        rememberedHosts: const ['192.168.1.2'],
        beaconHosts: const ['192.168.1.2', '10.0.0.5'],
      );
      expect(result, isA<Ok<Pong>>());
      expect(winner, '10.0.0.5');
      // The beacon dup of the failed primary is not redialed.
      expect(tried, ['192.168.1.2', '10.0.0.5']);
    });
  });
}
