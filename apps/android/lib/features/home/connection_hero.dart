// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Reading this as: paired home hero for connection status, following HIG 5/6/7/9/14.

import 'package:flutter/material.dart';

import '../../widgets/app_icon.dart';
import '../../widgets/haptics.dart';
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
    final isDark = scheme.brightness == Brightness.dark;

    return Container(
      decoration: BoxDecoration(
        color: scheme.surfaceContainer,
        borderRadius: BorderRadius.circular(28),
        boxShadow: isDark
            ? null
            : [
                BoxShadow(
                  color: Colors.black.withValues(alpha: 0.03),
                  blurRadius: 18,
                  offset: const Offset(0, 4),
                ),
              ],
      ),
      padding: const EdgeInsets.fromLTRB(20, 22, 20, 18),
      child: Column(
        children: [
          _BeaconIcon(connected: connected),
          const SizedBox(height: 12),
          Text(
            'FuseItAll',
            style: Theme.of(context).textTheme.titleMedium?.copyWith(
                  color: scheme.onSurface,
                  fontWeight: FontWeight.w800,
                  letterSpacing: -0.2,
                ),
          ),
          const SizedBox(height: 2),
          Text(
            'Android ↔ Mac',
            style: Theme.of(context).textTheme.labelSmall?.copyWith(
                  color: scheme.onSurfaceVariant,
                  fontWeight: FontWeight.w600,
                  letterSpacing: 0.5,
                ),
          ),
          const SizedBox(height: 16),
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
                  shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(14)),
                  child: InkWell(
                    borderRadius: BorderRadius.circular(14),
                    onTap: () {
                      AppHaptics.light();
                      onDisconnect();
                    },
                    child: Padding(
                      padding: const EdgeInsets.all(12),
                      child: Icon(
                        Icons.power_settings_new_rounded,
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
            const SizedBox(height: 10),
            SelectableText(
              subtitle,
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                    letterSpacing: -0.1,
                  ),
            ),
          ],
          if (!connected) ...[
            const SizedBox(height: 14),
            SizedBox(
              width: double.infinity,
              child: FilledButton.icon(
                onPressed: sending
                    ? null
                    : () {
                        AppHaptics.light();
                        onReconnect();
                      },
                icon: sending
                    ? SizedBox(
                        width: 16,
                        height: 16,
                        child: CircularProgressIndicator(
                          strokeWidth: 2,
                          color: scheme.onPrimary,
                        ),
                      )
                    : const Icon(Icons.refresh_rounded, size: 18),
                label: Text(sending ? 'Connecting…' : 'Reconnect'),
              ),
            ),
          ],
          if (onSendClipboard != null) ...[
            const SizedBox(height: 14),
            Row(
              children: [
                Expanded(
                  child: _tile(
                    context,
                    icon: Icons.content_paste_go_rounded,
                    label: 'Send Clipboard',
                    onTap: clipSending
                        ? null
                        : () {
                            AppHaptics.light();
                            onSendClipboard!();
                          },
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
      borderRadius: BorderRadius.circular(18),
      child: InkWell(
        borderRadius: BorderRadius.circular(18),
        onTap: onTap,
        child: Container(
          decoration: BoxDecoration(borderRadius: BorderRadius.circular(18)),
          padding: const EdgeInsets.symmetric(vertical: 16, horizontal: 14),
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
                  width: 34,
                  height: 34,
                  decoration: BoxDecoration(
                    color: scheme.primary,
                    borderRadius: BorderRadius.circular(11),
                  ),
                  child: Icon(icon, color: scheme.onPrimary, size: 18),
                ),
              const SizedBox(width: 12),
              Text(
                label,
                style: Theme.of(context).textTheme.labelLarge?.copyWith(
                      color: scheme.onSecondaryContainer,
                      fontWeight: FontWeight.w700,
                      letterSpacing: -0.2,
                    ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _BeaconIcon extends StatefulWidget {
  const _BeaconIcon({required this.connected});

  final bool connected;

  @override
  State<_BeaconIcon> createState() => _BeaconIconState();
}

class _BeaconIconState extends State<_BeaconIcon>
    with SingleTickerProviderStateMixin {
  late final AnimationController _anim;

  @override
  void initState() {
    super.initState();
    _anim = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 2200),
    );
    if (widget.connected) {
      _anim.repeat(reverse: true);
    }
  }

  @override
  void didUpdateWidget(covariant _BeaconIcon oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.connected != oldWidget.connected) {
      if (widget.connected) {
        _anim.repeat(reverse: true);
      } else {
        _anim.stop();
        _anim.reset();
      }
    }
  }

  @override
  void dispose() {
    _anim.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final noAnim = MediaQuery.disableAnimationsOf(context);

    if (noAnim || !widget.connected) {
      return const AppIconWell(size: 60);
    }

    return AnimatedBuilder(
      animation: _anim,
      builder: (context, child) {
        final spread = 2.0 + (_anim.value * 6.0);
        final opacity = 0.12 + (_anim.value * 0.16);
        return Container(
          decoration: BoxDecoration(
            shape: BoxShape.circle,
            boxShadow: [
              BoxShadow(
                color: scheme.primary.withValues(alpha: opacity),
                blurRadius: 16,
                spreadRadius: spread,
              ),
            ],
          ),
          child: child,
        );
      },
      child: const AppIconWell(size: 60),
    );
  }
}
