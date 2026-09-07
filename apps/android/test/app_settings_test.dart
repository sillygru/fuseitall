// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/clipboard/clipboard_sync.dart';
import 'package:fuseitall/features/notifications/notif_models.dart';
import 'package:fuseitall/features/ping/proto_client.dart';
import 'package:fuseitall/features/settings/app_settings.dart';
import 'package:fuseitall/result.dart';

void main() {
  group('AppSettings', () {
    test('unknown mode falls back, absent toggle means true', () {
      final st = AppSettings.fromJson({
        'clipboard_mode': 'both',
        'updated_unix': 7,
      });
      expect(st.clipboardMode, AppSettings.twoWay);
      expect(st.notificationsEnabled, isTrue);
    });

    test('newer wins, ties go to mac', () {
      const local = AppSettings(
        clipboardMode: AppSettings.off,
        notificationsEnabled: true,
        updatedUnix: 10,
        updatedBy: 'android',
      );
      const newer = AppSettings(
        clipboardMode: AppSettings.twoWay,
        notificationsEnabled: true,
        updatedUnix: 11,
        updatedBy: 'android',
      );
      expect(remoteSettingsWins(local, newer), isTrue);
      expect(remoteSettingsWins(newer, local), isFalse);
      const tieMac = AppSettings(
        clipboardMode: AppSettings.off,
        notificationsEnabled: true,
        updatedUnix: 10,
        updatedBy: 'mac',
      );
      expect(remoteSettingsWins(local, tieMac), isTrue);
      expect(remoteSettingsWins(tieMac, local), isFalse);
    });

    test('direction gate', () {
      expect(
          clipDirectionAllows(
              AppSettings.twoWay, AppSettings.phoneToMac),
          isTrue);
      expect(clipDirectionAllows(AppSettings.off, AppSettings.phoneToMac),
          isFalse);
      expect(
          clipDirectionAllows(
              AppSettings.macToPhone, AppSettings.phoneToMac),
          isFalse);
    });

    test('labels carry arrows', () {
      expect(clipboardModeArrow(AppSettings.twoWay), '⇄');
      expect(clipboardModeArrow(AppSettings.macToPhone), '→');
      expect(clipboardModeArrow(AppSettings.phoneToMac), '←');
      expect(clipboardModeArrow(AppSettings.off), '∅');
    });
  });

  group('ClipState', () {
    test('newer adopts and disarms, ties keep local', () {
      var clip = const ClipState().setLocal('hi', 10)!;
      expect(clip.pending, isTrue);
      expect(clip.applyRemote(next: 'old', nextChangedAt: 5, nextOrigin: 'mac'),
          isNull);
      final next = clip.applyRemote(
          next: 'new', nextChangedAt: 20, nextOrigin: 'mac');
      expect(next, isNotNull);
      expect(next!.text, 'new');
      expect(next.pending, isFalse);
      expect(next.takePending(), isNull);
    });

    test('own echo and oversize drop', () {
      const clip = ClipState(
          text: 'a', changedAt: 10, origin: 'android', hasText: true);
      expect(
          clip.applyRemote(
              next: 'b', nextChangedAt: 20, nextOrigin: 'android'),
          isNull);
      expect(const ClipState().setLocal('x' * (ClipState.maxLen + 1), 1),
          isNull);
    });
  });

  group('NotifModels', () {
    test('bad id drops, fields truncate', () {
      expect(NotifItem.fromPosted(id: '  '), isNull);
      final item = NotifItem.fromPosted(
          id: 'n1', title: 't' * 200, text: 'b' * 600)!;
      expect(item.title.length, lessThanOrEqualTo(NotifItem.maxTitleLen));
      expect(item.text.length, lessThanOrEqualTo(NotifItem.maxTextLen));
    });

    test('outbox caps and requeues', () {
      final box = NotifOutbox(maxQueued: 3);
      for (var i = 0; i < 5; i++) {
        box.queuePost(NotifItem.fromPosted(id: 'n$i')!);
      }
      expect(box.posts.length, 3);
      final batch = box.takePosts(2);
      expect(batch.length, 2);
      box.requeuePosts(batch);
      expect(box.posts.length, 3);
    });
  });

  group('feature wire', () {
    test('envelope stamps nonce and caps', () {
      final env = buildFeatureEnvelope('notif-post', 'n', {'id': 'x'});
      expect(env['type'], 'notif-post');
      expect((env['payload'] as Map)['nonce'], 'n');
      expect((env['capabilities'] as List), contains('notifications'));
      expect(featurePath('clip-push'), '/clip');
      expect(featurePath('settings-sync'), '/settings');
      expect(featurePath('notif-dismiss'), '/notif');
    });

    test('ack echo strict, 426 maps to update', () {
      const nonce = 'abc';
      final ok = parseFeatureAck(
        type: 'clip-push',
        statusCode: 200,
        body: '{"protocol_v":1,"type":"pong","sender":{},'
            '"capabilities":[],"payload":{"nonce":"$nonce"}}',
        expectedNonce: nonce,
      );
      expect(ok, isA<Ok<String>>());
      final mismatch = parseFeatureAck(
        type: 'clip-push',
        statusCode: 200,
        body: '{"protocol_v":1,"type":"pong","sender":{},'
            '"capabilities":[],"payload":{"nonce":"other"}}',
        expectedNonce: nonce,
      );
      expect((mismatch as Err).failure, isA<NonceMismatch>());
      final upd = parseFeatureAck(
        type: 'clip-push',
        statusCode: 426,
        body: '{"protocol_v":1,"type":"error","sender":{},'
            '"capabilities":[],"payload":{"code":"UPDATE_REQUIRED",'
            '"message":"Update me"}}',
        expectedNonce: nonce,
      );
      expect((upd as Err).failure, isA<UpdateRequired>());
    });
  });
}
