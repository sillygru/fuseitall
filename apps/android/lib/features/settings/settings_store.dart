// SPDX-License-Identifier: AGPL-3.0-only

import 'dart:convert';

import 'package:flutter/foundation.dart';

import '../pairing/pairing_store.dart';
import 'app_settings.dart';

// Persisted app-settings store: one AppSettings JSON blob in secure
// storage (same seam as PairingStore so tests inject a fake). A missing or
// corrupt blob falls back to defaults; storage errors go to the debug
// console only, never the UI.
const kSettingsStorageKey = 'fuseitall_settings';

class SettingsStore {
  SettingsStore([KeyValueStorage? storage])
      : _storage = storage ?? SecureStorageAdapter();

  final KeyValueStorage _storage;

  /// Load persisted settings, or defaults when absent/corrupt/unreadable.
  /// Never throws.
  Future<AppSettings> load() async {
    String? raw;
    try {
      raw = await _storage.read(kSettingsStorageKey);
    } catch (e) {
      debugPrint('settings load failed: $e');
      return AppSettings.defaults(nowUnix: _now());
    }
    if (raw == null || raw.isEmpty) {
      return AppSettings.defaults(nowUnix: _now());
    }
    try {
      final decoded = jsonDecode(raw);
      if (decoded is Map<String, dynamic>) {
        return AppSettings.fromJson(decoded);
      }
      return AppSettings.defaults(nowUnix: _now());
    } catch (e) {
      debugPrint('settings parse failed: $e');
      return AppSettings.defaults(nowUnix: _now());
    }
  }

  Future<void> save(AppSettings settings) async {
    await _storage.write(kSettingsStorageKey, jsonEncode(settings.toJson()));
  }

  int _now() => DateTime.now().toUtc().millisecondsSinceEpoch ~/ 1000;
}
