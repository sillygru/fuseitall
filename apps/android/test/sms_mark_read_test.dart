// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/messages/sms_store.dart';
import 'package:fuseitall/features/messages/sms_sync.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('SmsSync handles sms-mark-read-req and sends response', () async {
    const channel = MethodChannel('fuseitall/sms');
    final log = <MethodCall>[];
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
      log.add(call);
      if (call.method == 'markRead') {
        return {'ok': true, 'thread_id': call.arguments['thread_id']};
      }
      return null;
    });

    final store = SmsStore(channel: channel);
    final sent = <Map<String, dynamic>>[];
    final sync = SmsSync(
      store: store,
      sendFeature: (type, payload) async {
        sent.add({'type': type, 'payload': payload});
      },
    );

    final handled = await sync.handleEvent({
      'type': 'sms-mark-read-req',
      'payload': {
        'req_id': 'req-read-1',
        'nonce': 'nonce-read-1',
        'thread_id': 42,
        'message_id': 0,
        'address': '+15551234567',
      },
    });

    expect(handled, isTrue);
    expect(log.length, 1);
    expect(log.first.method, 'markRead');
    expect(log.first.arguments['thread_id'], 42);
    expect(log.first.arguments['address'], '+15551234567');

    expect(sent.length, 1);
    expect(sent.first['type'], 'sms-mark-read-resp');
    final respPayload = sent.first['payload'] as Map<String, dynamic>;
    expect(respPayload['req_id'], 'req-read-1');
    expect(respPayload['thread_id'], 42);
    expect(respPayload['ok'], isTrue);
  });
}
