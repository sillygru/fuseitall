// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter/material.dart';

/// Settings screen reached from the paired view's gear icon.
/// Owns no persistence — the caller (PingPage) owns [settings] and applies
/// changes via the callbacks so heartbeat sync stays single-sourced.
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
    return Scaffold(
      appBar: AppBar(title: const Text('Settings')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
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
