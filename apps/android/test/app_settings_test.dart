// SPDX-License-Identifier: AGPL-3.0-only

import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/clipboard/clipboard_sync.dart';
import 'package:fuseitall/features/notifications/notif_models.dart';
import 'package:fuseitall/features/ping/proto_client.dart';
import 'package:fuseitall/features/settings/app_settings.dart';
import 'package:fuseitall/result.dart';

void main() {
  group('AppSettings', () {
    test('absent toggle means true', () {
      final st = AppSettings.fromJson({
        'updated_unix': 7,
      });
      expect(st.notificationsEnabled, isTrue);
    });

    test('background auto toggle is local-only, off by default', () {
      final st = AppSettings.fromJson({'updated_unix': 7});
      expect(st.clipboardAutoBackground, isFalse);
      expect(st.toJson().containsKey('clipboard_auto_background'), isFalse);

      final on = st.withClipboardAutoBackground(true, nowUnix: 8);
      expect(on.clipboardAutoBackground, isTrue);
      expect(on.toJson()['clipboard_auto_background'], isTrue);
      // Wire payload strips it: packages/proto stays the only contract.
      expect(on.toSyncJson().containsKey('clipboard_auto_background'), isFalse);

      final rt = AppSettings.fromJson(on.toJson());
      expect(rt.clipboardAutoBackground, isTrue);
    });

    test('remote adopt preserves local-only background toggle', () {
      final local = AppSettings.defaults(nowUnix: 10)
          .withClipboardAutoBackground(true, nowUnix: 11);
      final remote = AppSettings.fromJson({
        'updated_unix': 12,
        'updated_by': 'mac',
        'clipboard_mode': 'both',
      });
      expect(remoteSettingsWins(local, remote), isTrue);
      final merged = remote.withLocalFlagsFrom(local);
      expect(merged.clipboardAutoBackground, isTrue);
      expect(merged.updatedUnix, 12);
      expect(merged.updatedBy, 'mac');
    });

    test('newer wins, ties go to mac', () {
      const local = AppSettings(
        notificationsEnabled: true,
        clipboardMode: 'both',
        notifMode: AppSettings.notifAllExceptMuted,
        mutedPackages: {},
        allowedPackages: {},
        playbackMode: AppSettings.playbackDefault,
        playbackOutput: AppSettings.playbackOutputInApp,
        updatedUnix: 10,
        updatedBy: 'android',
      );
      const newer = AppSettings(
        notificationsEnabled: true,
        clipboardMode: 'both',
        notifMode: AppSettings.notifAllExceptMuted,
        mutedPackages: {},
        allowedPackages: {},
        playbackMode: AppSettings.playbackDefault,
        playbackOutput: AppSettings.playbackOutputInApp,
        updatedUnix: 11,
        updatedBy: 'android',
      );
      expect(remoteSettingsWins(local, newer), isTrue);
      expect(remoteSettingsWins(newer, local), isFalse);
      const tieMac = AppSettings(
        notificationsEnabled: true,
        clipboardMode: 'both',
        notifMode: AppSettings.notifAllExceptMuted,
        mutedPackages: {},
        allowedPackages: {},
        playbackMode: AppSettings.playbackDefault,
        playbackOutput: AppSettings.playbackOutputInApp,
        updatedUnix: 10,
        updatedBy: 'mac',
      );
      expect(remoteSettingsWins(local, tieMac), isTrue);
      expect(remoteSettingsWins(tieMac, local), isFalse);
    });

    test('absent filter means allow-all; round-trips lists', () {
      final st = AppSettings.fromJson({'updated_unix': 7});
      expect(st.notifMode, AppSettings.notifAllExceptMuted);
      expect(st.mutedPackages, isEmpty);
      expect(st.allowedPackages, isEmpty);
      expect(st.shouldMirrorNotif('com.whatsapp'), isTrue);

      final muted = st.withMutedToggled('com.muted', nowUnix: 8);
      expect(muted.shouldMirrorNotif('com.muted'), isFalse);
      expect(muted.shouldMirrorNotif('com.other'), isTrue);
      final rt = AppSettings.fromJson(muted.toJson());
      expect(rt.mutedPackages, contains('com.muted'));

      final allowed = AppSettings.fromJson({
        'updated_unix': 9,
        'notif_mode': 'only_allowed',
        'allowed_packages': ['com.keep'],
      });
      expect(allowed.shouldMirrorNotif('com.keep'), isTrue);
      expect(allowed.shouldMirrorNotif('com.other'), isFalse);
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
