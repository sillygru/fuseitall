// SPDX-License-Identifier: AGPL-3.0-only

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/clipboard/clip_send.dart';
import 'package:fuseitall/features/permissions/permissions.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('ClipSendChannel', () {
    test('focus handler runs on native onClipFocus then ack closes it',
        () async {
      var focused = 0;
      String? acked;
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(
        const MethodChannel('fuseitall/clipSend'),
        (call) async {
          if (call.method == 'clipSendDone') {
            acked = call.method;
            return null;
          }
          if (call.method == 'popRequested') return false;
          return null;
        },
      );
      addTearDown(() {
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
            .setMockMethodCallHandler(
                const MethodChannel('fuseitall/clipSend'), null);
      });
      final ch = ClipSendChannel();
      ch.setFocusHandler(() async {
        focused++;
        await ch.ackDone();
      });
      await TestDefaultBinaryMessengerBinding
          .instance.defaultBinaryMessenger
          .handlePlatformMessage(
        'fuseitall/clipSend',
        const StandardMethodCodec()
            .encodeMethodCall(const MethodCall('onClipFocus')),
        (_) {},
      );
      expect(focused, 1);
      expect(acked, 'clipSendDone');
      ch.clearHandler();
    });

    test('popRequested drains cold-path latch once', () async {
      var pops = 0;
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(
        const MethodChannel('fuseitall/clipSend'),
        (call) async {
          if (call.method == 'popRequested') {
            pops++;
            return pops == 1;
          }
          return null;
        },
      );
      addTearDown(() {
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
            .setMockMethodCallHandler(
                const MethodChannel('fuseitall/clipSend'), null);
      });
      final ch = ClipSendChannel();
      expect(await ch.popRequested(), isTrue);
      expect(await ch.popRequested(), isFalse);
    });

    test('channel errors degrade to no-op, never throw', () async {
      final ch = ClipSendChannel(
          channel: const MethodChannel('fuseitall/clipSend-missing'));
      expect(await ch.popRequested(), isFalse);
      await ch.ackDone();
      ch.setFocusHandler(() async {});
      ch.clearHandler();
    });
  });

  group('clip auto permissions', () {
    test('missing perms degrade to false, update fails closed', () async {
      final p = Permissions(
          channel: const MethodChannel('fuseitall/permissions-missing'));
      expect(await p.isReadLogsGranted(), isFalse);
      expect(await p.isOverlayAllowed(), isFalse);
      expect(await p.updateClipAuto(true), isFalse);
      await p.openOverlaySettings();
    });

    test('updateClipAuto true only on native ack', () async {
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(
        const MethodChannel('fuseitall/permissions'),
        (call) async {
          if (call.method == 'updateClipAuto') {
            return (call.arguments as Map)['enabled'] == true;
          }
          if (call.method == 'isReadLogsGranted') return true;
          if (call.method == 'isOverlayAllowed') return false;
          return null;
        },
      );
      addTearDown(() {
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
            .setMockMethodCallHandler(
                const MethodChannel('fuseitall/permissions'), null);
      });
      final p = Permissions();
      expect(await p.updateClipAuto(true), isTrue);
      expect(await p.updateClipAuto(false), isFalse);
      expect(await p.isReadLogsGranted(), isTrue);
      expect(await p.isOverlayAllowed(), isFalse);
    });
  });
}
