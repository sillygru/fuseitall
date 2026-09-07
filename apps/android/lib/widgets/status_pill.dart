// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter/material.dart';

// HIG 5/9: status communicated by label + icon, never color alone.
// Container fill uses only semantic ColorScheme roles.
class StatusPill extends StatelessWidget {
  const StatusPill({required this.enabled, super.key});

  final bool enabled;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final bg = enabled ? scheme.secondaryContainer : scheme.surfaceContainerHighest;
    final fg = enabled ? scheme.onSecondaryContainer : scheme.onSurfaceVariant;
    final icon = enabled ? Icons.check_circle : Icons.cancel_outlined;
    final label = enabled ? 'Enabled' : 'Disabled';
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: bg,
        borderRadius: BorderRadius.circular(999),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 14, color: fg),
          const SizedBox(width: 4),
          Text(
            label,
            style: Theme.of(context).textTheme.labelSmall?.copyWith(
                  color: fg,
                  fontWeight: FontWeight.w600,
                ),
          ),
        ],
      ),
    );
  }
}

/// Connected/Offline pill for the hero & device list. Uses
/// secondaryContainer for online, surfaceContainerHighest for offline,
/// plus a dot+icon cue so color is not the only signal.
class ConnectedPill extends StatelessWidget {
  const ConnectedPill({
    required this.connected,
    required this.deviceName,
    super.key,
  });

  final bool connected;
  final String deviceName;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final bg = connected ? scheme.secondaryContainer : scheme.surfaceContainerHighest;
    final fg = connected ? scheme.onSecondaryContainer : scheme.onSurfaceVariant;
    final dot = connected ? scheme.primary : scheme.outline;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
      decoration: BoxDecoration(
        color: bg,
        borderRadius: BorderRadius.circular(12),
      ),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Container(
            width: 8,
            height: 8,
            decoration: BoxDecoration(shape: BoxShape.circle, color: dot),
          ),
          const SizedBox(width: 8),
          Icon(
            connected ? Icons.link : Icons.link_off,
            size: 14,
            color: fg,
          ),
          const SizedBox(width: 6),
          Flexible(
            child: Text(
              connected ? 'Connected to $deviceName' : 'Offline — $deviceName',
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.labelMedium?.copyWith(
                    color: fg,
                    fontWeight: FontWeight.w700,
                  ),
            ),
          ),
        ],
      ),
    );
  }
}
