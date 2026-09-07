// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// App settings synced with the Mac (last-writer-wins). Mirrors
// packages/proto/settings.json and core SanitizeSettings/RemoteSettingsWins:
// greater updatedUnix wins, ties go to mac.
class AppSettings {
  const AppSettings({
    required this.notificationsEnabled,
    required this.clipboardMode,
    required this.updatedUnix,
    required this.updatedBy,
  });

  final bool notificationsEnabled;
  final String clipboardMode;
  final int updatedUnix;
  final String updatedBy;

  static const both = 'both';
  static const macToAndroid = 'mac_to_android';
  static const androidToMac = 'android_to_mac';
  static const disabled = 'disabled';
  static const validModes = {
    both,
    macToAndroid,
    androidToMac,
    disabled,
  };

  static String normalizeClipboardMode(String s) {
    final n = s.trim().toLowerCase();
    return validModes.contains(n) ? n : both;
  }

  static bool allowsSend(String mode, String origin) {
    final m = normalizeClipboardMode(mode);
    final o = origin.trim().toLowerCase() == 'macos' ? 'mac' : origin.trim().toLowerCase();
    switch (m) {
      case disabled:
        return false;
      case macToAndroid:
        return o == 'mac';
      case androidToMac:
        return o == 'android';
      default:
        return true;
    }
  }

  static bool allowsReceive(String mode, String origin) => allowsSend(mode, origin);

  /// First-launch defaults: notifications on, clipboard both.
  factory AppSettings.defaults({required int nowUnix}) => const AppSettings(
        notificationsEnabled: true,
        clipboardMode: both,
        updatedUnix: 0,
        updatedBy: 'android',
      )._withNow(nowUnix);

  AppSettings _withNow(int nowUnix) => AppSettings(
        notificationsEnabled: notificationsEnabled,
        clipboardMode: clipboardMode,
        updatedUnix: nowUnix,
        updatedBy: 'android',
      );

  /// Fail-soft decode: absent notifications_enabled means true,
  /// absent clipboard_mode means both, negative timestamps clamp to 0.
  factory AppSettings.fromJson(Map<String, dynamic> json) {
    final notif = json['notifications_enabled'];
    final mode = json['clipboard_mode'];
    final ts = json['updated_unix'];
    return AppSettings(
      notificationsEnabled: notif is bool ? notif : true,
      clipboardMode: mode is String ? normalizeClipboardMode(mode) : both,
      updatedUnix: ts is int && ts >= 0 ? ts : 0,
      updatedBy: normalizeUpdatedBy(json['updated_by'] as String? ?? ''),
    );
  }

  Map<String, dynamic> toJson() => {
        'notifications_enabled': notificationsEnabled,
        'clipboard_mode': clipboardMode,
        'updated_unix': updatedUnix,
        'updated_by': updatedBy,
      };

  AppSettings withNotifications(bool enabled, {required int nowUnix}) => AppSettings(
        notificationsEnabled: enabled,
        clipboardMode: clipboardMode,
        updatedUnix: nowUnix,
        updatedBy: 'android',
      );

  AppSettings withClipboardMode(String mode, {required int nowUnix}) => AppSettings(
        notificationsEnabled: notificationsEnabled,
        clipboardMode: normalizeClipboardMode(mode),
        updatedUnix: nowUnix,
        updatedBy: 'android',
      );
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
