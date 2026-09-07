// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter/material.dart';

/// Paired Devices section: the one remembered Mac with live status.
class PairedDevicesCard extends StatelessWidget {
  const PairedDevicesCard({
    required this.deviceName,
    required this.online,
    required this.onUnpair,
    super.key,
  });

  final String deviceName;
  final bool online;
  final VoidCallback onUnpair;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            const Icon(Icons.devices_outlined, size: 18),
            const SizedBox(width: 6),
            const Text(
              'Paired Devices',
              style: TextStyle(fontWeight: FontWeight.bold),
            ),
            const Spacer(),
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 2),
              decoration: BoxDecoration(
                color: scheme.primaryContainer,
                borderRadius: BorderRadius.circular(999),
              ),
              child: const Text(
                '1',
                style: TextStyle(fontWeight: FontWeight.bold),
              ),
            ),
          ],
        ),
        const SizedBox(height: 8),
        Card(
          child: Padding(
            padding: const EdgeInsets.all(12),
            child: Row(
              children: [
                CircleAvatar(
                  backgroundColor: online
                      ? Colors.green.withValues(alpha: 0.2)
                      : scheme.surfaceContainerHighest,
                  child: Icon(
                    online ? Icons.check_circle : Icons.smartphone_outlined,
                    color: online ? Colors.green : scheme.onSurfaceVariant,
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        deviceName,
                        style: const TextStyle(fontWeight: FontWeight.bold),
                      ),
                      const SizedBox(height: 4),
                      Wrap(
                        spacing: 6,
                        children: [
                          const Chip(
                            label: Text('Mac'),
                            visualDensity: VisualDensity.compact,
                          ),
                          Chip(
                            label: Text(online ? 'Connected' : 'Offline'),
                            visualDensity: VisualDensity.compact,
                            backgroundColor: online
                                ? Colors.green.withValues(alpha: 0.2)
                                : null,
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
                PopupMenuButton<String>(
                  tooltip: 'Device options',
                  icon: const Icon(Icons.more_vert),
                  onSelected: (v) {
                    if (v == 'unpair') onUnpair();
                  },
                  itemBuilder: (context) => const [
                    PopupMenuItem(value: 'unpair', child: Text('Unpair…')),
                  ],
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }
}
