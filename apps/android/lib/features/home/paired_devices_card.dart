// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Reading this as: paired devices list for Devices tab, following HIG 5/6/9/12.

import 'package:flutter/material.dart';

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
            Text(
              'Paired Devices',
              style: Theme.of(context).textTheme.titleSmall?.copyWith(
                    fontWeight: FontWeight.bold,
                  ),
            ),
            const Spacer(),
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 2),
              decoration: BoxDecoration(
                color: scheme.primaryContainer,
                borderRadius: BorderRadius.circular(999),
              ),
              child: Text(
                '1',
                style: Theme.of(context).textTheme.labelMedium?.copyWith(
                      color: scheme.onPrimaryContainer,
                      fontWeight: FontWeight.bold,
                    ),
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
                  backgroundColor: online ? scheme.secondaryContainer : scheme.surfaceContainerHighest,
                  child: Icon(
                    online ? Icons.check_circle : Icons.smartphone_outlined,
                    color: online ? scheme.onSecondaryContainer : scheme.onSurfaceVariant,
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        deviceName,
                        style: Theme.of(context).textTheme.titleSmall?.copyWith(
                              fontWeight: FontWeight.bold,
                            ),
                      ),
                      const SizedBox(height: 4),
                      Wrap(
                        spacing: 6,
                        children: [
                          Chip(
                            label: const Text('Mac'),
                            visualDensity: VisualDensity.compact,
                            backgroundColor: scheme.surfaceContainerHighest,
                          ),
                          Chip(
                            label: Text(online ? 'Connected' : 'Offline'),
                            visualDensity: VisualDensity.compact,
                            backgroundColor: online ? scheme.secondaryContainer : null,
                            labelStyle: TextStyle(
                              color: online ? scheme.onSecondaryContainer : null,
                            ),
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
                  itemBuilder: (context) => [
                    PopupMenuItem(
                      value: 'unpair',
                      child: Text(
                        'Unpair…',
                        style: TextStyle(color: scheme.error),
                      ),
                    ),
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
