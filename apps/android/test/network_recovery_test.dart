// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version. See LICENSE for details.

import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/pairing/pair_qr.dart';
import 'package:fuseitall/net/phone_websocket.dart';

void main() {
  test('disconnect invalidates the socket and emits offline state', () async {
    final states = <WsConnectionState>[];
    final client = PhoneWebSocket(
      pairing: const PairQR(
        v: 1,
        deviceName: 'Mac',
        platform: 'macos',
        host: '127.0.0.1',
        port: 18789,
        fingerprint: 'unused',
        pubkey: 'unused',
        token: 'token',
        code: 'code',
      ),
      onStateChanged: states.add,
    );

    client.disconnect();

    expect(client.state, WsConnectionState.disconnected);
    expect(client.isConnected, isFalse);
    expect(states, isEmpty);
    await client.dispose();
  });
}
