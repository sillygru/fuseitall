// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter/material.dart';

/// Compact hero — own identity, not a clone. Teal/ink palette, rounded
/// link-badge, pill + power, reconnect. Plus optional Send Clipboard tile.
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

  /// Manual clipboard push — reads local clipboard and pushes to Mac.
  final VoidCallback? onSendClipboard;
  final bool clipSending;

  @override
  Widget build(BuildContext context) {
    const bg = Color(0xFF0F1F1D);
    return Container(
      decoration: BoxDecoration(
        color: bg,
        borderRadius: BorderRadius.circular(28),
      ),
      padding: const EdgeInsets.fromLTRB(18, 18, 18, 16),
      child: Column(
        children: [
          // Mac-matched mark: circular navy badge with PlugZap (plug + zap)
          // Diagonal composition like the Mac header — dark circle + blue icon.
          Container(
            width: 56,
            height: 56,
            decoration: const BoxDecoration(
              color: Color(0xFF16243E),
              shape: BoxShape.circle,
            ),
            child: Stack(
              alignment: Alignment.center,
              children: [
                // Plug body
                Transform.rotate(
                  angle: -0.55,
                  child: const Icon(Icons.power_outlined, size: 28, color: Color(0xFF5B8DEF)),
                ),
                // Zap overlay top-right
                Positioned(
                  top: 10,
                  right: 10,
                  child: Transform.rotate(
                    angle: 0.15,
                    child: const Icon(Icons.bolt, size: 18, color: Color(0xFF5B8DEF)),
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(height: 10),
          Text(
            'FuseItAll',
            style: TextStyle(
              color: Color(0xF5FFFFFF),
              fontWeight: FontWeight.w800,
              letterSpacing: 0.6,
              fontSize: 16,
            ),
          ),
          const SizedBox(height: 2),
          Text(
            'Android ↔ Mac',
            style: TextStyle(
              color: Color(0x73FFFFFF),
              fontWeight: FontWeight.w500,
              letterSpacing: 0.8,
              fontSize: 10,
            ),
          ),
          const SizedBox(height: 14),
          Row(
            children: [
              Expanded(
                child: Container(
                  padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
                  decoration: BoxDecoration(
                    color: connected ? const Color(0xFF12342E) : const Color(0x1AFFFFFF),
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: Row(
                    mainAxisAlignment: MainAxisAlignment.center,
                    children: [
                      Container(
                        width: 8,
                        height: 8,
                        decoration: BoxDecoration(
                          shape: BoxShape.circle,
                          color: connected ? const Color(0xFF3DD598) : Colors.white38,
                        ),
                      ),
                      const SizedBox(width: 8),
                      Flexible(
                        child: Text(
                          connected ? 'Connected to $deviceName' : 'Offline — $deviceName',
                          textAlign: TextAlign.center,
                          style: TextStyle(
                            color: connected ? const Color(0xFFA7E8D0) : Colors.white70,
                            fontWeight: FontWeight.w700,
                            fontSize: 12,
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
              ),
              const SizedBox(width: 10),
              Material(
                color: const Color(0x14FFFFFF),
                shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
                child: InkWell(
                  borderRadius: BorderRadius.circular(12),
                  onTap: onDisconnect,
                  child: const Padding(
                    padding: EdgeInsets.all(10),
                    child: Icon(Icons.power_settings_new, size: 18, color: Colors.white70),
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
              style: const TextStyle(color: Colors.white38, fontSize: 11),
            ),
          ],
          if (!connected) ...[
            const SizedBox(height: 12),
            SizedBox(
              width: double.infinity,
              child: FilledButton.icon(
                style: FilledButton.styleFrom(
                  backgroundColor: const Color(0x1FFFFFFF),
                  foregroundColor: Colors.white70,
                  padding: const EdgeInsets.symmetric(vertical: 11),
                  shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
                ),
                onPressed: sending ? null : onReconnect,
                icon: sending
                    ? const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2, color: Color(0xFFA7E8D0)))
                    : const Icon(Icons.refresh, size: 18),
                label: Text(sending ? 'Connecting…' : 'Reconnect', style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w600)),
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
    return Material(
      color: const Color(0xFF122E29),
      borderRadius: BorderRadius.circular(16),
      child: InkWell(
        borderRadius: BorderRadius.circular(16),
        onTap: onTap,
        child: Container(
          decoration: BoxDecoration(
            borderRadius: BorderRadius.circular(16),
          ),
          padding: const EdgeInsets.symmetric(vertical: 16, horizontal: 12),
          child: Row(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              if (busy)
                const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2, color: Color(0xFF7ED4B8)))
              else
                Container(
                  width: 32,
                  height: 32,
                  decoration: BoxDecoration(
                    color: const Color(0xFF1A4D41),
                    borderRadius: BorderRadius.circular(10),
                  ),
                  child: Icon(icon, color: const Color(0xFF7ED4B8), size: 18),
                ),
              const SizedBox(width: 12),
              Text(
                label,
                style: const TextStyle(color: Color(0xFFA7E8D0), fontWeight: FontWeight.w700, fontSize: 13),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
