// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Reading this as: paired devices list for Devices tab, following HIG 5/6/9/12.

import 'package:flutter/material.dart';

import '../../widgets/haptics.dart';

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
    final isDark = scheme.brightness == Brightness.dark;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Icon(Icons.devices_outlined, size: 18, color: scheme.onSurface),
            const SizedBox(width: 8),
            Text(
              'Paired Devices',
              style: Theme.of(context).textTheme.titleSmall?.copyWith(
                    fontWeight: FontWeight.w700,
                    letterSpacing: -0.2,
                  ),
            ),
            const Spacer(),
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 3),
              decoration: BoxDecoration(
                color: scheme.surfaceContainerHighest,
                borderRadius: BorderRadius.circular(999),
              ),
              child: Text(
                '1',
                style: Theme.of(context).textTheme.labelSmall?.copyWith(
                      color: scheme.onSurface,
                      fontWeight: FontWeight.w700,
                    ),
              ),
            ),
          ],
        ),
        const SizedBox(height: 10),
        Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Row(
              children: [
                Container(
                  width: 44,
                  height: 44,
                  decoration: BoxDecoration(
                    color: online ? scheme.secondaryContainer : scheme.surfaceContainerHighest,
                    borderRadius: BorderRadius.circular(14),
                  ),
                  child: Center(
                    child: Icon(
                      online ? Icons.laptop_mac_rounded : Icons.laptop_mac_outlined,
                      color: online ? scheme.onSecondaryContainer : scheme.onSurfaceVariant,
                      size: 22,
                    ),
                  ),
                ),
                const SizedBox(width: 14),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        deviceName,
                        style: Theme.of(context).textTheme.titleSmall?.copyWith(
                              fontWeight: FontWeight.w700,
                              letterSpacing: -0.2,
                            ),
                      ),
                      const SizedBox(height: 6),
                      Row(
                        children: [
                          Container(
                            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                            decoration: BoxDecoration(
                              color: scheme.surfaceContainerHighest,
                              borderRadius: BorderRadius.circular(8),
                            ),
                            child: Text(
                              'Mac',
                              style: Theme.of(context).textTheme.labelSmall?.copyWith(
                                    color: scheme.onSurfaceVariant,
                                    fontWeight: FontWeight.w600,
                                  ),
                            ),
                          ),
                          const SizedBox(width: 6),
                          Container(
                            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                            decoration: BoxDecoration(
                              color: online ? scheme.secondaryContainer : scheme.surfaceContainerHighest,
                              borderRadius: BorderRadius.circular(8),
                            ),
                            child: Row(
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                Container(
                                  width: 6,
                                  height: 6,
                                  decoration: BoxDecoration(
                                    shape: BoxShape.circle,
                                    color: online
                                        ? (isDark ? const Color(0xFF30D158) : const Color(0xFF248A3D))
                                        : scheme.outline,
                                  ),
                                ),
                                const SizedBox(width: 5),
                                Text(
                                  online ? 'Connected' : 'Offline',
                                  style: Theme.of(context).textTheme.labelSmall?.copyWith(
                                        color: online ? scheme.onSecondaryContainer : scheme.onSurfaceVariant,
                                        fontWeight: FontWeight.w600,
                                      ),
                                ),
                              ],
                            ),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
                PopupMenuButton<String>(
                  tooltip: 'Device options',
                  icon: Icon(Icons.more_vert_rounded, color: scheme.onSurfaceVariant),
                  shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(14)),
                  elevation: 2,
                  onSelected: (v) {
                    if (v == 'unpair') {
                      AppHaptics.error();
                      onUnpair();
                    }
                  },
                  itemBuilder: (context) => [
                    PopupMenuItem(
                      value: 'unpair',
                      child: Row(
                        children: [
                          Icon(Icons.link_off_rounded, color: scheme.error, size: 18),
                          const SizedBox(width: 8),
                          Text(
                            'Unpair…',
                            style: TextStyle(color: scheme.error, fontWeight: FontWeight.w600),
                          ),
                        ],
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
