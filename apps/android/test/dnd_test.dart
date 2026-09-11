// SPDX-License-Identifier: AGPL-3.0-only

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/dnd/dnd_models.dart';
import 'package:fuseitall/features/dnd/dnd_sync.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('DndState', () {
    test('serialization round-trip', () {
      const state = DndState(
        enabled: true,
        hasPermission: true,
        updatedMs: 1234567,
      );

      final json = state.toJson(origin: 'android');
      expect(json['enabled'], isTrue);
      expect(json['has_permission'], isTrue);
      expect(json['updated_ms'], 1234567);
      expect(json['origin'], 'android');

      final restored = DndState.fromJson(json);
      expect(restored.enabled, isTrue);
      expect(restored.hasPermission, isTrue);
      expect(restored.updatedMs, 1234567);
    });

    test('fromJson handles absent/empty values fail-soft', () {
      final state = DndState.fromJson(const {});
      expect(state.enabled, isFalse);
      expect(state.hasPermission, isFalse);
      expect(state.updatedMs, 0);
    });

    test('equality and copyWith', () {
      const a = DndState(enabled: false, hasPermission: false, updatedMs: 100);
      final b = a.copyWith(enabled: true);
      expect(b.enabled, isTrue);
      expect(b.hasPermission, isFalse);
      expect(b.updatedMs, 100);
      expect(a == b, isFalse);
      expect(b == const DndState(enabled: true, hasPermission: false, updatedMs: 100), isTrue);
    });
  });

  group('DndSync', () {
    const channel = MethodChannel('fuseitall/dnd');

    tearDown(() {
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, null);
    });

    test('getState queries method channel and returns state', () async {
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (MethodCall call) async {
        if (call.method == 'getDndState') {
          return {'enabled': true, 'has_permission': true};
        }
        return null;
      });

      final sync = DndSync(methodChannel: channel);
      final state = await sync.getState();
      expect(state.enabled, isTrue);
      expect(state.hasPermission, isTrue);
    });

    test('setDnd invokes method channel with enabled flag', () async {
      bool? passedEnabled;
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (MethodCall call) async {
        if (call.method == 'setDnd') {
          passedEnabled = call.arguments['enabled'] as bool?;
          return true;
        }
        return null;
      });

      final sync = DndSync(methodChannel: channel);
      final ok = await sync.setDnd(true);
      expect(ok, isTrue);
      expect(passedEnabled, isTrue);
    });

    test('openSettings invokes openDndSettings on method channel', () async {
      var called = false;
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (MethodCall call) async {
        if (call.method == 'openDndSettings') {
          called = true;
          return true;
        }
        return null;
      });

      final sync = DndSync(methodChannel: channel);
      await sync.openSettings();
      expect(called, isTrue);
    });
  });
}
