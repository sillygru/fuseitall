// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Reading this as: settings content for Settings tab / sheet, following HIG 4/6/9
// with studio-grade light/dark theme selection and inset grouped cards.

// ignore_for_file: deprecated_member_use

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../widgets/haptics.dart';
import '../notifications/notif_listener.dart';
import 'app_settings.dart';

class SettingsPage extends StatelessWidget {
  const SettingsPage({
    required this.deviceName,
    required this.settings,
    required this.onNotificationsChanged,
    this.onClipboardModeChanged,
    this.onClipboardAllowSensitiveChanged,
    this.clipboardAutoBackground = false,
    this.onClipboardAutoBackgroundChanged,
    this.clipAutoStatus,
    this.onOpenOverlaySettings,
    this.onNotifModeChanged,
    this.onMutedToggled,
    this.onAllowedToggled,
    this.onPlaybackModeChanged,
    this.onThemeModeChanged,
    required this.onUnpair,
    super.key,
  });

  final String deviceName;
  final AppSettings? settings;
  final ValueChanged<bool> onNotificationsChanged;
  final ValueChanged<String>? onClipboardModeChanged;
  final ValueChanged<bool>? onClipboardAllowSensitiveChanged;
  final bool clipboardAutoBackground;
  final ValueChanged<bool>? onClipboardAutoBackgroundChanged;
  final String? clipAutoStatus;
  final VoidCallback? onOpenOverlaySettings;
  final ValueChanged<String>? onNotifModeChanged;
  final ValueChanged<String>? onMutedToggled;
  final ValueChanged<String>? onAllowedToggled;
  final ValueChanged<String>? onPlaybackModeChanged;
  final ValueChanged<String>? onThemeModeChanged;
  final VoidCallback onUnpair;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Settings'),
        scrolledUnderElevation: 0,
      ),
      body: SafeArea(
        child: SettingsBody(
          deviceName: deviceName,
          settings: settings,
          onNotificationsChanged: onNotificationsChanged,
          onClipboardModeChanged: onClipboardModeChanged,
          onClipboardAllowSensitiveChanged: onClipboardAllowSensitiveChanged,
          clipboardAutoBackground: clipboardAutoBackground,
          onClipboardAutoBackgroundChanged: onClipboardAutoBackgroundChanged,
          clipAutoStatus: clipAutoStatus,
          onOpenOverlaySettings: onOpenOverlaySettings,
          onNotifModeChanged: onNotifModeChanged,
          onMutedToggled: onMutedToggled,
          onAllowedToggled: onAllowedToggled,
          onPlaybackModeChanged: onPlaybackModeChanged,
          onThemeModeChanged: onThemeModeChanged,
          onUnpair: onUnpair,
        ),
      ),
    );
  }
}

/// Inline variant for use inside RootShell without its own Scaffold/AppBar.
class SettingsContent extends StatelessWidget {
  const SettingsContent({
    required this.deviceName,
    required this.settings,
    required this.onNotificationsChanged,
    this.onClipboardModeChanged,
    this.onClipboardAllowSensitiveChanged,
    this.clipboardAutoBackground = false,
    this.onClipboardAutoBackgroundChanged,
    this.clipAutoStatus,
    this.onOpenOverlaySettings,
    this.onNotifModeChanged,
    this.onMutedToggled,
    this.onAllowedToggled,
    this.onPlaybackModeChanged,
    this.onThemeModeChanged,
    required this.onUnpair,
    super.key,
  });

  final String deviceName;
  final AppSettings? settings;
  final ValueChanged<bool> onNotificationsChanged;
  final ValueChanged<String>? onClipboardModeChanged;
  final ValueChanged<bool>? onClipboardAllowSensitiveChanged;
  final bool clipboardAutoBackground;
  final ValueChanged<bool>? onClipboardAutoBackgroundChanged;
  final String? clipAutoStatus;
  final VoidCallback? onOpenOverlaySettings;
  final ValueChanged<String>? onNotifModeChanged;
  final ValueChanged<String>? onMutedToggled;
  final ValueChanged<String>? onAllowedToggled;
  final ValueChanged<String>? onPlaybackModeChanged;
  final ValueChanged<String>? onThemeModeChanged;
  final VoidCallback onUnpair;

  @override
  Widget build(BuildContext context) {
    return SettingsBody(
      deviceName: deviceName,
      settings: settings,
      onNotificationsChanged: onNotificationsChanged,
      onClipboardModeChanged: onClipboardModeChanged,
      onClipboardAllowSensitiveChanged: onClipboardAllowSensitiveChanged,
      clipboardAutoBackground: clipboardAutoBackground,
      onClipboardAutoBackgroundChanged: onClipboardAutoBackgroundChanged,
      clipAutoStatus: clipAutoStatus,
      onOpenOverlaySettings: onOpenOverlaySettings,
      onNotifModeChanged: onNotifModeChanged,
      onMutedToggled: onMutedToggled,
      onAllowedToggled: onAllowedToggled,
      onPlaybackModeChanged: onPlaybackModeChanged,
      onThemeModeChanged: onThemeModeChanged,
      onUnpair: onUnpair,
    );
  }
}

/// Shared settings body: Appearance + master switch + per-app filter + clipboard + playback + unpair.
class SettingsBody extends StatelessWidget {
  const SettingsBody({
    required this.deviceName,
    required this.settings,
    required this.onNotificationsChanged,
    this.onClipboardModeChanged,
    this.onClipboardAllowSensitiveChanged,
    this.clipboardAutoBackground = false,
    this.onClipboardAutoBackgroundChanged,
    this.clipAutoStatus,
    this.onOpenOverlaySettings,
    this.onNotifModeChanged,
    this.onMutedToggled,
    this.onAllowedToggled,
    this.onPlaybackModeChanged,
    this.onThemeModeChanged,
    required this.onUnpair,
    super.key,
  });

  final String deviceName;
  final AppSettings? settings;
  final ValueChanged<bool> onNotificationsChanged;
  final ValueChanged<String>? onClipboardModeChanged;
  final ValueChanged<bool>? onClipboardAllowSensitiveChanged;
  final bool clipboardAutoBackground;
  final ValueChanged<bool>? onClipboardAutoBackgroundChanged;
  final String? clipAutoStatus;
  final VoidCallback? onOpenOverlaySettings;
  final ValueChanged<String>? onNotifModeChanged;
  final ValueChanged<String>? onMutedToggled;
  final ValueChanged<String>? onAllowedToggled;
  final ValueChanged<String>? onPlaybackModeChanged;
  final ValueChanged<String>? onThemeModeChanged;
  final VoidCallback onUnpair;

  @override
  Widget build(BuildContext context) {
    final st = settings;
    final notifEnabled = st?.notificationsEnabled ?? true;
    final clipMode = st?.clipboardMode ?? AppSettings.both;
    final allowSensitive = st?.clipboardAllowSensitive ?? false;
    final playbackMode = st?.playbackMode ?? AppSettings.playbackDefault;
    final themeMode = st?.themeMode ?? AppSettings.themeModeSystem;
    final scheme = Theme.of(context).colorScheme;
    final isDark = scheme.brightness == Brightness.dark;

    return CustomScrollView(
      slivers: [
        SliverPadding(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
          sliver: SliverList.list(
            children: [
              // ── Appearance Section ──────────────────────────────
              _sectionHeader(context, 'Appearance', Icons.palette_outlined),
              Card(
                child: Padding(
                  padding: const EdgeInsets.all(16),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        'Theme',
                        style: Theme.of(context).textTheme.titleSmall?.copyWith(
                              fontWeight: FontWeight.w700,
                              letterSpacing: -0.2,
                            ),
                      ),
                      const SizedBox(height: 4),
                      Text(
                        'Choose between studio dark, warm porcelain light, or system auto.',
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                              color: scheme.onSurfaceVariant,
                            ),
                      ),
                      const SizedBox(height: 14),
                      SizedBox(
                        width: double.infinity,
                        child: SegmentedButton<String>(
                          segments: const [
                            ButtonSegment(
                              value: AppSettings.themeModeSystem,
                              label: Text('System'),
                              icon: Icon(Icons.brightness_auto_outlined, size: 16),
                            ),
                            ButtonSegment(
                              value: AppSettings.themeModeLight,
                              label: Text('Light'),
                              icon: Icon(Icons.light_mode_outlined, size: 16),
                            ),
                            ButtonSegment(
                              value: AppSettings.themeModeDark,
                              label: Text('Dark'),
                              icon: Icon(Icons.dark_mode_outlined, size: 16),
                            ),
                          ],
                          selected: {themeMode},
                          onSelectionChanged: onThemeModeChanged == null
                              ? null
                              : (s) {
                                  if (s.isNotEmpty) {
                                    AppHaptics.selection();
                                    onThemeModeChanged!(s.first);
                                  }
                                },
                        ),
                      ),
                    ],
                  ),
                ),
              ),

              const SizedBox(height: 20),

              // ── Notifications Section ───────────────────────────
              _sectionHeader(context, 'Notifications', Icons.notifications_outlined),
              Card(
                child: SwitchListTile(
                  title: Text(
                    'Phone notifications',
                    style: Theme.of(context).textTheme.titleSmall?.copyWith(
                          fontWeight: FontWeight.w700,
                          letterSpacing: -0.2,
                        ),
                  ),
                  subtitle: Text(
                    'Mirror to Mac',
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: scheme.onSurfaceVariant,
                        ),
                  ),
                  value: notifEnabled,
                  onChanged: (v) {
                    AppHaptics.selection();
                    onNotificationsChanged(v);
                  },
                ),
              ),
              if (notifEnabled && st != null) ...[
                const SizedBox(height: 10),
                NotifFilterSection(
                  settings: st,
                  onNotifModeChanged: onNotifModeChanged,
                  onMutedToggled: onMutedToggled,
                  onAllowedToggled: onAllowedToggled,
                ),
              ],

              const SizedBox(height: 20),

              // ── Clipboard Section ───────────────────────────────
              _sectionHeader(context, 'Clipboard', Icons.content_paste_outlined),
              Card(
                child: Column(
                  children: [
                    Padding(
                      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                      child: DropdownButtonFormField<String>(
                        initialValue: clipMode,
                        decoration: const InputDecoration(
                          labelText: 'Clipboard auto sync',
                          border: InputBorder.none,
                        ),
                        items: const [
                          DropdownMenuItem(value: 'both', child: Text('Both ways')),
                          DropdownMenuItem(value: 'android_to_mac', child: Text('Phone → Mac only')),
                          DropdownMenuItem(value: 'mac_to_android', child: Text('Mac → Phone only')),
                          DropdownMenuItem(value: 'disabled', child: Text('Disabled')),
                        ],
                        onChanged: onClipboardModeChanged == null
                            ? null
                            : (v) {
                                if (v != null) {
                                  AppHaptics.selection();
                                  onClipboardModeChanged!(v);
                                }
                              },
                      ),
                    ),
                    _itemDivider(isDark),
                    SwitchListTile(
                      title: Text(
                        'Auto-sync passwords and codes',
                        style: Theme.of(context).textTheme.titleSmall?.copyWith(
                              fontWeight: FontWeight.w600,
                            ),
                      ),
                      subtitle: Text(
                        'Off skips sensitive clips in auto sync. Manual Send always works.',
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                              color: scheme.onSurfaceVariant,
                            ),
                      ),
                      value: allowSensitive,
                      onChanged: onClipboardAllowSensitiveChanged == null
                          ? null
                          : (v) {
                              AppHaptics.selection();
                              onClipboardAllowSensitiveChanged!(v);
                            },
                    ),
                    _itemDivider(isDark),
                    SwitchListTile(
                      title: Text(
                        'Auto send in background',
                        style: Theme.of(context).textTheme.titleSmall?.copyWith(
                              fontWeight: FontWeight.w600,
                            ),
                      ),
                      subtitle: Text(
                        clipboardAutoBackground
                            ? 'On. Status: ${clipAutoStatus ?? 'checking'}.'
                            : 'Off. Needs a one-time computer setup, then copies send on their own.',
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                              color: scheme.onSurfaceVariant,
                            ),
                      ),
                      value: clipboardAutoBackground,
                      onChanged: onClipboardAutoBackgroundChanged == null
                          ? null
                          : (v) {
                              AppHaptics.selection();
                              onClipboardAutoBackgroundChanged!(v);
                            },
                    ),
                  ],
                ),
              ),

              const SizedBox(height: 6),
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Padding(
                      padding: const EdgeInsets.only(top: 2, right: 8),
                      child: Icon(Icons.info_outline_rounded, size: 14, color: scheme.onSurfaceVariant),
                    ),
                    Expanded(
                      child: Text(
                        'Android allows clipboard access only when the app is focused. Use the Quick Settings tile or FuseItAll notification to send on demand without opening the app.',
                        style: TextStyle(fontSize: 12, color: scheme.onSurfaceVariant, height: 1.35),
                      ),
                    ),
                  ],
                ),
              ),

              if (clipboardAutoBackground) ...[
                const SizedBox(height: 10),
                Card(
                  child: Padding(
                    padding: const EdgeInsets.all(16),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        Text(
                          'One-time setup on your computer (USB debugging):',
                          style: Theme.of(context).textTheme.titleSmall?.copyWith(
                                fontWeight: FontWeight.w700,
                                fontSize: 13,
                              ),
                        ),
                        const SizedBox(height: 8),
                        Container(
                          padding: const EdgeInsets.all(12),
                          decoration: BoxDecoration(
                            color: scheme.surfaceContainerHighest,
                            borderRadius: BorderRadius.circular(12),
                          ),
                          child: SelectableText(
                            'adb shell pm grant com.fuseitall.fuseitall android.permission.READ_LOGS\n'
                            'adb shell appops set com.fuseitall.fuseitall SYSTEM_ALERT_WINDOW allow\n'
                            'adb shell am force-stop com.fuseitall.fuseitall',
                            style: const TextStyle(
                              fontFamily: 'RobotoMono',
                              fontSize: 11,
                              height: 1.4,
                            ),
                          ),
                        ),
                        const SizedBox(height: 12),
                        Row(
                          children: [
                            FilledButton.tonal(
                              onPressed: () {
                                AppHaptics.light();
                                Clipboard.setData(const ClipboardData(
                                  text: 'adb shell pm grant com.fuseitall.fuseitall android.permission.READ_LOGS && '
                                      'adb shell appops set com.fuseitall.fuseitall SYSTEM_ALERT_WINDOW allow && '
                                      'adb shell am force-stop com.fuseitall.fuseitall',
                                ));
                              },
                              child: const Text('Copy adb commands'),
                            ),
                            const SizedBox(width: 8),
                            if (onOpenOverlaySettings != null)
                              TextButton(
                                onPressed: () {
                                  AppHaptics.light();
                                  onOpenOverlaySettings!();
                                },
                                child: const Text('Allow overlay'),
                              ),
                          ],
                        ),
                      ],
                    ),
                  ),
                ),
              ],

              const SizedBox(height: 20),

              // ── Playback Section ────────────────────────────────
              _sectionHeader(context, 'Playback', Icons.play_circle_outline_rounded),
              Card(
                child: Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                  child: DropdownButtonFormField<String>(
                    initialValue: playbackMode,
                    decoration: const InputDecoration(
                      labelText: 'Playback sync',
                      border: InputBorder.none,
                    ),
                    items: const [
                      DropdownMenuItem(value: 'both', child: Text('Both ways')),
                      DropdownMenuItem(value: 'android_to_mac', child: Text('Phone to Mac only')),
                      DropdownMenuItem(value: 'mac_to_android', child: Text('Mac to Phone only')),
                      DropdownMenuItem(value: 'disabled', child: Text('Off')),
                    ],
                    onChanged: onPlaybackModeChanged == null
                        ? null
                        : (v) {
                            if (v != null) {
                              AppHaptics.selection();
                              onPlaybackModeChanged!(v);
                            }
                          },
                  ),
                ),
              ),
              const SizedBox(height: 6),
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Padding(
                      padding: const EdgeInsets.only(top: 2, right: 8),
                      child: Icon(Icons.info_outline_rounded, size: 14, color: scheme.onSurfaceVariant),
                    ),
                    Expanded(
                      child: Text(
                        'Phone is the music source. Phone to Mac shows active playback on your Mac. Mac to Phone allows controlling playback from the Mac.',
                        style: TextStyle(fontSize: 12, color: scheme.onSurfaceVariant, height: 1.35),
                      ),
                    ),
                  ],
                ),
              ),

              const SizedBox(height: 24),

              // ── Danger Zone / Unpair ────────────────────────────
              Card(
                child: ListTile(
                  leading: Container(
                    padding: const EdgeInsets.all(8),
                    decoration: BoxDecoration(
                      color: scheme.errorContainer,
                      borderRadius: BorderRadius.circular(12),
                    ),
                    child: Icon(Icons.link_off_rounded, color: scheme.error, size: 20),
                  ),
                  title: Text(
                    'Unpair Mac',
                    style: Theme.of(context).textTheme.titleSmall?.copyWith(
                          color: scheme.error,
                          fontWeight: FontWeight.w700,
                          letterSpacing: -0.2,
                        ),
                  ),
                  subtitle: Text(deviceName, style: TextStyle(color: scheme.onSurfaceVariant)),
                  trailing: const Icon(Icons.chevron_right_rounded),
                  onTap: () {
                    AppHaptics.error();
                    onUnpair();
                  },
                ),
              ),
              const SizedBox(height: 24),
            ],
          ),
        ),
      ],
    );
  }

  Widget _sectionHeader(BuildContext context, String title, IconData icon) {
    final scheme = Theme.of(context).colorScheme;
    return Padding(
      padding: const EdgeInsets.fromLTRB(6, 4, 6, 8),
      child: Row(
        children: [
          Icon(icon, size: 16, color: scheme.onSurfaceVariant),
          const SizedBox(width: 6),
          Text(
            title,
            style: Theme.of(context).textTheme.labelMedium?.copyWith(
                  fontWeight: FontWeight.w700,
                  color: scheme.onSurfaceVariant,
                  letterSpacing: 0.4,
                ),
          ),
        ],
      ),
    );
  }

  Widget _itemDivider(bool isDark) => Divider(
        color: isDark ? const Color(0x18FFFFFF) : const Color(0x0F000000),
        height: 1,
        thickness: 0.5,
      );
}

/// Per-app notification filter: mode switch + searchable toggle list.
class NotifFilterSection extends StatefulWidget {
  const NotifFilterSection({
    required this.settings,
    this.onNotifModeChanged,
    this.onMutedToggled,
    this.onAllowedToggled,
    super.key,
  });

  final AppSettings settings;
  final ValueChanged<String>? onNotifModeChanged;
  final ValueChanged<String>? onMutedToggled;
  final ValueChanged<String>? onAllowedToggled;

  @override
  State<NotifFilterSection> createState() => _NotifFilterSectionState();
}

class _NotifFilterSectionState extends State<NotifFilterSection> {
  final _query = TextEditingController();
  var _apps = const <Map<String, String>>[];
  var _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _query.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final apps = await NotifListener().listApps();
      if (!mounted) return;
      setState(() {
        _apps = apps;
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = '$e';
        _loading = false;
      });
    }
  }

  List<Map<String, String>> _visible() {
    final st = widget.settings;
    final onlyAllowed = st.notifMode == AppSettings.notifOnlyAllowed;
    final merged = <String, Map<String, String>>{};
    for (final a in _apps) {
      final pkg = (a['package_name'] ?? '').trim();
      if (pkg.isEmpty) continue;
      merged[pkg] = a;
    }
    for (final pkg in {...st.mutedPackages, ...st.allowedPackages}) {
      merged.putIfAbsent(pkg, () => {'package_name': pkg, 'app': pkg});
    }
    final q = _query.text.trim().toLowerCase();
    final list = merged.values.where((a) {
      if (q.isEmpty) return true;
      return (a['app'] ?? '').toLowerCase().contains(q) ||
          (a['package_name'] ?? '').toLowerCase().contains(q);
    }).toList()
      ..sort((a, b) {
        final ao = _isOn(a['package_name'] ?? '', onlyAllowed, st) ? 0 : 1;
        final bo = _isOn(b['package_name'] ?? '', onlyAllowed, st) ? 0 : 1;
        if (ao != bo) return ao - bo;
        return (a['app'] ?? '').toLowerCase().compareTo((b['app'] ?? '').toLowerCase());
      });
    return list.take(100).toList();
  }

  bool _isOn(String pkg, bool onlyAllowed, AppSettings st) {
    if (onlyAllowed) return st.allowedPackages.contains(pkg);
    return !st.mutedPackages.contains(pkg);
  }

  @override
  Widget build(BuildContext context) {
    final st = widget.settings;
    final scheme = Theme.of(context).colorScheme;
    final onlyAllowed = st.notifMode == AppSettings.notifOnlyAllowed;
    final count = onlyAllowed ? st.allowedPackages.length : st.mutedPackages.length;

    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    onlyAllowed ? 'Allowed apps ($count)' : count > 0 ? 'Muted apps ($count)' : 'All apps mirror',
                    style: Theme.of(context).textTheme.titleSmall?.copyWith(
                          fontWeight: FontWeight.w700,
                          letterSpacing: -0.2,
                        ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 4),
            Text(
              'Download progress never mirrors.',
              style: TextStyle(fontSize: 12, color: scheme.onSurfaceVariant),
            ),
            const SizedBox(height: 12),
            SegmentedButton<String>(
              segments: const [
                ButtonSegment(
                  value: AppSettings.notifAllExceptMuted,
                  label: Text('All except muted'),
                  icon: Icon(Icons.notifications_active_outlined, size: 16),
                ),
                ButtonSegment(
                  value: AppSettings.notifOnlyAllowed,
                  label: Text('Only allowed'),
                  icon: Icon(Icons.notifications_paused_outlined, size: 16),
                ),
              ],
              selected: {st.notifMode},
              onSelectionChanged: widget.onNotifModeChanged == null
                  ? null
                  : (s) {
                      if (s.isNotEmpty) {
                        AppHaptics.selection();
                        widget.onNotifModeChanged!(s.first);
                      }
                    },
            ),
            const SizedBox(height: 12),
            Container(
              decoration: BoxDecoration(
                color: scheme.surfaceContainerHighest,
                borderRadius: BorderRadius.circular(14),
              ),
              padding: const EdgeInsets.symmetric(horizontal: 12),
              child: TextField(
                controller: _query,
                decoration: const InputDecoration(
                  hintText: 'Search apps',
                  prefixIcon: Icon(Icons.search, size: 18),
                  border: InputBorder.none,
                  isDense: true,
                ),
                onChanged: (_) => setState(() {}),
              ),
            ),
            const SizedBox(height: 8),
            if (_loading)
              const Padding(
                padding: EdgeInsets.symmetric(vertical: 16),
                child: Center(child: SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2))),
              )
            else if (_error != null)
              SelectableText.rich(
                TextSpan(
                  children: [
                    const TextSpan(text: 'Could not list apps. Saved toggles still apply.\n'),
                    TextSpan(text: _error, style: TextStyle(color: scheme.onSurfaceVariant, fontSize: 12)),
                  ],
                ),
                style: Theme.of(context).textTheme.bodySmall,
              )
            else
              ..._rows(context, _visible(), onlyAllowed),
          ],
        ),
      ),
    );
  }

  List<Widget> _rows(BuildContext context, List<Map<String, String>> apps, bool onlyAllowed) {
    final scheme = Theme.of(context).colorScheme;
    if (apps.isEmpty) {
      return [
        Padding(
          padding: const EdgeInsets.symmetric(vertical: 12),
          child: Text(
            _query.text.trim().isEmpty
                ? 'Apps appear here once one is installed. Saved toggles still apply.'
                : 'No apps match this search.',
            style: Theme.of(context).textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant),
          ),
        ),
      ];
    }
    return [
      for (final a in apps)
        Builder(builder: (context) {
          final pkg = a['package_name'] ?? '';
          final on = _isOn(pkg, onlyAllowed, widget.settings);
          return SwitchListTile(
            contentPadding: EdgeInsets.zero,
            dense: true,
            title: Text(
              a['app'] ?? pkg,
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                    fontWeight: FontWeight.w600,
                  ),
            ),
            subtitle: Text(
              pkg,
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                    fontSize: 11,
                  ),
            ),
            value: on,
            onChanged: (_) {
              AppHaptics.selection();
              if (onlyAllowed) {
                widget.onAllowedToggled?.call(pkg);
              } else {
                widget.onMutedToggled?.call(pkg);
              }
            },
          );
        }),
    ];
  }
}
