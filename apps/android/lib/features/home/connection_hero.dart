// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Reading this as: paired home hero for connection status, following HIG 5/6/7/9/14.

import 'package:flutter/material.dart';

import '../../widgets/status_pill.dart';

class ConnectionHero extends StatelessWidget {
  const ConnectionHero({
    required this.deviceName,
    required this.connected,
    required this.subtitle,
    required this.onDisconnect,
    required this.onReconnect,
    required this.sending,
    this.onSendClipboard,
    this.clipSending = false,
    super.key,
  });

  final String deviceName;
  final bool connected;
  final String subtitle;
  final VoidCallback onDisconnect;
  final VoidCallback onReconnect;
  final bool sending;
  final VoidCallback? onSendClipboard;
  final bool clipSending;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
      decoration: BoxDecoration(
        color: scheme.surfaceContainer,
        borderRadius: BorderRadius.circular(28),
        border: Border.all(color: scheme.outlineVariant),
      ),
      padding: const EdgeInsets.fromLTRB(18, 18, 18, 16),
      child: Column(
        children: [
          Container(
            width: 56,
            height: 56,
            decoration: BoxDecoration(
              color: scheme.primaryContainer,
              shape: BoxShape.circle,
            ),
            child: Icon(
              Icons.link,
              size: 28,
              color: scheme.onPrimaryContainer,
            ),
          ),
          const SizedBox(height: 10),
          Text(
            'FuseItAll',
            style: Theme.of(context).textTheme.titleMedium?.copyWith(
                  color: scheme.onSurface,
                  fontWeight: FontWeight.w800,
                  letterSpacing: 0.6,
                ),
          ),
          const SizedBox(height: 2),
          Text(
            'Android ↔ Mac',
            style: Theme.of(context).textTheme.labelSmall?.copyWith(
                  color: scheme.onSurfaceVariant,
                  fontWeight: FontWeight.w500,
                  letterSpacing: 0.8,
                ),
          ),
          const SizedBox(height: 14),
          Row(
            children: [
              Expanded(
                child: ConnectedPill(connected: connected, deviceName: deviceName),
              ),
              const SizedBox(width: 10),
              Semantics(
                label: 'Disconnect',
                button: true,
                child: Material(
                  color: scheme.surfaceContainerHighest,
                  shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
                  child: InkWell(
                    borderRadius: BorderRadius.circular(12),
                    onTap: onDisconnect,
                    child: Padding(
                      padding: const EdgeInsets.all(12),
                      child: Icon(
                        Icons.power_settings_new,
                        size: 20,
                        color: scheme.onSurfaceVariant,
                        semanticLabel: 'Disconnect',
                      ),
                    ),
                  ),
                ),
              ),
            ],
          ),
          if (subtitle.isNotEmpty) ...[
            const SizedBox(height: 8),
            SelectableText(
              subtitle,
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
            ),
          ],
          if (!connected) ...[
            const SizedBox(height: 12),
            SizedBox(
              width: double.infinity,
              child: FilledButton.icon(
                onPressed: sending ? null : onReconnect,
                icon: sending
                    ? SizedBox(
                        width: 16,
                        height: 16,
                        child: CircularProgressIndicator(
                          strokeWidth: 2,
                          color: scheme.onPrimary,
                        ),
                      )
                    : const Icon(Icons.refresh, size: 18),
                label: Text(sending ? 'Connecting…' : 'Reconnect'),
              ),
            ),
          ],
          if (onSendClipboard != null) ...[
            const SizedBox(height: 12),
            Row(
              children: [
                Expanded(
                  child: _tile(
                    context,
                    icon: Icons.content_paste_go_rounded,
                    label: 'Send Clipboard',
                    onTap: clipSending ? null : onSendClipboard,
                    busy: clipSending,
                  ),
                ),
              ],
            ),
          ],
        ],
      ),
    );
  }

  Widget _tile(
    BuildContext context, {
    required IconData icon,
    required String label,
    VoidCallback? onTap,
    bool busy = false,
  }) {
    final scheme = Theme.of(context).colorScheme;
    return Material(
      color: scheme.secondaryContainer,
      borderRadius: BorderRadius.circular(16),
      child: InkWell(
        borderRadius: BorderRadius.circular(16),
        onTap: onTap,
        child: Container(
          decoration: BoxDecoration(borderRadius: BorderRadius.circular(16)),
          padding: const EdgeInsets.symmetric(vertical: 16, horizontal: 12),
          child: Row(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              if (busy)
                SizedBox(
                  width: 20,
                  height: 20,
                  child: CircularProgressIndicator(
                    strokeWidth: 2,
                    color: scheme.onSecondaryContainer,
                  ),
                )
              else
                Container(
                  width: 32,
                  height: 32,
                  decoration: BoxDecoration(
                    color: scheme.primary,
                    borderRadius: BorderRadius.circular(10),
                  ),
                  child: Icon(icon, color: scheme.onPrimary, size: 18),
                ),
              const SizedBox(width: 12),
              Text(
                label,
                style: Theme.of(context).textTheme.labelLarge?.copyWith(
                      color: scheme.onSecondaryContainer,
                      fontWeight: FontWeight.w700,
                    ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
