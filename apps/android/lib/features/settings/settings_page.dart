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
    required this.onUnpair,
    super.key,
  });

  final String deviceName;
  final dynamic settings;
  final ValueChanged<bool> onNotificationsChanged;
  final VoidCallback onUnpair;

  @override
  Widget build(BuildContext context) {
    final notifEnabled = (settings?.notificationsEnabled as bool?) ?? true;
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
    required this.onUnpair,
    super.key,
  });

  final String deviceName;
  final dynamic settings;
  final ValueChanged<bool> onNotificationsChanged;
  final VoidCallback onUnpair;

  @override
  Widget build(BuildContext context) {
    final notifEnabled = (settings?.notificationsEnabled as bool?) ?? true;
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
