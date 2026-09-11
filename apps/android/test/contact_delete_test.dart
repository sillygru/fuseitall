// SPDX-License-Identifier: AGPL-3.0-only

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/contacts/contact_store.dart';
import 'package:fuseitall/features/contacts/contact_sync.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('ContactSync handles contact-delete-req and sends response', () async {
    const channel = MethodChannel('fuseitall/contacts');
    final log = <MethodCall>[];
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
      log.add(call);
      if (call.method == 'deleteContact') {
        return {'ok': true, 'contact_id': call.arguments['contact_id']};
      }
      return null;
    });

    final store = ContactStore(channel: channel);
    final sent = <Map<String, dynamic>>[];
    final sync = ContactSync(
      store: store,
      sendFeature: (type, payload) async {
        sent.add({'type': type, 'payload': payload});
      },
    );

    final handled = await sync.handleEvent({
      'type': 'contact-delete-req',
      'payload': {
        'req_id': 'req-del-1',
        'nonce': 'nonce-del-1',
        'contact_id': '101',
        'lookup_key': 'key-101',
      },
    });

    expect(handled, isTrue);
    expect(log.length, 1);
    expect(log.first.method, 'deleteContact');
    expect(log.first.arguments['contact_id'], '101');
    expect(log.first.arguments['lookup_key'], 'key-101');

    expect(sent.length, 1);
    expect(sent.first['type'], 'contact-delete-resp');
    final respPayload = sent.first['payload'] as Map<String, dynamic>;
    expect(respPayload['req_id'], 'req-del-1');
    expect(respPayload['contact_id'], '101');
    expect(respPayload['ok'], isTrue);
  });

  test('ContactSync handles permission denied on deleteContact', () async {
    const channel = MethodChannel('fuseitall/contacts');
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
      if (call.method == 'deleteContact') {
        return {
          'ok': false,
          'contact_id': call.arguments['contact_id'],
          'error': 'Contacts write permission needed on phone.',
          'error_code': 'permission_denied',
        };
      }
      return null;
    });

    final store = ContactStore(channel: channel);
    final sent = <Map<String, dynamic>>[];
    final sync = ContactSync(
      store: store,
      sendFeature: (type, payload) async {
        sent.add({'type': type, 'payload': payload});
      },
    );

    final handled = await sync.handleEvent({
      'type': 'contact-delete-req',
      'payload': {
        'req_id': 'req-del-2',
        'nonce': 'nonce-del-2',
        'contact_id': '102',
      },
    });

    expect(handled, isTrue);
    expect(sent.length, 1);
    expect(sent.first['type'], 'contact-delete-resp');
    final respPayload = sent.first['payload'] as Map<String, dynamic>;
    expect(respPayload['req_id'], 'req-del-2');
    expect(respPayload['contact_id'], '102');
    expect(respPayload['ok'], isFalse);
    expect(respPayload['error_code'], 'permission_denied');
    expect(respPayload['permission'], 'contacts');
    expect(respPayload['error'], contains('permission needed'));
  });
}
