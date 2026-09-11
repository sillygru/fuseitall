// SPDX-License-Identifier: AGPL-3.0-only

import '../pairing/pairing_store.dart';

/// Remembered Mac IPs for DHCP-proof reconnect. Stores the last working
/// hosts (most-recent-first, max 4) as a sidecar next to the pairing blob:
/// the QR payload itself is the versioned contract and stays untouched.
const kLocatorStorageKey = 'fuseitall_mac_hosts';
const kMaxLocatorHosts = 4;

class MacLocator {
  MacLocator([KeyValueStorage? storage])
    : _storage = storage ?? SecureStorageAdapter();

  final KeyValueStorage _storage;

  Future<List<String>> load() async {
    try {
      final raw = await _storage.read(kLocatorStorageKey);
      if (raw == null || raw.isEmpty) return const [];
      return raw
          .split(',')
          .map((h) => h.trim())
          .where((h) => h.isNotEmpty)
          .take(kMaxLocatorHosts)
          .toList();
    } catch (_) {
      return const [];
    }
  }

  /// Record a working host as most-recent (dedupe, cap). Never throws.
  Future<void> remember(String host) async {
    host = host.trim();
    if (host.isEmpty) return;
    try {
      final current = await load();
      final next = [
        host,
        ...current.where((h) => h != host),
      ].take(kMaxLocatorHosts).toList();
      await _storage.write(kLocatorStorageKey, next.join(','));
    } catch (_) {}
  }

  /// Drop all remembered hosts (unpair/revoke). Never throws.
  Future<void> clear() async {
    try {
      await _storage.delete(kLocatorStorageKey);
    } catch (_) {}
  }

  /// Dial order: primary QR host first, then remembered candidates, then QR candidates.
  static List<String> orderedTargets(String primary, List<String> remembered, [List<String>? candidates]) {
    final seen = <String>{};
    final out = <String>[];
    for (final h in [primary, ...remembered, ...?candidates]) {
      final t = h.trim();
      if (t.isEmpty || !seen.add(t)) continue;
      out.add(t);
    }
    return out;
  }

  /// /24 sweep targets for the manual Reconnect fallback (same port).
  /// Pure: callers dial with short timeouts, first 200 wins.
  static List<String> sweepTargets(String primary, {int cap = 16}) {
    final parts = primary.trim().split('.');
    if (parts.length != 4) return const [];
    final octets = parts.map(int.tryParse).toList();
    if (octets.any((o) => o == null || o < 0 || o > 255)) return const [];
    final base = '${octets[0]}.${octets[1]}.${octets[2]}';
    final last = octets[3] ?? 0;
    final out = <String>[];
    for (var delta = 1; delta <= cap; delta++) {
      for (final cand in [last + delta, last - delta]) {
        if (cand < 1 || cand > 254 || out.length >= cap) continue;
        out.add('$base.$cand');
      }
    }
    return out;
  }
}
