// SPDX-License-Identifier: AGPL-3.0-only

import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';

import '../pairing/pair_qr.dart';

/// Listens for LAN UDP discovery beacons broadcast by the Mac on startup or
/// network change. Enables instant reconnection across subnets without QR re-scan.
class BeaconListener {
  BeaconListener({
    required this.pairing,
    required this.onMacDiscovered,
  });

  final PairQR pairing;
  final void Function(String host, int port) onMacDiscovered;

  RawDatagramSocket? _socket;
  StreamSubscription<RawSocketEvent>? _sub;
  static const int defaultBeaconPort = 18790;

  Future<void> start({int port = defaultBeaconPort}) async {
    await stop();
    try {
      final socket = await RawDatagramSocket.bind(
        InternetAddress.anyIPv4,
        port,
        reuseAddress: true,
        reusePort: false,
      );
      _socket = socket;
      socket.broadcastEnabled = true;

      _sub = socket.listen((event) {
        if (event == RawSocketEvent.read) {
          final dg = socket.receive();
          if (dg == null) return;
          _handleDatagram(dg.data);
        }
      }, onError: (e) {
        debugPrint('beacon listener socket error: $e');
      });
    } catch (e) {
      debugPrint('failed to bind beacon listener on port $port: $e');
    }
  }

  void _handleDatagram(List<int> bytes) {
    try {
      final text = utf8.decode(bytes);
      final json = jsonDecode(text);
      if (json is! Map<String, dynamic>) return;
      final fp = '${json['fp'] ?? ''}'.trim().toLowerCase();
      final host = '${json['host'] ?? ''}'.trim();
      final port = json['port'] is int ? json['port'] as int : int.tryParse('${json['port']}') ?? 0;

      if (host.isEmpty || port < 1 || port > 65535) return;
      if (fingerprintsMatch(fp, pairing.fingerprint)) {
        debugPrint('discovered paired mac via beacon at $host:$port');
        onMacDiscovered(host, port);
      }
    } catch (_) {}
  }

  @visibleForTesting
  void handleDatagramForTesting(List<int> bytes) => _handleDatagram(bytes);


  /// Broadcast a discovery probe on the LAN to immediately locate the paired Mac.
  Future<void> broadcastProbe({int port = defaultBeaconPort}) async {
    try {
      final probe = jsonEncode({
        'v': 1,
        'type': 'probe',
        'fp': pairing.fingerprint.trim().toLowerCase(),
      });
      final bytes = utf8.encode(probe);
      final socket = _socket ??
          await RawDatagramSocket.bind(
            InternetAddress.anyIPv4,
            0,
          );
      socket.broadcastEnabled = true;
      socket.send(bytes, InternetAddress('255.255.255.255'), port);
      debugPrint('broadcasted discovery probe for Mac on port $port');
      if (_socket == null) {
        socket.close();
      }
    } catch (e) {
      debugPrint('failed to broadcast discovery probe: $e');
    }
  }

  Future<void> stop() async {
    await _sub?.cancel();
    _sub = null;
    try {
      _socket?.close();
    } catch (_) {}
    _socket = null;
  }
}
