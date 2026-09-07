// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:crypto/crypto.dart';
import 'package:http/http.dart' show Response;
import 'package:http/io_client.dart';

import '../../result.dart';
import '../../version.dart';
import '../device/device_info_provider.dart';
import '../pairing/pair_qr.dart';

// Hand-written HTTP client against packages/proto (envelope + ping/pong +
// update-required). No gomobile/FFI here; the wiring step adds the
// phone-side server through the seam named in the handoff note.

// This build's side of the version gate (matches packages/proto v1).
// Versions live in lib/version.dart (mirror of core); builds gate,
// appVersion displays.
const kCapabilities = ['ping'];
const kPingPath = '/ping';

// Feature capabilities (packages/proto v1, build 2+). Peers gate per
// message: a build-1 Mac answers 426 to these, surfaced as UpdateRequired.
const kNotifCapability = 'notifications';
const kClipCapability = 'clipboard';
const kSettingsCapability = 'settings-sync';
const kFeatureCapabilities = [
  'ping',
  kNotifCapability,
  kClipCapability,
  kSettingsCapability,
];
const kNotifPath = '/notif';
const kClipPath = '/clip';
const kSettingsPath = '/settings';
const kUnpairPath = '/unpair';

/// Pong echo accepted only when [nonce] equals the ping nonce.
class Pong {
  const Pong({required this.nonce, required this.receivedAt});
  final String nonce;
  final int receivedAt;
}

/// Build the POST body for POST https://host:port/ping. Unknown fields are
/// never emitted; the server is required to ignore any it does not know.
/// [replyPort] advertises the phone-side server port (payload key
/// `reply_port`): packages/proto/ping.json allows additional payload
/// properties, so older Mac builds ignore it while newer ones can ping back.
/// [replyFingerprint] advertises the phone TLS pin (`reply_fingerprint`) so
/// the Mac can re-pin after a phone reinstall without a fresh QR scan;
/// null/empty omits it for older Macs.
/// [facts] advertises the phone's self-reported identity (`device_name`,
/// `model`, `battery_pct`, `charging`) on every ping; null omits them all
/// (older Macs need nothing). Out-of-range battery levels throw: the
/// provider guarantees the range, so this is a programmer error.
Map<String, Object?> buildPingEnvelope(
  String nonce, {
  int? sentAt,
  int? replyPort,
  String? replyFingerprint,
  DeviceFacts? facts,
}) {
  if (replyPort != null && (replyPort < 1 || replyPort > 65535)) {
    throw ArgumentError('replyPort must be 1..65535');
  }
  if (facts?.batteryPct != null &&
      (facts!.batteryPct! < 0 || facts.batteryPct! > 100)) {
    throw ArgumentError('batteryPct must be 0..100');
  }
  final payload = <String, Object?>{
    'nonce': nonce,
    'sent_at': sentAt ?? DateTime.now().toUtc().millisecondsSinceEpoch ~/ 1000,
  };
  if (replyPort != null) payload['reply_port'] = replyPort;
  final fp = replyFingerprint?.trim().toLowerCase() ?? '';
  if (fp.isNotEmpty) payload['reply_fingerprint'] = fp;
  final name = facts?.deviceName?.trim() ?? '';
  if (name.isNotEmpty) payload['device_name'] = name;
  final model = facts?.model?.trim() ?? '';
  if (model.isNotEmpty) payload['model'] = model;
  if (facts?.batteryPct != null) payload['battery_pct'] = facts!.batteryPct;
  if (facts?.charging != null) payload['charging'] = facts!.charging;
  return {
    'protocol_v': kProtocolV,
    'type': 'ping',
    'sender': {
      'platform': 'android',
      'app_build': kAppBuild,
      'min_peer_build': kMinPeerBuild,
      'app_version': kAppVersion,
    },
    'capabilities': kCapabilities,
    'payload': payload,
  };
}

/// 128-bit hex nonce from crypto-strength randomness.
String newNonce() {
  final rnd = Random.secure();
  final bytes = List<int>.generate(16, (_) => rnd.nextInt(256));
  return bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
}

/// Map an HTTP ping response to [Pong]. Pure: unit-tested without network.
/// 426 -> UpdateRequired (server message verbatim). 403 -> AuthFailure.
/// 200 -> pong with strict nonce echo, else NonceMismatch. Fail closed.
Result<Pong> parsePingResponse({
  required int statusCode,
  required String body,
  required String expectedNonce,
}) {
  if (statusCode == 426) return _updateRequired(body);
  if (statusCode == 403) {
    return const Err(
      AuthFailure('Mac rejected the pairing token (403). Re-scan its QR.'),
    );
  }
  if (statusCode != 200) {
    return Err(NetworkFailure('Unexpected status $statusCode from Mac.'));
  }
  dynamic decoded;
  try {
    decoded = jsonDecode(body);
  } on FormatException {
    return const Err(ParseFailure('Pong is not valid JSON.'));
  }
  if (decoded is! Map<String, dynamic>) {
    return const Err(ParseFailure('Pong must be a JSON object.'));
  }
  if (decoded['type'] == 'error') return _updateRequired(body);
  if (decoded['type'] != 'pong') {
    return Err(ParseFailure('Expected pong, got "${decoded['type']}".'));
  }
  final payload = decoded['payload'];
  final nonce = payload is Map<String, dynamic> ? payload['nonce'] : null;
  if (nonce is! String || nonce.isEmpty) {
    return const Err(ParseFailure('Pong is missing payload.nonce.'));
  }
  if (nonce != expectedNonce) {
    return const Err(NonceMismatch('Pong nonce differs. Discarded.'));
  }
  final receivedAt = payload['received_at'];
  return Ok(Pong(nonce: nonce, receivedAt: receivedAt is int ? receivedAt : 0));
}

Result<Pong> _updateRequired(String body) {
  try {
    final decoded = jsonDecode(body);
    if (decoded is Map<String, dynamic>) {
      final payload = decoded['payload'];
      if (payload is Map<String, dynamic>) {
        final code = payload['code'];
        final message = payload['message'];
        if (code == 'UPDATE_REQUIRED' && message is String) {
          final requiredVersion = payload['required_version'];
          final currentVersion = payload['current_version'];
          final requiredBuild = payload['required_build'];
          final device = payload['device'];
          return Err(
            UpdateRequired(
              message,
              requiredVersion: requiredVersion is String ? requiredVersion : '',
              currentVersion: currentVersion is String ? currentVersion : '',
              requiredBuild: requiredBuild is int ? requiredBuild : 0,
              device: device is String ? device : '',
            ),
          );
        }
      }
    }
  } on FormatException {
    // Fall through to the generic 426 failure below.
  }
  return const Err(UpdateRequired('Update required (HTTP 426).'));
}

/// HttpClient that pins the Mac TLS cert to the QR fingerprint (TOFU).
/// Empty/error paths return false: fail closed, never fail open.
HttpClient createTofuClient(String expectedFingerprint) {
  final client = HttpClient();
  client.badCertificateCallback = (cert, host, port) {
    try {
      final digest = sha256.convert(cert.der).toString();
      return fingerprintsMatch(digest, expectedFingerprint);
    } catch (_) {
      return false;
    }
  };
  return client;
}

/// Route for a feature message type. Pure.
String featurePath(String type) {
  switch (type) {
    case 'notif-post':
    case 'notif-dismiss':
      return kNotifPath;
    case 'clip-push':
      return kClipPath;
    case 'settings-sync':
      return kSettingsPath;
    case 'unpair':
      return kUnpairPath;
    default:
      return kPingPath;
  }
}

/// Build a feature envelope (notifications, clipboard, settings-sync).
/// Unknown fields are never emitted; the server ignores any it does not
/// know. [nonce] must be fresh per message; the ack must echo it.
Map<String, Object?> buildFeatureEnvelope(
  String type,
  String nonce,
  Map<String, Object?> payload,
) {
  final body = Map<String, Object?>.from(payload);
  body['nonce'] = nonce;
  return {
    'protocol_v': kProtocolV,
    'type': type,
    'sender': {
      'platform': 'android',
      'app_build': kAppBuild,
      'min_peer_build': kMinPeerBuild,
      'app_version': kAppVersion,
    },
    'capabilities': kFeatureCapabilities,
    'payload': body,
  };
}

/// Map a feature ack response to its echoed nonce. Pure: 426 -> the
/// UpdateRequired failure (verbatim message), 403 -> AuthFailure, 200 ->
/// pong nonce echo (strict), else a typed failure. Fail closed.
Result<String> parseFeatureAck({
  required String type,
  required int statusCode,
  required String body,
  required String expectedNonce,
}) {
  if (statusCode == 426) {
    final upd = _updateRequired(body);
    if (upd case Err(failure: final f)) return Err(f);
    return const Err(UpdateRequired('Update required (HTTP 426).'));
  }
  if (statusCode == 403) {
    return const Err(
      AuthFailure('Mac rejected the pairing token (403). Re-scan its QR.'),
    );
  }
  if (statusCode != 200) {
    return Err(NetworkFailure('Unexpected status $statusCode for $type.'));
  }
  dynamic decoded;
  try {
    decoded = jsonDecode(body);
  } on FormatException {
    return Err(ParseFailure('$type ack is not valid JSON.'));
  }
  if (decoded is! Map<String, dynamic>) {
    return Err(ParseFailure('$type ack must be a JSON object.'));
  }
  if (decoded['type'] == 'error') {
    final upd = _updateRequired(body);
    if (upd case Err(failure: final f)) return Err(f);
    return Err(ParseFailure('$type rejected by Mac.'));
  }
  if (decoded['type'] != 'pong') {
    return Err(ParseFailure('Expected pong ack, got "${decoded['type']}".'));
  }
  final payload = decoded['payload'];
  final nonce = payload is Map<String, dynamic> ? payload['nonce'] : null;
  if (nonce is! String || nonce.isEmpty) {
    return Err(ParseFailure('$type ack is missing payload.nonce.'));
  }
  if (nonce != expectedNonce) {
    return const Err(NonceMismatch('Ack nonce differs. Discarded.'));
  }
  return Ok(nonce);
}

/// POST a feature envelope to the Mac and verify the ack echo. Returns the
/// echoed nonce or a typed [Failure]. [payload] carries the type-specific
/// fields (nonce is stamped here).
Future<Result<String>> sendFeature(
  PairQR pairing,
  String type,
  Map<String, Object?> payload, {
  Duration timeout = const Duration(seconds: 10),
}) async {
  final nonce = newNonce();
  final body = jsonEncode(buildFeatureEnvelope(type, nonce, payload));
  final client = IOClient(createTofuClient(pairing.fingerprint));
  try {
    final uri = Uri.parse(
      'https://${pairing.host}:${pairing.port}${featurePath(type)}',
    );
    final Response resp = await client
        .post(
          uri,
          headers: {
            'Content-Type': 'application/json',
            'Authorization': 'Bearer ${pairing.token}',
          },
          body: body,
        )
        .timeout(timeout);
    return parseFeatureAck(
      type: type,
      statusCode: resp.statusCode,
      body: resp.body,
      expectedNonce: nonce,
    );
  } on TimeoutException {
    return Err(NetworkFailure('$type timed out after 10s.'));
  } catch (e) {
    return Err(NetworkFailure('$type failed: $e'));
  } finally {
    client.close();
  }
}

/// POST the ping envelope to the Mac. Returns the accepted pong or a
/// typed [Failure] the ping page renders (426 message shown verbatim).
/// [replyPort] is sent as payload `reply_port` so the Mac learns where the
/// phone-side server listens; null omits it (older Macs need nothing).
/// [replyFingerprint] is sent as `reply_fingerprint` so the Mac re-pins a
/// rotated phone cert over the token-authenticated channel.
/// [facts] advertises device_name/model/battery on every ping; null omits
/// them (older Macs need nothing).
Future<Result<Pong>> sendPing(
  PairQR pairing, {
  Duration timeout = const Duration(seconds: 10),
  int? replyPort,
  String? replyFingerprint,
  DeviceFacts? facts,
}) async {
  final nonce = newNonce();
  final body = jsonEncode(
    buildPingEnvelope(
      nonce,
      replyPort: replyPort,
      replyFingerprint: replyFingerprint,
      facts: facts,
    ),
  );
  final client = IOClient(createTofuClient(pairing.fingerprint));
  try {
    final uri = Uri.parse('https://${pairing.host}:${pairing.port}$kPingPath');
    final Response resp = await client
        .post(
          uri,
          headers: {
            'Content-Type': 'application/json',
            'Authorization': 'Bearer ${pairing.token}',
          },
          body: body,
        )
        .timeout(timeout);
    return parsePingResponse(
      statusCode: resp.statusCode,
      body: resp.body,
      expectedNonce: nonce,
    );
  } on TimeoutException {
    return const Err(NetworkFailure('Ping timed out after 10s.'));
  } catch (e) {
    return Err(NetworkFailure('Ping failed: $e'));
  } finally {
    client.close();
  }
}
