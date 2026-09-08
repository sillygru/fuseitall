// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';

import '../features/pairing/pair_qr.dart';
import '../features/ping/proto_client.dart';

enum WsConnectionState {
  disconnected,
  connecting,
  connected,
}

/// Real-time persistent TLS WebSocket connection to the Mac.
/// Replaces the 20-second HTTP heartbeat with a persistent bi-directional
/// event stream over `wss://<host>:<port>/ws` with TOFU cert pinning.
class PhoneWebSocket {
  PhoneWebSocket({
    required this.pairing,
    this.onStateChanged,
    this.customClient,
    this.useTls = true,
  });

  final PairQR pairing;
  final void Function(WsConnectionState state)? onStateChanged;
  final HttpClient? customClient;
  final bool useTls;

  WebSocket? _ws;
  StreamSubscription<dynamic>? _wsSub;
  WsConnectionState _state = WsConnectionState.disconnected;
  final _envelopeCtrl = StreamController<Map<String, dynamic>>.broadcast();

  int _connectEpoch = 0;
  String? _connectedHost;

  WsConnectionState get state => _state;
  bool get isConnected => _state == WsConnectionState.connected;
  String? get connectedHost => _connectedHost;
  Stream<Map<String, dynamic>> get onEnvelope => _envelopeCtrl.stream;

  void _setState(WsConnectionState next) {
    if (_state == next) return;
    _state = next;
    onStateChanged?.call(next);
  }

  /// Connect to the Mac at [host]:[port] over TLS WebSocket.
  /// Validates the server certificate SHA-256 against pairing.fingerprint (TOFU).
  Future<bool> connect(String host, int port) async {
    if (isConnected) return true;
    final winner = await fastConnect([host], port);
    return winner != null;
  }

  /// Dial a single host without mutating instance state.
  /// Returns the connected WebSocket or null on failure.
  Future<WebSocket?> _dial({
    required String host,
    required int port,
    HttpClient? customClient,
    bool? useTls,
    Duration timeout = const Duration(seconds: 4),
  }) async {
    final completer = Completer<WebSocket?>();
    runZonedGuarded(() async {
      try {
        final client = customClient ?? this.customClient ?? createTofuClient(pairing.fingerprint);
        final scheme = (useTls ?? this.useTls) ? 'wss' : 'ws';
        final url = '$scheme://$host:$port/ws';
        final ws = await WebSocket.connect(
          url,
          headers: {'Authorization': 'Bearer ${pairing.token}'},
          customClient: client,
        ).timeout(timeout);
        if (!completer.isCompleted) completer.complete(ws);
      } catch (e) {
        debugPrint('phone websocket dial to $host:$port failed: $e');
        if (!completer.isCompleted) completer.complete(null);
      }
    }, (err, _) {
      debugPrint('phone websocket dial zoned error to $host:$port: $err');
      if (!completer.isCompleted) completer.complete(null);
    });
    return completer.future;
  }

  /// Atomically installs an established WebSocket as the active connection.
  void _installSocket(WebSocket ws, String host) {
    _cleanupSocket();
    _ws = ws;
    _connectedHost = host;
    _setState(WsConnectionState.connected);
    debugPrint('phone websocket connected to $host');

    _wsSub = ws.listen(
      (data) {
        if (data is String) {
          try {
            final decoded = jsonDecode(data);
            if (decoded is Map<String, dynamic>) {
              _envelopeCtrl.add(decoded);
            }
          } catch (e) {
            debugPrint('ws decode error: $e');
          }
        }
      },
      onDone: () {
        debugPrint('phone websocket closed by peer');
        if (_ws == ws) {
          _handleDisconnect();
        }
      },
      onError: (err) {
        debugPrint('phone websocket error: $err');
        if (_ws == ws) {
          _handleDisconnect();
        }
      },
      cancelOnError: true,
    );
  }

  /// Fast parallel connection to candidate hosts (Happy Eyeballs pattern).
  /// Dials candidates in parallel with 50ms stagger. Returns the winning host or null.
  /// Individual dial failures or timeouts NEVER close an established connection.
  Future<String?> fastConnect(List<String> candidates, int port) async {
    if (isConnected) return _connectedHost ?? pairing.host;
    if (candidates.isEmpty) return null;

    final epoch = ++_connectEpoch;
    _setState(WsConnectionState.connecting);

    final completer = Completer<String?>();
    var remaining = candidates.length;

    for (var i = 0; i < candidates.length; i++) {
      if (completer.isCompleted || epoch != _connectEpoch) break;
      final host = candidates[i];
      if (i > 0) {
        await Future<void>.delayed(const Duration(milliseconds: 50));
      }
      if (completer.isCompleted || epoch != _connectEpoch) break;

      unawaited(_dial(host: host, port: port).then((ws) {
        if (epoch != _connectEpoch) {
          // A newer connection attempt was started; abort this dial result
          if (ws != null) {
            try {
              ws.close(WebSocketStatus.normalClosure);
            } catch (_) {}
          }
          return;
        }

        if (ws != null) {
          if (!completer.isCompleted) {
            _installSocket(ws, host);
            completer.complete(host);
          } else {
            // Another candidate already won; close this one
            try {
              ws.close(WebSocketStatus.normalClosure);
            } catch (_) {}
          }
        } else {
          remaining--;
          if (remaining <= 0 && !completer.isCompleted) {
            _setState(WsConnectionState.disconnected);
            completer.complete(null);
          }
        }
      }));
    }

    return completer.future;
  }

  /// Connect to the Mac with custom client / TLS options (used in testing).
  Future<bool> connectWithClient(
    String host,
    int port, {
    HttpClient? customClient,
    bool useTls = true,
  }) async {
    if (isConnected) return true;

    final epoch = ++_connectEpoch;
    _setState(WsConnectionState.connecting);

    final ws = await _dial(
      host: host,
      port: port,
      customClient: customClient,
      useTls: useTls,
    );

    if (epoch != _connectEpoch) {
      if (ws != null) {
        try {
          ws.close(WebSocketStatus.normalClosure);
        } catch (_) {}
      }
      return false;
    }

    if (ws != null) {
      _installSocket(ws, host);
      return true;
    } else {
      _setState(WsConnectionState.disconnected);
      return false;
    }
  }

  void _handleDisconnect() {
    _cleanupSocket();
    _connectedHost = null;
    _setState(WsConnectionState.disconnected);
  }

  /// Write a wire envelope directly to the persistent WebSocket.
  Future<bool> sendEnvelope(Map<String, dynamic> env) async {
    final ws = _ws;
    if (ws == null || _state != WsConnectionState.connected) {
      return false;
    }
    try {
      final text = jsonEncode(env);
      ws.add(text);
      return true;
    } catch (e) {
      debugPrint('ws send error: $e');
      _handleDisconnect();
      return false;
    }
  }

  void _cleanupSocket() {
    unawaited(_wsSub?.cancel());
    _wsSub = null;
    final ws = _ws;
    _ws = null;
    if (ws != null) {
      try {
        ws.close(WebSocketStatus.normalClosure);
      } catch (_) {}
    }
  }

  Future<void> dispose() async {
    _connectEpoch++;
    _cleanupSocket();
    _connectedHost = null;
    _setState(WsConnectionState.disconnected);
    await _envelopeCtrl.close();
  }
}
