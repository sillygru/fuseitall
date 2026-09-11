// SPDX-License-Identifier: AGPL-3.0-only

import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

import '../../result.dart';
import 'pair_qr.dart';

// Persisted-pairing store: one PairQR JSON blob in the OS keychain/
// keystore. Small seam (KeyValueStorage) so tests inject a fake.
const kPairingStorageKey = 'fuseitall_pairing';

/// Minimal secure-storage surface used by [PairingStore].
abstract class KeyValueStorage {
  Future<String?> read(String key);
  Future<void> write(String key, String value);
  Future<void> delete(String key);
}

/// Real adapter over flutter_secure_storage.
class SecureStorageAdapter implements KeyValueStorage {
  SecureStorageAdapter([FlutterSecureStorage? storage])
      : _storage = storage ?? const FlutterSecureStorage();

  final FlutterSecureStorage _storage;

  @override
  Future<String?> read(String key) => _storage.read(key: key);

  @override
  Future<void> write(String key, String value) =>
      _storage.write(key: key, value: value);

  @override
  Future<void> delete(String key) => _storage.delete(key: key);
}

class PairingStore {
  PairingStore([KeyValueStorage? storage])
      : _storage = storage ?? SecureStorageAdapter();

  final KeyValueStorage _storage;

  /// Load the persisted pairing, or null when absent/corrupt/unreadable.
  /// Fail closed: storage errors and bad JSON fall back to the scan screen;
  /// details go to the debug console only, never the UI.
  Future<PairQR?> load() async {
    String? raw;
    try {
      raw = await _storage.read(kPairingStorageKey);
    } catch (e) {
      debugPrint('pairing load failed: $e');
      return null;
    }
    if (raw == null || raw.isEmpty) return null;
    try {
      switch (parsePairQrJson(raw)) {
        case Ok(value: final qr):
          return qr;
        case Err():
          return null;
      }
    } catch (e) {
      debugPrint('pairing parse failed: $e');
      return null;
    }
  }

  Future<void> save(PairQR pairing) async {
    await _storage.write(
      kPairingStorageKey,
      jsonEncode(pairing.toJson()),
    );
  }

  Future<void> clear() async {
    await _storage.delete(kPairingStorageKey);
  }
}
