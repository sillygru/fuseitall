// SPDX-License-Identifier: AGPL-3.0-only

// App settings synced with the Mac (last-writer-wins). Mirrors
// packages/proto/settings.json and core SanitizeSettings/RemoteSettingsWins:
// greater updatedUnix wins, ties go to mac. notifMode + muted/allowed
// (0.9.0+) carry the per-app notification filter; absent means allow-all.
// playbackMode + playbackOutput (0.10.0+) carry playback direction
// (absent means phone-to-Mac view-only) and Mac presentation (absent inapp).
class AppSettings {
  const AppSettings({
    required this.notificationsEnabled,
    required this.clipboardMode,
    required this.notifMode,
    required this.mutedPackages,
    required this.allowedPackages,
    required this.playbackMode,
    required this.playbackOutput,
    required this.updatedUnix,
    required this.updatedBy,
    this.clipboardAllowSensitive = false,
    this.clipboardAutoBackground = false,
    this.themeMode = themeModeSystem,
  });

  final bool notificationsEnabled;
  final String clipboardMode;
  final String notifMode;
  final Set<String> mutedPackages;
  final Set<String> allowedPackages;
  final String playbackMode;
  final String playbackOutput;
  final int updatedUnix;
  final String updatedBy;
  /// Local theme mode: 'system', 'light', 'dark'. Not sent over wire.
  final String themeMode;
  /// Opt in to auto-syncing OS-flagged secrets. Default false: auto skips
  /// loud, manual Send always bypasses.
  final bool clipboardAllowSensitive;

  /// Opt-in automatic phone to Mac clipboard in background (local-only, never
  /// synced: needs one-time adb READ_LOGS grant + overlay allowed). Default
  /// false: one-tap Send via notification or Quick Settings tile instead.
  final bool clipboardAutoBackground;

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

  /// Playback default is two-way show + control (0.12.0+ decision, matching clipboard).
  static const playbackDefault = both;
  static const playbackOutputInApp = 'inapp';
  static const playbackOutputSystem = 'system';

  static const notifAllExceptMuted = 'all_except_muted';
  static const notifOnlyAllowed = 'only_allowed';
  static const maxFilterApps = 100;

  static const themeModeSystem = 'system';
  static const themeModeLight = 'light';
  static const themeModeDark = 'dark';
  static const validThemeModes = {
    themeModeSystem,
    themeModeLight,
    themeModeDark,
  };

  static String normalizeThemeMode(String? s) {
    if (s == null) return themeModeSystem;
    final n = s.trim().toLowerCase();
    return validThemeModes.contains(n) ? n : themeModeSystem;
  }

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

  static String normalizeNotifMode(String s) {
    final n = s.trim().toLowerCase();
    if (n == notifOnlyAllowed) return notifOnlyAllowed;
    return notifAllExceptMuted;
  }

  static String normalizePlaybackMode(String s) {
    final n = s.trim().toLowerCase();
    return validModes.contains(n) ? n : playbackDefault;
  }

  static String normalizePlaybackOutput(String s) {
    final n = s.trim().toLowerCase();
    if (n == playbackOutputSystem) return playbackOutputSystem;
    return playbackOutputInApp;
  }

  static bool playbackAllowsState(String mode) {
    final m = normalizePlaybackMode(mode);
    return m == both || m == androidToMac;
  }

  static bool playbackAllowsCommand(String mode) {
    final m = normalizePlaybackMode(mode);
    return m == both || m == macToAndroid;
  }

  static Set<String> sanitizeFilterList(Iterable<String>? input) {
    if (input == null) return const {};
    final out = <String>{};
    for (final raw in input) {
      final pkg = raw.trim();
      if (pkg.isEmpty || pkg.length > 128) continue;
      out.add(pkg);
      if (out.length >= maxFilterApps) break;
    }
    return out;
  }

  /// Canonical per-app predicate, mirrors core.ShouldMirrorNotif. Pure.
  bool shouldMirrorNotif(String packageName, {bool hasProgress = false}) {
    if (hasProgress) return false;
    final pkg = packageName.trim();
    if (notifMode == notifOnlyAllowed) {
      if (pkg.isEmpty) return false;
      return allowedPackages.contains(pkg);
    }
    if (pkg.isEmpty) return true;
    return !mutedPackages.contains(pkg);
  }

  /// First-launch defaults: notifications on, clipboard both, sensitive auto
  /// off, filter allow-all, playback both + in-app output.
  factory AppSettings.defaults({required int nowUnix}) => AppSettings(
        notificationsEnabled: true,
        clipboardMode: both,
        notifMode: notifAllExceptMuted,
        mutedPackages: const {},
        allowedPackages: const {},
        playbackMode: playbackDefault,
        playbackOutput: playbackOutputInApp,
        updatedUnix: nowUnix,
        updatedBy: 'android',
      );

  /// Fail-soft decode: absent notifications_enabled means true,
  /// absent clipboard_mode means both, absent notif filter means allow-all,
  /// absent playback_mode means phone-to-Mac, absent output means inapp,
  /// negative timestamps clamp to 0.
  factory AppSettings.fromJson(Map<String, dynamic> json) {
    final notif = json['notifications_enabled'];
    final mode = json['clipboard_mode'];
    final ts = json['updated_unix'];
    Set<String> readList(Object? v) {
      if (v is! List) return const {};
      return sanitizeFilterList(v.whereType<String>());
    }
    final pm = json['playback_mode'];
    final po = json['playback_output'];

    return AppSettings(
      notificationsEnabled: notif is bool ? notif : true,
      clipboardMode: mode is String ? normalizeClipboardMode(mode) : both,
      notifMode: json['notif_mode'] is String
          ? normalizeNotifMode(json['notif_mode'] as String)
          : notifAllExceptMuted,
      mutedPackages: readList(json['muted_packages']),
      allowedPackages: readList(json['allowed_packages']),
      playbackMode:
          pm is String ? normalizePlaybackMode(pm) : playbackDefault,
      playbackOutput:
          po is String ? normalizePlaybackOutput(po) : playbackOutputInApp,
      updatedUnix: ts is int && ts >= 0 ? ts : 0,
      updatedBy: normalizeUpdatedBy(json['updated_by'] as String? ?? ''),
      clipboardAllowSensitive: json['clipboard_allow_sensitive'] == true,
      clipboardAutoBackground: json['clipboard_auto_background'] == true,
      themeMode: normalizeThemeMode(json['theme_mode'] as String?),
    );
  }

  Map<String, dynamic> toJson() {
    final muted = mutedPackages.toList()..sort();
    final allowed = allowedPackages.toList()..sort();
    return {
      'notifications_enabled': notificationsEnabled,
      'clipboard_mode': clipboardMode,
      'notif_mode': notifMode,
      'muted_packages': muted,
      'allowed_packages': allowed,
      'playback_mode': playbackMode,
      'playback_output': playbackOutput,
      'updated_unix': updatedUnix,
      'updated_by': updatedBy,
      if (clipboardAllowSensitive) 'clipboard_allow_sensitive': true,
      if (clipboardAutoBackground) 'clipboard_auto_background': true,
      if (themeMode != themeModeSystem) 'theme_mode': themeMode,
    };
  }

  /// Wire payload for settings-sync: strips the local-only background auto
  /// and theme toggles so packages/proto stays the only cross-language contract.
  Map<String, dynamic> toSyncJson() {
    final m = toJson();
    m.remove('clipboard_auto_background');
    m.remove('theme_mode');
    return m;
  }

  AppSettings withNotifications(bool enabled, {required int nowUnix}) => AppSettings(
        notificationsEnabled: enabled,
        clipboardMode: clipboardMode,
        notifMode: notifMode,
        mutedPackages: mutedPackages,
        allowedPackages: allowedPackages,
        playbackMode: playbackMode,
        playbackOutput: playbackOutput,
        updatedUnix: nowUnix,
        updatedBy: 'android',
        clipboardAllowSensitive: clipboardAllowSensitive,
        clipboardAutoBackground: clipboardAutoBackground,
        themeMode: themeMode,
      );

  AppSettings withClipboardMode(String mode, {required int nowUnix}) => AppSettings(
        notificationsEnabled: notificationsEnabled,
        clipboardMode: normalizeClipboardMode(mode),
        notifMode: notifMode,
        mutedPackages: mutedPackages,
        allowedPackages: allowedPackages,
        playbackMode: playbackMode,
        playbackOutput: playbackOutput,
        updatedUnix: nowUnix,
        updatedBy: 'android',
        clipboardAllowSensitive: clipboardAllowSensitive,
        clipboardAutoBackground: clipboardAutoBackground,
        themeMode: themeMode,
      );

  AppSettings withClipboardAllowSensitive(bool allow, {required int nowUnix}) => AppSettings(
        notificationsEnabled: notificationsEnabled,
        clipboardMode: clipboardMode,
        notifMode: notifMode,
        mutedPackages: mutedPackages,
        allowedPackages: allowedPackages,
        playbackMode: playbackMode,
        playbackOutput: playbackOutput,
        updatedUnix: nowUnix,
        updatedBy: 'android',
        clipboardAllowSensitive: allow,
        clipboardAutoBackground: clipboardAutoBackground,
        themeMode: themeMode,
      );

  AppSettings withClipboardAutoBackground(bool allow, {required int nowUnix}) => AppSettings(
        notificationsEnabled: notificationsEnabled,
        clipboardMode: clipboardMode,
        notifMode: notifMode,
        mutedPackages: mutedPackages,
        allowedPackages: allowedPackages,
        playbackMode: playbackMode,
        playbackOutput: playbackOutput,
        updatedUnix: nowUnix,
        updatedBy: 'android',
        clipboardAllowSensitive: clipboardAllowSensitive,
        clipboardAutoBackground: allow,
        themeMode: themeMode,
      );

  /// Keeps local-only flags when adopting a remote sync payload: the adopted
  /// stamp stays remote (last-writer-wins untouched), only device-local
  /// toggles carry over. Pure.
  AppSettings withLocalFlagsFrom(AppSettings local) => AppSettings(
        notificationsEnabled: notificationsEnabled,
        clipboardMode: clipboardMode,
        notifMode: notifMode,
        mutedPackages: mutedPackages,
        allowedPackages: allowedPackages,
        playbackMode: playbackMode,
        playbackOutput: playbackOutput,
        updatedUnix: updatedUnix,
        updatedBy: updatedBy,
        clipboardAllowSensitive: clipboardAllowSensitive,
        clipboardAutoBackground: local.clipboardAutoBackground,
        themeMode: local.themeMode,
      );

  AppSettings withNotifMode(String mode, {required int nowUnix}) => AppSettings(
        notificationsEnabled: notificationsEnabled,
        clipboardMode: clipboardMode,
        notifMode: normalizeNotifMode(mode),
        mutedPackages: mutedPackages,
        allowedPackages: allowedPackages,
        playbackMode: playbackMode,
        playbackOutput: playbackOutput,
        updatedUnix: nowUnix,
        updatedBy: 'android',
        clipboardAllowSensitive: clipboardAllowSensitive,
        clipboardAutoBackground: clipboardAutoBackground,
        themeMode: themeMode,
      );

  AppSettings withPlaybackOutput(String output, {required int nowUnix}) => AppSettings(
        notificationsEnabled: notificationsEnabled,
        clipboardMode: clipboardMode,
        notifMode: notifMode,
        mutedPackages: mutedPackages,
        allowedPackages: allowedPackages,
        playbackMode: playbackMode,
        playbackOutput: normalizePlaybackOutput(output),
        updatedUnix: nowUnix,
        updatedBy: 'android',
        clipboardAllowSensitive: clipboardAllowSensitive,
        clipboardAutoBackground: clipboardAutoBackground,
        themeMode: themeMode,
      );

  AppSettings withMutedToggled(String pkg, {required int nowUnix}) {
    final p = pkg.trim();
    final next = Set<String>.from(mutedPackages);
    if (p.isNotEmpty) {
      if (!next.remove(p) && next.length < maxFilterApps) next.add(p);
    }
    return AppSettings(
      notificationsEnabled: notificationsEnabled,
      clipboardMode: clipboardMode,
      notifMode: notifMode,
      mutedPackages: next,
      allowedPackages: allowedPackages,
      playbackMode: playbackMode,
      playbackOutput: playbackOutput,
      updatedUnix: nowUnix,
      updatedBy: 'android',
      clipboardAllowSensitive: clipboardAllowSensitive,
      clipboardAutoBackground: clipboardAutoBackground,
      themeMode: themeMode,
    );
  }

  AppSettings withPlaybackMode(String mode, {required int nowUnix}) => AppSettings(
        notificationsEnabled: notificationsEnabled,
        clipboardMode: clipboardMode,
        notifMode: notifMode,
        mutedPackages: mutedPackages,
        allowedPackages: allowedPackages,
        playbackMode: normalizePlaybackMode(mode),
        playbackOutput: playbackOutput,
        updatedUnix: nowUnix,
        updatedBy: 'android',
        clipboardAllowSensitive: clipboardAllowSensitive,
        clipboardAutoBackground: clipboardAutoBackground,
        themeMode: themeMode,
      );

  AppSettings withAllowedToggled(String pkg, {required int nowUnix}) {
    final p = pkg.trim();
    final next = Set<String>.from(allowedPackages);
    if (p.isNotEmpty) {
      if (!next.remove(p) && next.length < maxFilterApps) next.add(p);
    }
    return AppSettings(
      notificationsEnabled: notificationsEnabled,
      clipboardMode: clipboardMode,
      notifMode: notifMode,
      mutedPackages: mutedPackages,
      allowedPackages: next,
      playbackMode: playbackMode,
      playbackOutput: playbackOutput,
      updatedUnix: nowUnix,
      updatedBy: 'android',
      clipboardAllowSensitive: clipboardAllowSensitive,
      clipboardAutoBackground: clipboardAutoBackground,
      themeMode: themeMode,
    );
  }

  AppSettings withThemeMode(String mode, {required int nowUnix}) => AppSettings(
        notificationsEnabled: notificationsEnabled,
        clipboardMode: clipboardMode,
        notifMode: notifMode,
        mutedPackages: mutedPackages,
        allowedPackages: allowedPackages,
        playbackMode: playbackMode,
        playbackOutput: playbackOutput,
        updatedUnix: nowUnix,
        updatedBy: 'android',
        clipboardAllowSensitive: clipboardAllowSensitive,
        clipboardAutoBackground: clipboardAutoBackground,
        themeMode: normalizeThemeMode(mode),
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
