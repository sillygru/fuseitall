// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'dart:convert';

import '../../result.dart';

// PairQR mirrors packages/proto/pair-qr.json, the only cross-language
// contract. Unknown JSON keys are ignored; missing `v` defaults to 1.
// `code` is the optional 6-digit human-typable pairing code from the QR:
// anti-mistake UX + consent, NOT a security boundary (token stays secret).
// Missing/empty code means the Mac predates codes (old build).
class PairQR {
  const PairQR({
    required this.v,
    required this.deviceName,
    required this.platform,
    required this.host,
    required this.port,
    required this.fingerprint,
    required this.pubkey,
    required this.token,
    this.code = '',
  });

  final int v;
  final String deviceName;
  final String platform;
  final String host;
  final int port;
  final String fingerprint;
  final String pubkey;
  final String token;
  final String code;

  /// Serialize for flutter_secure_storage persistence (keys match QR JSON).
  Map<String, Object?> toJson() => {
        'v': v,
        'device_name': deviceName,
        'platform': platform,
        'host': host,
        'port': port,
        'fingerprint': fingerprint,
        'pubkey': pubkey,
        'token': token,
        if (code.isNotEmpty) 'code': code,
      };
}

/// Parse raw QR text into [PairQR]. `v` absent means 1; `v != 1` is an
/// [UnsupportedVersion] (update-error screen), never a silent drop.
Result<PairQR> parsePairQrJson(String raw) {
  dynamic decoded;
  try {
    decoded = jsonDecode(raw);
  } on FormatException catch (e) {
    return Err(ParseFailure('QR is not valid JSON: $e'));
  }
  return parsePairQr(decoded);
}

/// Parse an already-decoded JSON value. Unknown keys are ignored.
Result<PairQR> parsePairQr(dynamic decoded) {
  if (decoded is! Map<String, dynamic>) return _fail('QR must be a JSON object.');
  final v = decoded['v'] ?? 1;
  if (v is! int) return _fail('QR field "v" must be an integer.');
  if (v != 1) return Err(UnsupportedVersion(v));
  final port = decoded['port'];
  if (port is! int || port < 1 || port > 65535) {
    return _fail('QR field "port" must be 1..65535.');
  }
  final fp = _string(decoded, 'fingerprint');
  if (fp == null || !_isHex64(_normalizeFp(fp))) {
    return _fail('QR field "fingerprint" must be 64 hex chars.');
  }
  for (final k in const ['device_name', 'platform', 'host', 'pubkey', 'token']) {
    if (_string(decoded, k) == null) return _fail('QR is missing "$k".');
  }
  final code = decoded['code'];
  // Absent code = old Mac build (UI shows the update path). Present but
  // malformed = corrupt/tampered QR: fail loudly, never mislabel as old.
  if (code != null && (code is! String || !_sixDigits.hasMatch(code))) {
    return _fail('QR field "code" must be 6 digits.');
  }
  return Ok(
    PairQR(
      v: 1,
      deviceName: _string(decoded, 'device_name')!,
      platform: _string(decoded, 'platform')!,
      host: _string(decoded, 'host')!,
      port: port,
      fingerprint: _normalizeFp(fp),
      pubkey: _string(decoded, 'pubkey')!,
      token: _string(decoded, 'token')!,
      code: _code(decoded),
    ),
  );
}

Err<PairQR> _fail(String message) => Err(ParseFailure(message));

String? _string(Map<String, dynamic> m, String key) {
  final v = m[key];
  return v is String && v.isNotEmpty ? v : null;
}

/// Optional 6-digit code: absent means the Mac predates codes (old build).
/// Present-but-malformed is rejected by the parser before this runs.
String _code(Map<String, dynamic> m) {
  final v = m['code'];
  if (v is! String) return '';
  return _sixDigits.hasMatch(v) ? v : '';
}

final _sixDigits = RegExp(r'^[0-9]{6}$');

final _hex64 = RegExp(r'^[0-9a-f]{64}$');

bool _isHex64(String s) => _hex64.hasMatch(s);

/// Lowercase, strip colons/whitespace users add when reading fingerprints.
String _normalizeFp(String s) =>
    s.toLowerCase().replaceAll(RegExp(r'[\s:]'), '');

/// Constant-time-ish equality: accumulate XOR over all chars, fail closed
/// on length mismatch. Used for TOFU pin checks and the confirm screen.
bool fingerprintsMatch(String a, String b) {
  final x = _normalizeFp(a);
  final y = _normalizeFp(b);
  if (x.length != y.length) return false;
  var diff = 0;
  for (var i = 0; i < x.length; i++) {
    diff |= x.codeUnitAt(i) ^ y.codeUnitAt(i);
  }
  return diff == 0;
}
