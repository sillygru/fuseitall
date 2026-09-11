// SPDX-License-Identifier: AGPL-3.0-only

import 'dart:convert';
import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/connection/beacon_listener.dart';
import 'package:fuseitall/features/pairing/pair_qr.dart';

void main() {
  final pairing = PairQR(
    v: 1,
    deviceName: 'MacBook Pro',
    platform: 'mac',
    host: '192.168.1.100',
    port: 18789,
    fingerprint: 'aa' * 32,
    pubkey: 'key',
    token: 'tok',
    code: '123456',
  );

  test('BeaconListener parses valid matching beacon and fires callback', () {
    String? seenHost;
    int? seenPort;

    final listener = BeaconListener(
      pairing: pairing,
      onMacDiscovered: (host, port) {
        seenHost = host;
        seenPort = port;
      },
    );

    final payload = jsonEncode({
      'proto': 'fuseitall-beacon-v1',
      'host': '192.168.1.200',
      'port': 18789,
      'fp': 'aa' * 32,
      'ts': 1700000000,
    });

    listener.handleDatagramForTesting(utf8.encode(payload));

    expect(seenHost, '192.168.1.200');
    expect(seenPort, 18789);
  });

  test('BeaconListener discovers via senderHost when provided', () {
    final discovered = <String>[];

    final listener = BeaconListener(
      pairing: pairing,
      onMacDiscovered: (host, port) {
        discovered.add(host);
      },
    );

    final payload = jsonEncode({
      'proto': 'fuseitall-beacon-v1',
      'host': '192.168.1.200',
      'port': 18789,
      'fp': 'aa' * 32,
      'ts': 1700000000,
    });

    listener.handleDatagramForTesting(utf8.encode(payload), '192.168.1.250');

    expect(discovered, contains('192.168.1.250'));
    expect(discovered, contains('192.168.1.200'));
  });

  test('BeaconListener ignores beacons with mismatched fingerprint', () {
    var fired = false;

    final listener = BeaconListener(
      pairing: pairing,
      onMacDiscovered: (host, port) => fired = true,
    );

    final payload = jsonEncode({
      'proto': 'fuseitall-beacon-v1',
      'host': '192.168.1.200',
      'port': 18789,
      'fp': 'bb' * 32,
      'ts': 1700000000,
    });

    listener.handleDatagramForTesting(utf8.encode(payload));
    expect(fired, isFalse);
  });
}
