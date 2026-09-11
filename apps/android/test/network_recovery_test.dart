// SPDX-License-Identifier: AGPL-3.0-only

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
