// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter/material.dart';

import 'app_settings.dart';

/// Settings screen reached from the paired view's gear icon.
/// Clipboard mode + notifications + unpair. Owns no persistence —
///
/// the caller (PingPage) owns [settings] and applies changes via
/// the callbacks so heartbeat sync stays single-sourced.
class SettingsPage extends StatelessWidget {
  const SettingsPage({
    required this.deviceName,
    required this.settings,
    required this.onModeChanged,
    required this.onNotificationsChanged,
    required this.onUnpair,
    super.key,
  });

  final String deviceName;
  final AppSettings? settings;
  final ValueChanged<String> onModeChanged;
  final ValueChanged<bool> onNotificationsChanged;
  final VoidCallback onUnpair;

  @override
  Widget build(BuildContext context) {
    final s = settings;
    final mode = s?.clipboardMode ?? AppSettings.twoWay;
    final notifEnabled = s?.notificationsEnabled ?? true;
    return Scaffold(
      appBar: AppBar(title: const Text('Settings')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          Card(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Row(
                    children: [
                      Icon(Icons.content_paste, size: 18),
                      SizedBox(width: 8),
                      Text('Clipboard', style: TextStyle(fontWeight: FontWeight.bold)),
                    ],
                  ),
                  const SizedBox(height: 4),
                  Text('Mode: ${clipboardModeLabel(mode)}',
                      style: Theme.of(context).textTheme.labelSmall),
                  const SizedBox(height: 8),
                  RadioGroup<String>(
                    groupValue: mode,
                    onChanged: (v) {
                      if (v != null) onModeChanged(v);
                    },
                    child: Column(
                      children: [
                        for (final m in AppSettings.validModes)
                          RadioListTile<String>(
                            title: Text(clipboardModeLabel(m)),
                            value: m,
                            dense: true,
                            contentPadding: EdgeInsets.zero,
                          ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          ),
          const SizedBox(height: 12),
          Card(
            child: SwitchListTile(
              title: const Text('Phone notifications'),
              subtitle: const Text('Mirror to Mac'),
              value: notifEnabled,
              onChanged: onNotificationsChanged,
            ),
          ),
          const SizedBox(height: 12),
          Card(
            child: ListTile(
              leading: const Icon(Icons.link_off, color: Colors.redAccent),
              title: const Text('Unpair Mac'),
              subtitle: Text(deviceName),
              trailing: const Icon(Icons.chevron_right),
              onTap: onUnpair,
            ),
          ),
        ],
      ),
    );
  }
}
