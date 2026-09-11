// SPDX-License-Identifier: AGPL-3.0-only

import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

// Regression guard for go_bridge/bridge.go: the phone-side server must bind
// 0.0.0.0 (all interfaces) so the LAN peer can connect. 127.0.0.1 would
// loopback-isolate it (connection refused from the Mac). Mirrors the Go
// constant phoneBindAddr covered by go_bridge/bridge_test.go.
void main() {
  test('bridge binds all interfaces for LAN reachability', () {
    final src = File('go_bridge/bridge.go').readAsStringSync();
    expect(src, contains('phoneBindAddr = "0.0.0.0"'));
    expect(src, isNot(contains('JoinHostPort("127.0.0.1"')));
    expect(src, contains('JoinHostPort(phoneBindAddr'));
  });

  test('bridge source documents the LAN-exposure rationale', () {
    final src = File('go_bridge/bridge.go').readAsStringSync();
    expect(src.toLowerCase(), contains('lan'));
  });

  test('ephemeral port + reply_port advertisement are intact', () {
    final server =
        File('lib/net/go_server.dart').readAsStringSync();
    expect(server, contains('bridge.start(token, 0)'));
    final client = File('lib/features/ping/proto_client.dart')
        .readAsStringSync();
    expect(client, contains('reply_port'));
  });
}
