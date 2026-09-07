// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

/// Status snapshot for the Essential Services card. Never throws: platform
/// errors degrade to false so the UI shows Disabled with a fix CTA.
class PermissionStatus {
  const PermissionStatus({
    required this.listenerEnabled,
    required this.batteryUnrestricted,
  });

  final bool listenerEnabled;
  final bool batteryUnrestricted;
}

/// Bridge to MainActivity's fuseitall/permissions channel. Injectable
/// channel for widget tests (no platform in tests).
class Permissions {
  Permissions({MethodChannel? channel})
    : _channel = channel ?? const MethodChannel('fuseitall/permissions');

  final MethodChannel _channel;

  Future<PermissionStatus> status() async {
    var listener = false;
    var battery = false;
    try {
      listener =
          await _channel.invokeMethod<bool>('isNotificationListenerEnabled') ??
          false;
    } catch (e) {
      debugPrint('listener status failed: $e');
    }
    try {
      battery =
          await _channel.invokeMethod<bool>('isBatteryUnrestricted') ?? false;
    } catch (e) {
      debugPrint('battery status failed: $e');
    }
    return PermissionStatus(
      listenerEnabled: listener,
      batteryUnrestricted: battery,
    );
  }

  Future<void> openListenerSettings() async {
    try {
      await _channel.invokeMethod<void>('openNotificationListenerSettings');
    } catch (e) {
      debugPrint('open listener settings failed: $e');
    }
  }

  Future<void> openBatterySettings() async {
    try {
      await _channel.invokeMethod<void>('openBatterySettings');
    } catch (e) {
      debugPrint('open battery settings failed: $e');
    }
  }

  Future<void> startLinkService() async {
    try {
      await _channel.invokeMethod<void>('startLinkService');
    } catch (e) {
      debugPrint('start link service failed: $e');
    }
  }

  Future<void> stopLinkService() async {
    try {
      await _channel.invokeMethod<void>('stopLinkService');
    } catch (e) {
      debugPrint('stop link service failed: $e');
    }
  }
}
