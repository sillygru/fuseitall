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
    this.allFilesAccessGranted = false,
    this.photosPermission = 'denied',
  });

  final bool listenerEnabled;
  final bool batteryUnrestricted;
  final bool allFilesAccessGranted;

  /// Photos permission: granted | limited | denied (API 34 SELECTED_PHOTOS = limited).
  final String photosPermission;

  bool get photosGranted => photosPermission == 'granted';
  bool get photosLimited => photosPermission == 'limited';
  bool get photosDenied => photosPermission == 'denied';
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
    var allFiles = false;
    var photosPerm = 'denied';
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
    try {
      allFiles =
          await _channel.invokeMethod<bool>('isAllFilesAccessGranted') ?? false;
    } catch (e) {
      debugPrint('all files status failed: $e');
    }
    try {
      final p = await _channel.invokeMethod<String>('getPhotosPermission');
      if (p != null && p.isNotEmpty) photosPerm = p;
    } catch (e) {
      debugPrint('photos permission failed: $e');
    }
    return PermissionStatus(
      listenerEnabled: listener,
      batteryUnrestricted: battery,
      allFilesAccessGranted: allFiles,
      photosPermission: photosPerm,
    );
  }

  Future<bool> isAllFilesAccessGranted() async {
    try {
      return await _channel.invokeMethod<bool>('isAllFilesAccessGranted') ??
          false;
    } catch (e) {
      debugPrint('check all files failed: $e');
      return false;
    }
  }

  Future<String?> getExternalRoot() async {
    try {
      final r = await _channel.invokeMethod<String>('getExternalRoot');
      if (r != null && r.isNotEmpty) return r;
    } catch (e) {
      debugPrint('get external root failed: $e');
    }
    return null;
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

  Future<void> openAllFilesAccessSettings() async {
    try {
      await _channel.invokeMethod<void>('openAllFilesAccessSettings');
    } catch (e) {
      debugPrint('open all files settings failed: $e');
    }
  }

  Future<String> getPhotosPermission() async {
    try {
      final r = await _channel.invokeMethod<String>('getPhotosPermission');
      if (r != null && r.isNotEmpty) return r;
    } catch (e) {
      debugPrint('get photos perm failed: $e');
    }
    return 'denied';
  }

  Future<bool> isPhotosGranted() async {
    final p = await getPhotosPermission();
    return p == 'granted' || p == 'limited';
  }

  Future<void> openPhotosSettings() async {
    try {
      await _channel.invokeMethod<void>('openPhotosSettings');
    } catch (e) {
      debugPrint('open photos settings failed: $e');
    }
  }

  Future<void> requestPhotosPermission() async {
    try {
      await _channel.invokeMethod<void>('requestPhotosPermission');
    } catch (e) {
      debugPrint('request photos failed: $e');
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

  /// Stage-2 clipboard auto trigger status. Never throws: platform errors
  /// degrade to false so the UI shows setup steps.
  Future<bool> isReadLogsGranted() async {
    try {
      return await _channel.invokeMethod<bool>('isReadLogsGranted') ?? false;
    } catch (e) {
      debugPrint('read logs status failed: $e');
      return false;
    }
  }

  Future<bool> isOverlayAllowed() async {
    try {
      return await _channel.invokeMethod<bool>('isOverlayAllowed') ?? false;
    } catch (e) {
      debugPrint('overlay status failed: $e');
      return false;
    }
  }

  Future<void> openOverlaySettings() async {
    try {
      await _channel.invokeMethod<void>('openOverlaySettings');
    } catch (e) {
      debugPrint('open overlay settings failed: $e');
    }
  }

  /// Pushes the opt-in auto toggle to the native watcher. Returns true when
  /// active, false when perms are missing (UI then shows setup steps).
  /// Never throws.
  Future<bool> updateClipAuto(bool enabled) async {
    try {
      final ok = await _channel.invokeMethod<bool>(
          'updateClipAuto', {'enabled': enabled});
      return ok ?? !enabled;
    } catch (e) {
      debugPrint('update clip auto failed: $e');
      return false;
    }
  }
}
