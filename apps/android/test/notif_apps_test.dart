// SPDX-License-Identifier: AGPL-3.0-only

import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/notifications/notif_apps_sync.dart';
import 'package:fuseitall/features/ping/proto_client.dart';

void main() {
  group('notif-apps wire', () {
    test('featurePath rides the /notif lane', () {
      expect(featurePath('notif-apps-req'), kNotifPath);
      expect(featurePath('notif-apps-resp'), kNotifPath);
    });

    test('ignores non-inventory types', () async {
      final sent = <Map<String, Object?>>[];
      final sync = NotifAppsSync(
        sendFeature: (type, payload) async {
          sent.add({'type': type, ...payload});
        },
      );
      expect(
        await sync.handleEvent({'type': 'notif-post', 'payload': {}}),
        isFalse,
      );
      expect(sent, isEmpty);
    });

    test('answers one page with req_id correlation', () async {
      final sent = <Map<String, Object?>>[];
      final sync = NotifAppsSync(
        sendFeature: (type, payload) async {
          sent.add({'type': type, ...payload});
        },
        listPage: ({required cursor, required limit, required withIcons}) async {
          expect(cursor, '');
          expect(limit, 50);
          expect(withIcons, isTrue);
          return (
            entries: <Map<String, String>>[
              {'package_name': 'com.a', 'app': 'A'},
              {'package_name': 'com.b', 'app': 'B', 'app_icon_b64': 'aGk='},
            ],
            nextCursor: 'com.b',
          );
        },
      );
      expect(
        await sync.handleEvent({
          'type': 'notif-apps-req',
          'payload': {'req_id': 'r1'},
        }),
        isTrue,
      );
      expect(sent.length, 1);
      expect(sent.single['type'], 'notif-apps-resp');
      expect(sent.single['req_id'], 'r1');
      expect((sent.single['entries'] as List).length, 2);
      expect(sent.single['next_cursor'], 'com.b');
    });

    test('rejects bad limit without calling inventory', () async {
      var listed = false;
      final sent = <Map<String, Object?>>[];
      final sync = NotifAppsSync(
        sendFeature: (type, payload) async {
          sent.add({'type': type, ...payload});
        },
        listPage: ({required cursor, required limit, required withIcons}) async {
          listed = true;
          return (entries: const <Map<String, String>>[], nextCursor: '');
        },
      );
      expect(
        await sync.handleEvent({
          'type': 'notif-apps-req',
          'payload': {'req_id': 'r1', 'limit': 99},
        }),
        isTrue,
      );
      expect(listed, isFalse);
      expect(sent.single['error_code'], 'invalid_arg');
    });

    test('drops missing req_id silently', () async {
      final sent = <Map<String, Object?>>[];
      final sync = NotifAppsSync(
        sendFeature: (type, payload) async {
          sent.add({'type': type, ...payload});
        },
      );
      expect(
        await sync.handleEvent({
          'type': 'notif-apps-req',
          'payload': <String, Object?>{},
        }),
        isTrue,
      );
      expect(sent, isEmpty);
    });
  });
}
