// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import 'dnd_models.dart';

/// Event-driven Do Not Disturb synchronization service.
/// Consumes push events from native [EventChannel] (ACTION_INTERRUPTION_FILTER_CHANGED),
/// eliminating any need for polling.
class DndSync {
  DndSync({
    MethodChannel? methodChannel,
    EventChannel? eventChannel,
  })  : _methodChannel = methodChannel ?? const MethodChannel('fuseitall/dnd'),
        _eventChannel = eventChannel ?? const EventChannel('fuseitall/dndEvents');

  final MethodChannel _methodChannel;
  final EventChannel _eventChannel;

  /// Stream of real-time DND state changes pushed directly by the OS.
  Stream<DndState> get states {
    return _eventChannel
        .receiveBroadcastStream()
        .map((event) {
          if (event is Map) {
            final enabled = event['enabled'] == true;
            final hasPerm = event['has_permission'] == true;
            final nowMs = DateTime.now().toUtc().millisecondsSinceEpoch;
            return DndState(
              enabled: enabled,
              hasPermission: hasPerm,
              updatedMs: nowMs,
            );
          }
          return null;
        })
        .where((state) => state != null)
        .cast<DndState>();
  }

  /// One-shot fetch of the current DND state (connect/resume only).
  Future<DndState> getState() async {
    try {
      final res = await _methodChannel.invokeMapMethod<String, dynamic>('getDndState');
      if (res != null) {
        final nowMs = DateTime.now().toUtc().millisecondsSinceEpoch;
        return DndState(
          enabled: res['enabled'] == true,
          hasPermission: res['has_permission'] == true,
          updatedMs: nowMs,
        );
      }
    } catch (e) {
      debugPrint('getDndState error: $e');
    }
    return const DndState(enabled: false, hasPermission: false);
  }

  /// Sets the system Do Not Disturb filter (true = Priority, false = All).
  Future<bool> setDnd(bool enabled) async {
    try {
      final ok = await _methodChannel.invokeMethod<bool>('setDnd', {'enabled': enabled});
      return ok == true;
    } catch (e) {
      debugPrint('setDnd error: $e');
      return false;
    }
  }

  /// Opens the system Do Not Disturb policy access settings.
  Future<void> openSettings() async {
    try {
      await _methodChannel.invokeMethod<void>('openDndSettings');
    } catch (e) {
      debugPrint('openDndSettings error: $e');
    }
  }
}
