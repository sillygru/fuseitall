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
    required this.updatedUnix,
    required this.updatedBy,
  });

  final bool notificationsEnabled;
  final int updatedUnix;
  final String updatedBy;

  /// First-launch defaults: notifications on.
  factory AppSettings.defaults({required int nowUnix}) => AppSettings(
        notificationsEnabled: true,
        updatedUnix: nowUnix,
        updatedBy: 'android',
      );

  /// Fail-soft decode: absent notifications_enabled means true,
  /// negative timestamps clamp to 0. Never throws. Ignores old
  /// clipboard_mode field for compat.
  factory AppSettings.fromJson(Map<String, dynamic> json) {
    final notif = json['notifications_enabled'];
    final ts = json['updated_unix'];
    return AppSettings(
      notificationsEnabled: notif is bool ? notif : true,
      updatedUnix: ts is int && ts >= 0 ? ts : 0,
      updatedBy: normalizeUpdatedBy(json['updated_by'] as String? ?? ''),
    );
  }

  Map<String, dynamic> toJson() => {
        'notifications_enabled': notificationsEnabled,
        'updated_unix': updatedUnix,
        'updated_by': updatedBy,
      };

  AppSettings withNotifications(bool enabled, {required int nowUnix}) =>
      AppSettings(
        notificationsEnabled: enabled,
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
