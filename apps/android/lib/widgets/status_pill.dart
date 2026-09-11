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
    final isDark = scheme.brightness == Brightness.dark;
    final bg = enabled ? scheme.secondaryContainer : scheme.surfaceContainerHighest;
    final fg = enabled ? (isDark ? const Color(0xFF85E89D) : const Color(0xFF1B6B2F)) : scheme.onSurfaceVariant;
    final icon = enabled ? Icons.check_circle_rounded : Icons.cancel_outlined;
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
          Icon(icon, size: 13, color: fg),
          const SizedBox(width: 5),
          Text(
            label,
            style: Theme.of(context).textTheme.labelSmall?.copyWith(
                  color: fg,
                  fontWeight: FontWeight.w700,
                  letterSpacing: -0.1,
                ),
          ),
        ],
      ),
    );
  }
}

/// Connected/Offline pill for the hero & device list. Uses
/// secondaryContainer for online, surfaceContainerHighest for offline,
/// plus a jewel dot+icon cue so color is not the only signal.
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
    final isDark = scheme.brightness == Brightness.dark;
    final okColor = isDark ? const Color(0xFF30D158) : const Color(0xFF248A3D);
    final bg = connected ? scheme.secondaryContainer : scheme.surfaceContainerHighest;
    final fg = connected ? scheme.onSecondaryContainer : scheme.onSurfaceVariant;
    final dotColor = connected ? okColor : scheme.outline;

    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
      decoration: BoxDecoration(
        color: bg,
        borderRadius: BorderRadius.circular(16),
      ),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Container(
            width: 8,
            height: 8,
            decoration: BoxDecoration(
              shape: BoxShape.circle,
              color: dotColor,
              boxShadow: connected
                  ? [
                      BoxShadow(
                        color: dotColor.withValues(alpha: 0.4),
                        blurRadius: 6,
                        spreadRadius: 1,
                      ),
                    ]
                  : null,
            ),
          ),
          const SizedBox(width: 8),
          Icon(
            connected ? Icons.link : Icons.link_off,
            size: 15,
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
                    letterSpacing: -0.2,
                  ),
            ),
          ),
        ],
      ),
    );
  }
}
