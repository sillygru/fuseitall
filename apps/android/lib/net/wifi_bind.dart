// SPDX-License-Identifier: AGPL-3.0-only

import 'package:flutter/services.dart';

/// Pins pairing sockets to the Wi-Fi transport
/// (`fuseitall/wifibind` native channel). The process default network flaps
/// to cellular while Wi-Fi validates, leaving LAN dials with EHOSTUNREACH
/// even on the right SSID with the right QR host. Binding survives until
/// [unbind]/process death; the native side auto-releases when the bound
/// Wi-Fi is lost. Every call is best-effort and never throws: `false`/null
/// means "unavailable, dial unbound as before".
class WifiBind {
  WifiBind([MethodChannel? channel])
      : _channel = channel ?? const MethodChannel('fuseitall/wifibind');

  final MethodChannel _channel;

  /// Resolve the Wi-Fi transport and pin the process to it. Idempotent
  /// while bound. Returns true only when newly or already bound.
  Future<bool> bindWifi({Duration timeout = const Duration(seconds: 8)}) async {
    try {
      final ok = await _channel.invokeMethod<bool>(
        'bindWifi',
        {'timeoutMs': timeout.inMilliseconds},
      );
      return ok == true;
    } catch (_) {
      return false;
    }
  }

  /// Release a previous bind (and its network request). Idempotent.
  Future<void> unbindWifi() async {
    try {
      await _channel.invokeMethod('unbindWifi');
    } catch (_) {}
  }
}
