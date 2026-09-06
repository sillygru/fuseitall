// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/device/device_info_provider.dart';
import 'package:fuseitall/features/ping/proto_client.dart';
import 'package:fuseitall/result.dart';
import 'package:fuseitall/version.dart';

const _nonce = '0123456789abcdef0123456789abcdef';

String _envelope(String type, Map<String, Object?> payload) => jsonEncode({
      'protocol_v': 1,
      'type': type,
      'sender': {'platform': 'mac', 'app_build': 1, 'min_peer_build': 1},
      'capabilities': ['ping'],
      'payload': payload,
      'future_unknown': 'ignored',
    });

void main() {
  group('buildPingEnvelope', () {
    test('has the frozen ping shape', () {
      final env = buildPingEnvelope(_nonce, sentAt: 1700000000);
      expect(env['protocol_v'], 1);
      expect(env['type'], 'ping');
      final sender = env['sender'] as Map;
      expect(sender['platform'], 'android');
      expect(sender['app_build'], isA<int>());
      expect(sender['min_peer_build'], isA<int>());
      expect(sender['app_version'], kAppVersion);
      expect((env['capabilities'] as List), contains('ping'));
      final payload = env['payload'] as Map;
      expect(payload['nonce'], _nonce);
      expect(payload['sent_at'], 1700000000);
    });

    test('reply_port is omitted unless provided', () {
      final env = buildPingEnvelope(_nonce, sentAt: 1700000000);
      expect((env['payload'] as Map).containsKey('reply_port'), isFalse);
    });

    test('reply_port is included when provided', () {
      final env = buildPingEnvelope(_nonce, sentAt: 1700000000, replyPort: 41233);
      expect((env['payload'] as Map)['reply_port'], 41233);
    });

    test('out-of-range reply_port throws', () {
      expect(() => buildPingEnvelope(_nonce, replyPort: 0), throwsArgumentError);
      expect(() => buildPingEnvelope(_nonce, replyPort: 65536), throwsArgumentError);
    });

    test('device facts are omitted unless provided', () {
      final payload = buildPingEnvelope(_nonce, sentAt: 1700000000)['payload'] as Map;
      expect(payload.containsKey('device_name'), isFalse);
      expect(payload.containsKey('model'), isFalse);
      expect(payload.containsKey('battery_pct'), isFalse);
      expect(payload.containsKey('charging'), isFalse);
    });

    test('device facts are included when provided', () {
      const facts = DeviceFacts(
        deviceName: 'OnePlus CPH2767',
        model: 'CPH2767',
        batteryPct: 78,
        charging: true,
      );
      final payload =
          buildPingEnvelope(_nonce, sentAt: 1700000000, facts: facts)['payload'] as Map;
      expect(payload['device_name'], 'OnePlus CPH2767');
      expect(payload['model'], 'CPH2767');
      expect(payload['battery_pct'], 78);
      expect(payload['charging'], isTrue);
    });

    test('blank names omitted, unknown battery omitted', () {
      const facts = DeviceFacts(deviceName: '  ', model: '', charging: true);
      final payload =
          buildPingEnvelope(_nonce, sentAt: 1700000000, facts: facts)['payload'] as Map;
      expect(payload.containsKey('device_name'), isFalse);
      expect(payload.containsKey('model'), isFalse);
      expect(payload.containsKey('battery_pct'), isFalse);
      expect(payload['charging'], isTrue);
    });

    test('out-of-range battery_pct throws', () {
      expect(
        () => buildPingEnvelope(_nonce,
            facts: const DeviceFacts(batteryPct: -1)),
        throwsArgumentError,
      );
      expect(
        () => buildPingEnvelope(_nonce,
            facts: const DeviceFacts(batteryPct: 101)),
        throwsArgumentError,
      );
    });
    test('newNonce is 128-bit hex and unique', () {
      final hex = RegExp(r'^[0-9a-f]{32}$');
      final a = newNonce();
      expect(hex.hasMatch(a), isTrue);
      expect(newNonce() == a, isFalse);
    });
  });

  group('parsePingResponse', () {
    test('426 maps to the verbatim update message', () {
      const msg = 'Update FuseItAll on mac to build >= 7';
      final r = parsePingResponse(
        statusCode: 426,
        body: _envelope('error', {
          'code': 'UPDATE_REQUIRED',
          'message': msg,
          'required_build': 7,
        }),
        expectedNonce: _nonce,
      );
      expect(r, isA<Err<Pong>>());
      final f = (r as Err<Pong>).failure;
      expect(f, isA<UpdateRequired>());
      expect(f.message, msg);
    });

    test('426 carries required/current versions when present', () {
      final r = parsePingResponse(
        statusCode: 426,
        body: _envelope('error', {
          'code': 'UPDATE_REQUIRED',
          'message': 'Update FuseItAll on mac to 0.2.0 (build >= 2); current 0.1.0',
          'required_build': 2,
          'required_version': '0.2.0',
          'current_version': '0.1.0',
          'device': 'mac',
        }),
        expectedNonce: _nonce,
      );
      final f = (r as Err<Pong>).failure as UpdateRequired;
      expect(f.requiredVersion, '0.2.0');
      expect(f.currentVersion, '0.1.0');
      expect(f.requiredBuild, 2);
      expect(f.device, 'mac');
    });

    test('version helpers map build 1 to 0.1.0', () {
      expect(kAppVersion, '0.1.0');
      expect(kAppBuild, 1);
      expect(appVersionForBuild(1), '0.1.0');
      expect(appVersionForBuild(9999), '');
    });

    test('426 with garbage body is still UpdateRequired', () {
      final r = parsePingResponse(
        statusCode: 426,
        body: 'not json',
        expectedNonce: _nonce,
      );
      expect((r as Err<Pong>).failure, isA<UpdateRequired>());
    });

    test('403 maps to an auth failure', () {
      final r = parsePingResponse(
        statusCode: 403,
        body: '{}',
        expectedNonce: _nonce,
      );
      expect((r as Err<Pong>).failure, isA<AuthFailure>());
    });

    test('other statuses map to network failures', () {
      final r = parsePingResponse(
        statusCode: 500,
        body: '{}',
        expectedNonce: _nonce,
      );
      expect((r as Err<Pong>).failure, isA<NetworkFailure>());
    });

    test('pong echoing the nonce round-trips', () {
      final r = parsePingResponse(
        statusCode: 200,
        body: _envelope('pong', {'nonce': _nonce, 'received_at': 1}),
        expectedNonce: _nonce,
      );
      expect(r, isA<Ok<Pong>>());
      expect((r as Ok<Pong>).value.nonce, _nonce);
    });

    test('pong with a different nonce fails closed', () {
      final r = parsePingResponse(
        statusCode: 200,
        body: _envelope('pong', {'nonce': 'ff' * 16}),
        expectedNonce: _nonce,
      );
      expect((r as Err<Pong>).failure, isA<NonceMismatch>());
    });

    test('pong missing nonce is a parse failure', () {
      final r = parsePingResponse(
        statusCode: 200,
        body: _envelope('pong', {}),
        expectedNonce: _nonce,
      );
      expect((r as Err<Pong>).failure, isA<ParseFailure>());
    });

    test('error envelope on 200 maps to update-required', () {
      const msg = 'Update FuseItAll on mac to build >= 3';
      final r = parsePingResponse(
        statusCode: 200,
        body: _envelope('error', {'code': 'UPDATE_REQUIRED', 'message': msg}),
        expectedNonce: _nonce,
      );
      final f = (r as Err<Pong>).failure;
      expect(f, isA<UpdateRequired>());
      expect(f.message, msg);
    });
  });
}
