// SPDX-License-Identifier: AGPL-3.0-only

import 'dart:convert';

import 'package:flutter/foundation.dart';

import '../features/pairing/pairing_store.dart';

// Persisted phone TLS identity: the PEM-encoded self-signed cert + key the
// phone-side ping server presents. Stored in the OS keychain/keystore (same
// encrypted seam as the pairing token) so an app restart keeps the same TLS
// fingerprint instead of minting a fresh cert every launch.
//
// Device identity survives Unpair: clearing the pairing (QR token) must NOT
// clear this — otherwise every unpair/re-pair would rotate the phone pin.
// Wiping the keychain entry is the documented reset (fresh cert on next
// launch, recovered on the Mac via reply_fingerprint re-pin).
const kPhoneIdentityStorageKey = 'fuseitall_phone_identity';

/// PEM identity blob held in secure storage.
class PhoneIdentity {
  const PhoneIdentity({required this.certPem, required this.keyPem});

  final String certPem;
  final String keyPem;

  bool get isValid =>
      certPem.trim().isNotEmpty && keyPem.trim().isNotEmpty;

  Map<String, String> toJson() => {'cert_pem': certPem, 'key_pem': keyPem};

  static PhoneIdentity? fromJson(Map<String, dynamic> json) {
    final cert = json['cert_pem'];
    final key = json['key_pem'];
    if (cert is! String || key is! String) return null;
    final id = PhoneIdentity(certPem: cert, keyPem: key);
    return id.isValid ? id : null;
  }
}

class PhoneIdentityStore {
  PhoneIdentityStore([KeyValueStorage? storage])
      : _storage = storage ?? SecureStorageAdapter();

  final KeyValueStorage _storage;

  /// Load the persisted phone identity, or null when absent/corrupt.
  /// Fail closed to mint-fresh: storage errors and bad JSON return null;
  /// details go to the debug console only, never the UI.
  Future<PhoneIdentity?> load() async {
    String? raw;
    try {
      raw = await _storage.read(kPhoneIdentityStorageKey);
    } catch (e) {
      debugPrint('phone identity load failed: $e');
      return null;
    }
    if (raw == null || raw.isEmpty) return null;
    try {
      final decoded = jsonDecode(raw);
      if (decoded is! Map<String, dynamic>) return null;
      return PhoneIdentity.fromJson(decoded);
    } catch (e) {
      debugPrint('phone identity parse failed: $e');
      return null;
    }
  }

  Future<void> save(PhoneIdentity identity) async {
    await _storage.write(
      kPhoneIdentityStorageKey,
      jsonEncode(identity.toJson()),
    );
  }

  Future<void> clear() async {
    await _storage.delete(kPhoneIdentityStorageKey);
  }
}
