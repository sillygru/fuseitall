// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// App settings synced with the Mac (last-writer-wins). Mirrors
// packages/proto/settings.json and core SanitizeSettings/RemoteSettingsWins:
// unknown modes fail closed, greater updatedUnix wins, ties go to mac.
class AppSettings {
  const AppSettings({
    required this.clipboardMode,
    required this.notificationsEnabled,
    required this.updatedUnix,
    required this.updatedBy,
  });

  /// Wire values: off|mac_to_phone|phone_to_mac|two_way.
  final String clipboardMode;
  final bool notificationsEnabled;
  final int updatedUnix;
  final String updatedBy;

  static const off = 'off';
  static const macToPhone = 'mac_to_phone';
  static const phoneToMac = 'phone_to_mac';
  static const twoWay = 'two_way';

  static const validModes = <String>[off, macToPhone, phoneToMac, twoWay];

  /// First-launch defaults: two-way clipboard, notifications on.
  factory AppSettings.defaults({required int nowUnix}) => AppSettings(
        clipboardMode: twoWay,
        notificationsEnabled: true,
        updatedUnix: nowUnix,
        updatedBy: 'android',
      );

  /// Fail-soft decode: unknown modes fall back to two_way, absent
  /// notifications_enabled means true (pre-toggle peers), negative
  /// timestamps clamp to 0. Never throws.
  factory AppSettings.fromJson(Map<String, dynamic> json) {
    final rawMode = (json['clipboard_mode'] as String? ?? '').trim();
    final mode = validModes.contains(rawMode) ? rawMode : twoWay;
    final notif = json['notifications_enabled'];
    final ts = json['updated_unix'];
    return AppSettings(
      clipboardMode: mode,
      notificationsEnabled: notif is bool ? notif : true,
      updatedUnix: ts is int && ts >= 0 ? ts : 0,
      updatedBy: normalizeUpdatedBy(json['updated_by'] as String? ?? ''),
    );
  }

  Map<String, dynamic> toJson() => {
        'clipboard_mode': clipboardMode,
        'notifications_enabled': notificationsEnabled,
        'updated_unix': updatedUnix,
        'updated_by': updatedBy,
      };

  AppSettings withMode(String mode, {required int nowUnix}) => AppSettings(
        clipboardMode: mode,
        notificationsEnabled: notificationsEnabled,
        updatedUnix: nowUnix,
        updatedBy: 'android',
      );

  AppSettings withNotifications(bool enabled, {required int nowUnix}) =>
      AppSettings(
        clipboardMode: clipboardMode,
        notificationsEnabled: enabled,
        updatedUnix: nowUnix,
        updatedBy: 'android',
      );
}

/// Short consumer label with arrow icon hint. Pure.
String clipboardModeLabel(String mode) {
  switch (mode) {
    case AppSettings.macToPhone:
      return 'Mac → Phone';
    case AppSettings.phoneToMac:
      return '← Phone';
    case AppSettings.twoWay:
      return 'Two-way ⇄';
    default:
      return 'Off ∅';
  }
}

/// Arrow glyph for the sidebar/detail indicator. Pure.
String clipboardModeArrow(String mode) {
  switch (mode) {
    case AppSettings.macToPhone:
      return '→';
    case AppSettings.phoneToMac:
      return '←';
    case AppSettings.twoWay:
      return '⇄';
    default:
      return '∅';
  }
}

/// Normalize updated_by/origin: macos == mac. Pure.
String normalizeUpdatedBy(String s) {
  final n = s.trim().toLowerCase();
  if (n == 'macos') return 'mac';
  return n;
}

/// Last-writer-wins: greater timestamp wins; ties go to mac. Pure.
bool remoteSettingsWins(AppSettings local, AppSettings remote) {
  if (remote.updatedUnix != local.updatedUnix) {
    return remote.updatedUnix > local.updatedUnix;
  }
  final remoteMac = normalizeUpdatedBy(remote.updatedBy) == 'mac';
  final localMac = normalizeUpdatedBy(local.updatedBy) == 'mac';
  if (remoteMac != localMac) return remoteMac;
  return false;
}

/// Whether a push may flow in [pushDir] under [mode]. Pure.
bool clipDirectionAllows(String mode, String pushDir) {
  switch (mode) {
    case AppSettings.twoWay:
      return pushDir == AppSettings.macToPhone ||
          pushDir == AppSettings.phoneToMac;
    case AppSettings.macToPhone:
    case AppSettings.phoneToMac:
      return mode == pushDir;
    default:
      return false;
  }
}
