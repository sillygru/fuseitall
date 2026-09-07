// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Reading this as: settings content for Settings tab / sheet, following HIG 4/6/9.

import 'package:flutter/material.dart';

class SettingsPage extends StatelessWidget {
  const SettingsPage({
    required this.deviceName,
    required this.settings,
    required this.onNotificationsChanged,
    this.onClipboardModeChanged,
    required this.onUnpair,
    super.key,
  });

  final String deviceName;
  final dynamic settings;
  final ValueChanged<bool> onNotificationsChanged;
  final ValueChanged<String>? onClipboardModeChanged;
  final VoidCallback onUnpair;

  @override
  Widget build(BuildContext context) {
    final notifEnabled = (settings?.notificationsEnabled as bool?) ?? true;
    final clipMode = (settings?.clipboardMode as String?) ?? 'both';
    final scheme = Theme.of(context).colorScheme;
    return Scaffold(
      appBar: AppBar(title: const Text('Settings')),
      body: SafeArea(
        child: CustomScrollView(
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
                  const SizedBox(height: 12),
                  Card(
                    child: Padding(
                      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                      child: DropdownButtonFormField<String>(
                        initialValue: clipMode,
                        decoration: const InputDecoration(labelText: 'Clipboard auto sync', border: InputBorder.none),
                        // ignore: deprecated_member_use
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
                  const Padding(
                    padding: EdgeInsets.symmetric(horizontal: 4, vertical: 4),
                    child: Row(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Padding(padding: EdgeInsets.only(top: 2, right: 6), child: Icon(Icons.info_outline, size: 14)),
                        Expanded(
                          child: Text(
                            'Mac \u2192 phone auto sync works. Phone \u2192 Mac auto sync is not yet available \u2014 use Send manually.',
                            style: TextStyle(fontSize: 11),
                          ),
                        ),
                      ],
                    ),
                  ),
                  const SizedBox(height: 12),
                  Card(
                    child: ListTile(
                      leading: Icon(Icons.link_off, color: scheme.error),
                      title: Text(
                        'Unpair Mac',
                        style: Theme.of(context).textTheme.titleSmall?.copyWith(
                              color: scheme.error,
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
    required this.onUnpair,
    super.key,
  });

  final String deviceName;
  final dynamic settings;
  final ValueChanged<bool> onNotificationsChanged;
  final ValueChanged<String>? onClipboardModeChanged;
  final VoidCallback onUnpair;

  @override
  Widget build(BuildContext context) {
    final notifEnabled = (settings?.notificationsEnabled as bool?) ?? true;
    final clipMode = (settings?.clipboardMode as String?) ?? 'both';
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
              const SizedBox(height: 8),
              Card(
                child: Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                  child: DropdownButtonFormField<String>(
                    initialValue: clipMode,
                    decoration: const InputDecoration(labelText: 'Clipboard auto sync', border: InputBorder.none),
                    // ignore: deprecated_member_use
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
              const Padding(
                padding: EdgeInsets.symmetric(horizontal: 4, vertical: 4),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Padding(padding: EdgeInsets.only(top: 2, right: 6), child: Icon(Icons.info_outline, size: 14)),
                    Expanded(
                      child: Text(
                        'Mac → phone auto sync works. Phone → Mac auto sync is not yet available — use Send manually.',
                        style: TextStyle(fontSize: 11),
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: 12),
              Card(
                child: ListTile(
                  leading: Icon(Icons.link_off, color: scheme.error),
                  title: Text(
                    'Unpair Mac',
                    style: Theme.of(context).textTheme.titleSmall?.copyWith(
                          color: scheme.error,
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
