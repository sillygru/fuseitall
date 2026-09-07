// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter/material.dart';

import '../permissions/permissions.dart';

/// Permissions home: one row per capability with live status + fix CTA.
/// This is the place the app asks for permissions — each Disabled row
/// opens the exact system screen.
class EssentialServicesCard extends StatelessWidget {
  const EssentialServicesCard({
    required this.status,
    required this.permissions,
    super.key,
  });

  final PermissionStatus? status;
  final Permissions permissions;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Wrap(
              alignment: WrapAlignment.center,
              crossAxisAlignment: WrapCrossAlignment.center,
              spacing: 6,
              children: [
                Icon(Icons.settings_suggest_outlined, size: 20),
                Text(
                  'Essential Services',
                  style: TextStyle(fontWeight: FontWeight.bold),
                ),
              ],
            ),
            const SizedBox(height: 8),
            _row(
              context,
              icon: Icons.notifications_outlined,
              title: 'Notification access',
              body: status == null
                  ? 'Checking…'
                  : status!.listenerEnabled
                  ? 'Enabled — phone notifications mirror to Mac.'
                  : 'Disabled — mirroring is paused until you enable it.',
              enabled: status?.listenerEnabled ?? false,
              onFix: permissions.openListenerSettings,
            ),
            const Divider(),
            _row(
              context,
              icon: Icons.qr_code_2_outlined,
              title: 'Camera (pairing only)',
              body: 'Used once to scan the Mac code. Nothing is recorded.',
              enabled: true,
              onFix: null,
            ),
            if (status != null && !status!.listenerEnabled) ...[
              const SizedBox(height: 4),
              Text(
                'Tip: enable notification access, then tap Reconnect.',
                style: TextStyle(color: scheme.onSurfaceVariant),
              ),
            ],
          ],
        ),
      ),
    );
  }

  Widget _row(
    BuildContext context, {
    required IconData icon,
    required String title,
    required String body,
    required bool enabled,
    required VoidCallback? onFix,
  }) {
    final scheme = Theme.of(context).colorScheme;
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, color: scheme.onSurfaceVariant),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  title,
                  style: const TextStyle(fontWeight: FontWeight.w600),
                ),
                const SizedBox(height: 4),
                Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 8,
                    vertical: 2,
                  ),
                  decoration: BoxDecoration(
                    color: enabled
                        ? Colors.green.withValues(alpha: 0.18)
                        : scheme.surfaceContainerHighest,
                    borderRadius: BorderRadius.circular(999),
                  ),
                  child: Text(
                    enabled ? 'Enabled' : 'Disabled',
                    style: TextStyle(
                      color: enabled ? Colors.green : scheme.onSurfaceVariant,
                      fontWeight: FontWeight.w600,
                      fontSize: 12,
                    ),
                  ),
                ),
                const SizedBox(height: 4),
                SelectableText(body),
                if (onFix != null && !enabled) ...[
                  const SizedBox(height: 6),
                  FilledButton.tonal(
                    onPressed: onFix,
                    child: const Text('Enable'),
                  ),
                ],
              ],
            ),
          ),
        ],
      ),
    );
  }
}
