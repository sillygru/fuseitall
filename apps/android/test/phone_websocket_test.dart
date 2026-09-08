// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/pairing/pair_qr.dart';
import 'package:fuseitall/net/phone_websocket.dart';

void main() {
  test('PhoneWebSocket lifecycle and envelope streaming', () async {
    // Spin up an insecure local HTTP server for loopback testing
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final port = server.port;

    final serverEnvelopes = <Map<String, dynamic>>[];
    final serverDone = Completer<void>();

    server.listen((req) async {
      if (req.uri.path == '/ws') {
        final ws = await WebSocketTransformer.upgrade(req);
        ws.listen((data) {
          if (data is String) {
            final dec = jsonDecode(data) as Map<String, dynamic>;
            serverEnvelopes.add(dec);
            // Echo an envelope back
            ws.add(jsonEncode({'type': 'pong', 'payload': {'nonce': 'echo-123'}}));
          }
        }, onDone: () {
          if (!serverDone.isCompleted) serverDone.complete();
        });
      }
    });

    final pairing = PairQR(
      v: 1,
      deviceName: 'Test Host',
      platform: 'mac',
      host: '127.0.0.1',
      port: port,
      fingerprint: 'dummy',
      pubkey: 'dummy',
      token: 'secret-token',
      code: '123456',
    );

    final states = <WsConnectionState>[];
    final client = PhoneWebSocket(
      pairing: pairing,
      onStateChanged: (st) => states.add(st),
    );

    // Insecure test hook or standard ws
    final incomingCompleter = Completer<Map<String, dynamic>>();
    client.onEnvelope.listen((env) {
      if (!incomingCompleter.isCompleted) incomingCompleter.complete(env);
    });

    // Test connection with bypassTofu for localhost unit test
    final connected = await client.connectWithClient(
      '127.0.0.1',
      port,
      customClient: HttpClient(),
      useTls: false,
    );
    expect(connected, isTrue);
    expect(client.isConnected, isTrue);

    // Send envelope
    final sent = await client.sendEnvelope({
      'protocol_v': 1,
      'type': 'ping',
      'payload': {'nonce': 'test-nonce'},
    });
    expect(sent, isTrue);

    final received = await incomingCompleter.future.timeout(const Duration(seconds: 3));
    expect(received['type'], 'pong');
    expect(received['payload']['nonce'], 'echo-123');
    expect(serverEnvelopes.length, 1);
    expect(serverEnvelopes.first['payload']['nonce'], 'test-nonce');

    await client.dispose();
    await server.close(force: true);
  });

  test('PhoneWebSocket fastConnect isolates failures and keeps winning connection', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    server.listen((req) async {
      if (req.uri.path == '/ws') {
        await WebSocketTransformer.upgrade(req);
      }
    });

    final pairing = PairQR(
      v: 1,
      deviceName: 'Test Host',
      platform: 'mac',
      host: '127.0.0.1',
      port: server.port,
      fingerprint: 'dummy',
      pubkey: 'dummy',
      token: 'secret-token',
      code: '123456',
    );

    final client = PhoneWebSocket(
      pairing: pairing,
      customClient: HttpClient(),
      useTls: false,
    );

    // Provide a dead candidate and the live candidate
    final winner = await client.fastConnect(['127.0.0.2', '127.0.0.1'], server.port);
    expect(winner, '127.0.0.1');
    expect(client.isConnected, isTrue);

    // Wait to ensure dead candidate does not disconnect client
    await Future<void>.delayed(const Duration(milliseconds: 300));
    expect(client.isConnected, isTrue);

    await client.dispose();
    await server.close(force: true);
  });
}
