// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Reading this as: settings content for Settings tab / sheet, following HIG 4/6/9.

// ignore_for_file: deprecated_member_use

import 'package:flutter/material.dart';

import '../notifications/notif_listener.dart';
import 'app_settings.dart';

class SettingsPage extends StatelessWidget {
  const SettingsPage({
    required this.deviceName,
    required this.settings,
    required this.onNotificationsChanged,
    this.onClipboardModeChanged,
    this.onClipboardAllowSensitiveChanged,
    this.onNotifModeChanged,
    this.onMutedToggled,
    this.onAllowedToggled,
    this.onPlaybackModeChanged,
    required this.onUnpair,
    super.key,
  });

  final String deviceName;
  final AppSettings? settings;
  final ValueChanged<bool> onNotificationsChanged;
  final ValueChanged<String>? onClipboardModeChanged;
  final ValueChanged<bool>? onClipboardAllowSensitiveChanged;
  final ValueChanged<String>? onNotifModeChanged;
  final ValueChanged<String>? onMutedToggled;
  final ValueChanged<String>? onAllowedToggled;
  final ValueChanged<String>? onPlaybackModeChanged;
  final VoidCallback onUnpair;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Settings')),
      body: SafeArea(
        child: SettingsBody(
          deviceName: deviceName,
          settings: settings,
          onNotificationsChanged: onNotificationsChanged,
          onClipboardModeChanged: onClipboardModeChanged,
          onClipboardAllowSensitiveChanged: onClipboardAllowSensitiveChanged,
          onNotifModeChanged: onNotifModeChanged,
          onMutedToggled: onMutedToggled,
          onAllowedToggled: onAllowedToggled,
          onPlaybackModeChanged: onPlaybackModeChanged,
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
    this.onNotifModeChanged,
    this.onMutedToggled,
    this.onAllowedToggled,
    this.onPlaybackModeChanged,
    required this.onUnpair,
    super.key,
  });

  final String deviceName;
  final AppSettings? settings;
  final ValueChanged<bool> onNotificationsChanged;
  final ValueChanged<String>? onClipboardModeChanged;
  final ValueChanged<bool>? onClipboardAllowSensitiveChanged;
  final ValueChanged<String>? onNotifModeChanged;
  final ValueChanged<String>? onMutedToggled;
  final ValueChanged<String>? onAllowedToggled;
  final ValueChanged<String>? onPlaybackModeChanged;
  final VoidCallback onUnpair;

  @override
  Widget build(BuildContext context) {
    return SettingsBody(
      deviceName: deviceName,
      settings: settings,
      onNotificationsChanged: onNotificationsChanged,
      onClipboardModeChanged: onClipboardModeChanged,
      onClipboardAllowSensitiveChanged: onClipboardAllowSensitiveChanged,
      onNotifModeChanged: onNotifModeChanged,
      onMutedToggled: onMutedToggled,
      onAllowedToggled: onAllowedToggled,
      onPlaybackModeChanged: onPlaybackModeChanged,
      onUnpair: onUnpair,
    );
  }
}

/// Shared settings body: master switch + per-app filter + clipboard + unpair.
class SettingsBody extends StatelessWidget {
  const SettingsBody({
    required this.deviceName,
    required this.settings,
    required this.onNotificationsChanged,
    this.onClipboardModeChanged,
    this.onClipboardAllowSensitiveChanged,
    this.onNotifModeChanged,
    this.onMutedToggled,
    this.onAllowedToggled,
    this.onPlaybackModeChanged,
    required this.onUnpair,
    super.key,
  });

  final String deviceName;
  final AppSettings? settings;
  final ValueChanged<bool> onNotificationsChanged;
  final ValueChanged<String>? onClipboardModeChanged;
  final ValueChanged<bool>? onClipboardAllowSensitiveChanged;
  final ValueChanged<String>? onNotifModeChanged;
  final ValueChanged<String>? onMutedToggled;
  final ValueChanged<String>? onAllowedToggled;
  final ValueChanged<String>? onPlaybackModeChanged;
  final VoidCallback onUnpair;

  @override
  Widget build(BuildContext context) {
    final st = settings;
    final notifEnabled = st?.notificationsEnabled ?? true;
    final clipMode = st?.clipboardMode ?? AppSettings.both;
    final allowSensitive = st?.clipboardAllowSensitive ?? false;
    final playbackMode = st?.playbackMode ?? AppSettings.playbackDefault;
    final scheme = Theme.of(context).colorScheme;
    return CustomScrollView(
      slivers: [
        SliverPadding(
          padding: const EdgeInsets.all(16),
          sliver: SliverList.list(
            children: [
              Card(
                child: SwitchListTile(
                  title: Text(
                    'Phone notifications',
                    style: Theme.of(context).textTheme.titleSmall,
                  ),
                  subtitle: Text(
                    'Mirror to Mac',
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: scheme.onSurfaceVariant,
                        ),
                  ),
                  value: notifEnabled,
                  onChanged: onNotificationsChanged,
                ),
              ),
              if (notifEnabled && st != null) ...[
                const SizedBox(height: 8),
                NotifFilterSection(
                  settings: st,
                  onNotifModeChanged: onNotifModeChanged,
                  onMutedToggled: onMutedToggled,
                  onAllowedToggled: onAllowedToggled,
                ),
              ],
              const SizedBox(height: 8),
              Card(
                child: Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                  child: DropdownButtonFormField<String>(
                    initialValue: clipMode,
                    decoration: const InputDecoration(labelText: 'Clipboard auto sync', border: InputBorder.none),
                    items: const [
                      DropdownMenuItem(value: 'both', child: Text('Both ways')),
                      DropdownMenuItem(value: 'android_to_mac', child: Text('Phone → Mac only')),
                      DropdownMenuItem(value: 'mac_to_android', child: Text('Mac → Phone only')),
                      DropdownMenuItem(value: 'disabled', child: Text('Disabled')),
                    ],
                    onChanged: onClipboardModeChanged == null ? null : (v) { if (v != null) onClipboardModeChanged!(v); },
                  ),
                ),
              ),
              Card(
                child: SwitchListTile(
                  title: Text(
                    'Auto-sync passwords and codes',
                    style: Theme.of(context).textTheme.titleSmall,
                  ),
                  subtitle: Text(
                    'Off skips sensitive clips in auto sync. Manual Send always works.',
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: scheme.onSurfaceVariant,
                        ),
                  ),
                  value: allowSensitive,
                  onChanged: onClipboardAllowSensitiveChanged,
                ),
              ),
              const Padding(
                padding: EdgeInsets.symmetric(horizontal: 4, vertical: 4),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Padding(padding: EdgeInsets.only(top: 2, right: 6), child: Icon(Icons.info_outline, size: 14)),
                    Expanded(
                      child: Text(
                        'Copy on one device, paste on the other. Large images send in chunks. Conflicts keep local and log to the feed.',
                        style: TextStyle(fontSize: 11),
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: 8),
              Card(
                child: Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                  child: DropdownButtonFormField<String>(
                    initialValue: playbackMode,
                    decoration: const InputDecoration(labelText: 'Playback sync', border: InputBorder.none),
                    items: const [
                      DropdownMenuItem(value: 'both', child: Text('Both ways')),
                      DropdownMenuItem(value: 'android_to_mac', child: Text('Phone to Mac only')),
                      DropdownMenuItem(value: 'mac_to_android', child: Text('Mac to Phone only')),
                      DropdownMenuItem(value: 'disabled', child: Text('Off')),
                    ],
                    onChanged: onPlaybackModeChanged == null ? null : (v) { if (v != null) onPlaybackModeChanged!(v); },
                  ),
                ),
              ),
              const Padding(
                padding: EdgeInsets.symmetric(horizontal: 4, vertical: 4),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Padding(padding: EdgeInsets.only(top: 2, right: 6), child: Icon(Icons.info_outline, size: 14)),
                    Expanded(
                      child: Text(
                        'Phone is the music source. Phone to Mac shows it on the Mac. Mac to Phone lets the Mac control playback.',
                        style: TextStyle(fontSize: 11),
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: 12),
              Card(
                child: ListTile(
                  leading: Icon(Icons.link_off, color: Colors.red),
                  title: Text(
                    'Unpair Mac',
                    style: Theme.of(context).textTheme.titleSmall?.copyWith(
                          color: Colors.red,
                        ),
                  ),
                  subtitle: Text(deviceName),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: onUnpair,
                ),
              ),
            ],
          ),
        ),
      ],
    );
  }
}

/// Per-app notification filter: mode switch + searchable toggle list.
/// Loads launchable apps once; muted/allowed sets keep unlisted packages
/// toggleable. Errors render inline via SelectableText.rich, never SnackBar.
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
    // Muted/allowed packages stay toggleable even with zero live rows.
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
        // Active-list apps first (muted/allowed state), then alpha.
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
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    onlyAllowed ? 'Allowed apps ($count)' : count > 0 ? 'Muted apps ($count)' : 'All apps mirror',
                    style: Theme.of(context).textTheme.titleSmall,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 4),
            const Text(
              'Download progress never mirrors.',
              style: TextStyle(fontSize: 11),
            ),
            const SizedBox(height: 8),
            SegmentedButton<String>(
              segments: const [
                ButtonSegment(value: AppSettings.notifAllExceptMuted, label: Text('All except muted'), icon: Icon(Icons.notifications_active, size: 16)),
                ButtonSegment(value: AppSettings.notifOnlyAllowed, label: Text('Only allowed'), icon: Icon(Icons.notifications_paused, size: 16)),
              ],
              selected: {st.notifMode},
              onSelectionChanged: widget.onNotifModeChanged == null
                  ? null
                  : (s) {
                      if (s.isNotEmpty) widget.onNotifModeChanged!(s.first);
                    },
            ),
            const SizedBox(height: 8),
            TextField(
              controller: _query,
              decoration: const InputDecoration(
                labelText: 'Search apps',
                prefixIcon: Icon(Icons.search, size: 18),
                border: OutlineInputBorder(),
                isDense: true,
              ),
              onChanged: (_) => setState(() {}),
            ),
            const SizedBox(height: 4),
            if (_loading)
              const Padding(
                padding: EdgeInsets.symmetric(vertical: 12),
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
    if (apps.isEmpty) {
      return [
        Padding(
          padding: const EdgeInsets.symmetric(vertical: 8),
          child: Text(
            _query.text.trim().isEmpty
                ? 'Apps appear here once one is installed. Saved toggles still apply.'
                : 'No apps match this search.',
            style: Theme.of(context).textTheme.bodySmall,
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
            title: Text(a['app'] ?? pkg, style: Theme.of(context).textTheme.bodyMedium),
            subtitle: Text(pkg, style: Theme.of(context).textTheme.bodySmall),
            value: on,
            onChanged: (_) {
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
