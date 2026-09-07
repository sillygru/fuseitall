// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/notifications/notif_listener.dart';
import 'package:fuseitall/features/notifications/notif_models.dart';

void main() {
  group('NotifItem', () {
    test('cleanId accepts up to 256 runes and rejects over 256', () {
      expect(NotifItem.cleanId(''), isNull);
      expect(NotifItem.cleanId('   '), isNull);
      expect(NotifItem.cleanId('abc'), 'abc');

      final id256 = 'x' * 256;
      expect(NotifItem.cleanId(id256), id256);

      final id257 = 'x' * 257;
      expect(NotifItem.cleanId(id257), isNull);
    });

    test('fromPosted builds valid NotifItem and truncates long fields fail-soft', () {
      final item = NotifItem.fromPosted(
        id: 'pkg|123|tag',
        app: '  WhatsApp  ',
        packageName: 'com.whatsapp',
        iconB64: 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAAAAAA6fptVAAAACklEQVR4nGP6zwAA/8cB+fO4hX4AAAAASUVORK5CYII=',
        groupKey: 'group1',
        title: 'John Doe',
        text: 'Hello there!',
        postedAt: 1234567890,
      );
      expect(item, isNotNull);
      expect(item!.id, 'pkg|123|tag');
      expect(item.app, 'WhatsApp');
      expect(item.packageName, 'com.whatsapp');
      expect(item.title, 'John Doe');
      expect(item.text, 'Hello there!');
      expect(item.postedAt, 1234567890);
    });
  });

  group('NotifListener.parseEvent', () {
    test('parses post event correctly', () {
      final map = <String, dynamic>{
        'event': 'post',
        'id': 'notif_1',
        'app': 'Messages',
        'package_name': 'com.google.android.apps.messaging',
        'title': 'Alice',
        'text': 'See you at 5',
        'posted_at': 1700000000,
      };
      final (:post, :removal) = NotifListener.parseEvent(map);
      expect(removal, isNull);
      expect(post, isNotNull);
      expect(post!.id, 'notif_1');
      expect(post.app, 'Messages');
      expect(post.title, 'Alice');
      expect(post.text, 'See you at 5');
    });

    test('parses remove event correctly', () {
      final map = <String, dynamic>{
        'event': 'remove',
        'id': 'notif_1',
      };
      final (:post, :removal) = NotifListener.parseEvent(map);
      expect(post, isNull);
      expect(removal, 'notif_1');
    });
  });

  group('NotifOutbox', () {
    test('queues and takes posts FIFO with deduplication', () {
      final outbox = NotifOutbox(maxQueued: 5);
      final item1 = NotifItem(id: '1', title: 'First', text: 'v1');
      final item2 = NotifItem(id: '2', title: 'Second', text: 'v1');
      final item1Updated = NotifItem(id: '1', title: 'First', text: 'v2');

      outbox.queuePost(item1);
      outbox.queuePost(item2);
      expect(outbox.posts.length, 2);
      expect(outbox.posts.first.text, 'v1');

      // Repost of id: '1' moves to end with updated content
      outbox.queuePost(item1Updated);
      expect(outbox.posts.length, 2);
      expect(outbox.posts.first.id, '2');
      expect(outbox.posts.last.id, '1');
      expect(outbox.posts.last.text, 'v2');

      final taken = outbox.takePosts(1);
      expect(taken.length, 1);
      expect(taken.first.id, '2');
      expect(outbox.posts.length, 1);
    });

    test('queues and takes dismissals', () {
      final outbox = NotifOutbox();
      outbox.queueDismiss('id_1');
      outbox.queueDismiss('id_2');
      expect(outbox.dismissals, ['id_1', 'id_2']);

      final taken = outbox.takeDismissals();
      expect(taken, ['id_1', 'id_2']);
      expect(outbox.dismissals, isEmpty);
    });
  });
}
