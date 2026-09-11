// SPDX-License-Identifier: AGPL-3.0-only

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/net/wifi_bind.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  const channel = MethodChannel('fuseitall/wifibind');

  tearDown(() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null);
  });

  group('WifiBind', () {
    test('bindWifi returns true when native binds', () async {
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (call) async {
        expect(call.method, 'bindWifi');
        expect(call.arguments['timeoutMs'], 8000);
        return true;
      });
      expect(await WifiBind().bindWifi(), isTrue);
    });

    test('bindWifi returns false when native declines', () async {
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (call) async => false);
      expect(await WifiBind().bindWifi(), isFalse);
    });

    test('bindWifi returns false when the channel is missing', () async {
      // No mock handler: MissingPluginException must not escape.
      expect(await WifiBind().bindWifi(), isFalse);
    });

    test('unbindWifi never throws, even without a host', () async {
      await WifiBind().unbindWifi();
    });
  });
}
