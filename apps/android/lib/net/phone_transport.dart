// SPDX-License-Identifier: AGPL-3.0-only

// ignore_for_file: prefer_initializing_formals

import 'dart:async';

import 'package:flutter/foundation.dart';

import '../features/connection/mac_locator.dart';
import '../features/device/device_info_provider.dart';
import '../features/pairing/pair_qr.dart';
import '../result.dart';
import '../features/ping/proto_client.dart';
import 'phone_websocket.dart';

/// Canonical phone -> Mac transport. Owns DHCP-proof fallback so every
/// feature (notif, clip, settings, unpair) and presence ping share one dial
/// policy. Adapters stay thin: this is the only place that knows about
/// remembered hosts, orderedTargets, or per-host timeouts.
///
/// Reuses the pure helpers in [proto_client.dart] (sendPing/sendFeature,
/// parse*). Never invents ad-hoc JSON — packages/proto is the only contract.
class PhoneTransport {
  PhoneTransport({
    required PairQR base,
    required MacLocator locator,
    required Future<Result<Pong>> Function(
      PairQR pairing, {
      int? replyPort,
      String? replyFingerprint,
      DeviceFacts? facts,
      String? filesPermission,
      String? photosPermission,
    }) pingFn,
    required Future<Result<String>> Function(
      PairQR pairing,
      String type,
      Map<String, Object?> payload,
    ) featureFn,
    PhoneWebSocket? webSocket,
  })  : _base = base,
        _locator = locator,
        _pingFn = pingFn,
        _featureFn = featureFn,
        _webSocket = webSocket;

  final PairQR _base;
  final MacLocator _locator;
  final Future<Result<Pong>> Function(
    PairQR pairing, {
    int? replyPort,
    String? replyFingerprint,
    DeviceFacts? facts,
    String? filesPermission,
    String? photosPermission,
  }) _pingFn;
  final Future<Result<String>> Function(
    PairQR pairing,
    String type,
    Map<String, Object?> payload,
  ) _featureFn;
  final PhoneWebSocket? _webSocket;

  // Feature per-host timeout: fast failover across up to 4 remembered hosts
  // (4*3s=12s worst). Ping keeps 10s per host (presence is slower).
  static const Duration _featurePerHostTimeout = Duration(seconds: 3);
  static const Duration _pingPerHostTimeout = Duration(seconds: 10);

  PairQR _forHost(String host) => PairQR(
        v: _base.v,
        deviceName: _base.deviceName,
        platform: _base.platform,
        host: host,
        port: _base.port,
        fingerprint: _base.fingerprint,
        pubkey: _base.pubkey,
        token: _base.token,
        code: _base.code,
      );

  /// Ping the Mac with fallback over remembered hosts. Returns the first
  /// terminal result: Ok, AuthFailure (403), or UpdateRequired (426). Network
  /// failures try the next host. On Ok the winner is remembered for next time.
  /// Pure fallback — no sweep here, sweep stays in the explicit reconnect flow.
  Future<({Result<Pong> result, String? winner})> pingWithFallback({
    int? replyPort,
    String? replyFingerprint,
    DeviceFacts? facts,
    String? filesPermission,
    String? photosPermission,
    List<String>? rememberedHosts,
  }) async {
    final ws = _webSocket;
    if (ws != null && ws.isConnected) {
      final env = buildPingEnvelope(
        newNonce(),
        replyPort: replyPort,
        replyFingerprint: replyFingerprint,
        facts: facts,
        filesPermission: filesPermission,
        photosPermission: photosPermission,
      );
      final ok = await ws.sendEnvelope(env);
      if (ok) {
        return (
          result: Ok(Pong(
            nonce: 'ws',
            receivedAt: DateTime.now().toUtc().millisecondsSinceEpoch ~/ 1000,
          )),
          winner: 'ws',
        );
      }
    }

    final remembered = rememberedHosts ?? await _loadHosts();
    final targets = MacLocator.orderedTargets(_base.host, remembered);
    Result<Pong>? last;
    for (final host in targets) {
      final res = await _pingOne(
        host,
        replyPort: replyPort,
        replyFingerprint: replyFingerprint,
        facts: facts,
        filesPermission: filesPermission,
        photosPermission: photosPermission,
      );
      last = res;
      if (res case Ok()) {
        unawaited(_locator.remember(host));
        return (result: res, winner: host);
      }
      if (res case Err(failure: AuthFailure())) {
        return (result: res, winner: null);
      }
      if (res case Err(failure: UpdateRequired())) {
        return (result: res, winner: null);
      }
      // NetworkFailure / NonceMismatch / ParseFailure -> try next host
      debugPrint('phone transport ping via $host failed: ${(res as Err).failure.message}');
    }
    // No success and no terminal error: return last network failure
    return (result: last ?? const Err(NetworkFailure('No ping targets')), winner: null);
  }

  Future<Result<Pong>> _pingOne(
    String host, {
    int? replyPort,
    String? replyFingerprint,
    DeviceFacts? facts,
    String? filesPermission,
    String? photosPermission,
  }) {
    return _pingFn(
      _forHost(host),
      replyPort: replyPort,
      replyFingerprint: replyFingerprint,
      facts: facts,
      filesPermission: filesPermission,
      photosPermission: photosPermission,
    ).timeout(
      _pingPerHostTimeout,
      onTimeout: () => const Err(NetworkFailure('Ping timed out after 10s.')),
    );
  }

  /// Send a feature envelope with fallback over remembered hosts.
  /// Terminal on Ok/AuthFailure/UpdateRequired, retry on NetworkFailure.
  /// Mirrors ping fallback so DHCP changes heal for notif/clip/settings/unpair
  /// alike — fixing P0 for every capability at once.
  Future<({Result<String> result, String? winner})> sendFeatureWithFallback(
    String type,
    Map<String, Object?> payload, {
    List<String>? rememberedHosts,
    Duration perHostTimeout = _featurePerHostTimeout,
  }) async {
    final ws = _webSocket;
    if (ws != null && ws.isConnected) {
      final env = buildFeatureEnvelope(type, newNonce(), payload);
      final ok = await ws.sendEnvelope(env);
      if (ok) {
        return (result: const Ok('ok'), winner: 'ws');
      }
    }

    final remembered = rememberedHosts ?? await _loadHosts();
    final targets = MacLocator.orderedTargets(_base.host, remembered);
    Result<String>? last;
    for (final host in targets) {
      final res = await _sendOne(host, type, payload, perHostTimeout);
      last = res;
      if (res case Ok()) {
        unawaited(_locator.remember(host));
        return (result: res, winner: host);
      }
      if (res case Err(failure: AuthFailure())) {
        debugPrint('phone transport $type via $host auth failed: ${res.failure.message}');
        return (result: res, winner: null);
      }
      if (res case Err(failure: UpdateRequired())) {
        debugPrint('phone transport $type via $host update required: ${res.failure.message}');
        return (result: res, winner: null);
      }
      debugPrint('phone transport $type via $host failed: ${(res as Err).failure.message}');
    }
    return (result: last ?? Err(NetworkFailure('$type: no targets')), winner: null);
  }

  Future<Result<String>> _sendOne(
    String host,
    String type,
    Map<String, Object?> payload,
    Duration timeout,
  ) {
    return _featureFn(_forHost(host), type, payload)
        .timeout(timeout, onTimeout: () => Err(NetworkFailure('$type timed out after ${timeout.inSeconds}s.')));
  }

  Future<List<String>> _loadHosts() async {
    try {
      return await _locator.load();
    } catch (_) {
      return const [];
    }
  }
}
