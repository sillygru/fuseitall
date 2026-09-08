// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'dart:async';
import 'dart:ffi';
import 'dart:io';

import 'package:ffi/ffi.dart';

import 'phone_identity_store.dart';

// FFI seam to libfuseitall.so (built by go_bridge/build_android.sh): the
// phone-side TLS ping server. The .so is packaged via jniLibs; on hosts
// without it every call fails closed with a StateError, never silently.

typedef _StartC = Pointer<Utf8> Function(Pointer<Utf8> token, Int32 port);
typedef _StartDart = Pointer<Utf8> Function(Pointer<Utf8> token, int port);
typedef _StartWithCertC = Pointer<Utf8> Function(
    Pointer<Utf8> token, Int32 port, Pointer<Utf8> cert, Pointer<Utf8> key);
typedef _StartWithCertDart = Pointer<Utf8> Function(
    Pointer<Utf8> token, int port, Pointer<Utf8> cert, Pointer<Utf8> key);
typedef _PemC = Pointer<Utf8> Function();
typedef _PemDart = Pointer<Utf8> Function();
typedef _PollC = Pointer<Utf8> Function();
typedef _PollDart = Pointer<Utf8> Function();
typedef _StopC = Int32 Function();
typedef _StopDart = int Function();
typedef _FreeC = Void Function(Pointer<Utf8> s);
typedef _FreeDart = void Function(Pointer<Utf8> s);

/// Thin handle over the exported C functions. Kept separate from
/// [PhoneServer] so tests can inject a fake without a .so on the host VM.
abstract class BridgeHandle {
  /// Start with [token] on [port] (0 = ephemeral). Returns the raw
  /// "actualPort:fingerprint" string, or null on failure.
  String? start(String token, int port);

  /// Start with a persisted cert ([certPem]/[keyPem]). Returns the raw
  /// "actualPort:fingerprint" string, or null when the PEMs are bad, the
  /// symbol is missing (older .so), or startup fails. Callers fall back to
  /// [start] (mint fresh) on null.
  String? startWithCert(
      String token, int port, String certPem, String keyPem);

  /// Active server cert PEM after start, or null when unknown/unavailable.
  String? certPem();

  /// Active server key PEM after start, or null when unknown/unavailable.
  String? keyPem();

  /// Next accepted-ping nonce, or null when the queue is empty.
  String? poll();

  /// Next accepted feature envelope (raw JSON for /notif, /clip, /settings
  /// posts), or null when the queue is empty or the .so predates the
  /// PhonePollEvent symbol (older builds simply miss Mac-initiated pushes).
  String? pollEvent();

  /// Stop the server. Returns 0 on stop, 1 when nothing was running.
  int stop();

  /// Last start failure, or null when the last start succeeded or no start
  /// was attempted. Null when the .so predates PhoneLastError (older build).
  String? lastError();
}

/// Real [BridgeHandle] backed by libfuseitall.so. Loading throws StateError
/// when the library is absent: fail closed, never a silent no-op server.
class FfiBridgeHandle implements BridgeHandle {
  FfiBridgeHandle._(
    DynamicLibrary lib,
    this._start,
    this._poll,
    this._stop,
    this._free,
  ) {
    try {
      _startWithCert =
          lib.lookupFunction<_StartWithCertC, _StartWithCertDart>(
              'PhoneStartWithCert');
    } catch (_) {
      _startWithCert = null;
    }
    try {
      _certPem = lib.lookupFunction<_PemC, _PemDart>('PhoneCertPEM');
    } catch (_) {
      _certPem = null;
    }
    try {
      _keyPem = lib.lookupFunction<_PemC, _PemDart>('PhoneKeyPEM');
    } catch (_) {
      _keyPem = null;
    }
    try {
      _pollEvent = lib.lookupFunction<_PollC, _PollDart>('PhonePollEvent');
    } catch (_) {
      _pollEvent = null;
    }
    try {
      _lastError = lib.lookupFunction<_PemC, _PemDart>('PhoneLastError');
    } catch (_) {
      _lastError = null;
    }
  }

  factory FfiBridgeHandle.load() {
    if (!Platform.isAndroid) {
      throw StateError('Phone server runs on Android only (no libfuseitall).');
    }
    try {
      final lib = DynamicLibrary.open('libfuseitall.so');
      return FfiBridgeHandle._(
        lib,
        lib.lookupFunction<_StartC, _StartDart>('PhoneStart'),
        lib.lookupFunction<_PollC, _PollDart>('PhonePoll'),
        lib.lookupFunction<_StopC, _StopDart>('PhoneStop'),
        lib.lookupFunction<_FreeC, _FreeDart>('PhoneFree'),
      );
    } catch (e) {
      throw StateError('Phone server library missing: $e');
    }
  }

  final _StartDart _start;
  final _PollDart _poll;
  final _StopDart _stop;
  final _FreeDart _free;
  _StartWithCertDart? _startWithCert;
  _PemDart? _certPem;
  _PemDart? _keyPem;
  _PollDart? _pollEvent;
  _PemDart? _lastError;

  String? _readNullableString(_PemDart? fn) {
    final f = fn;
    if (f == null) return null;
    final out = f();
    if (out == nullptr) return null;
    try {
      final s = out.toDartString();
      return s.isEmpty ? null : s;
    } finally {
      _free(out);
    }
  }

  @override
  String? start(String token, int port) {
    final tokenPtr = token.toNativeUtf8();
    try {
      final out = _start(tokenPtr, port);
      if (out == nullptr) return null;
      try {
        return out.toDartString();
      } finally {
        _free(out);
      }
    } finally {
      calloc.free(tokenPtr);
    }
  }

  @override
  String? startWithCert(
      String token, int port, String certPem, String keyPem) {
    final fn = _startWithCert;
    if (fn == null) return null;
    final tokenPtr = token.toNativeUtf8();
    final certPtr = certPem.toNativeUtf8();
    final keyPtr = keyPem.toNativeUtf8();
    try {
      final out = fn(tokenPtr, port, certPtr, keyPtr);
      if (out == nullptr) return null;
      try {
        return out.toDartString();
      } finally {
        _free(out);
      }
    } finally {
      calloc.free(tokenPtr);
      calloc.free(certPtr);
      calloc.free(keyPtr);
    }
  }

  @override
  String? certPem() => _readNullableString(_certPem);

  @override
  String? keyPem() => _readNullableString(_keyPem);

  @override
  String? poll() {
    final out = _poll();
    if (out == nullptr) return null;
    try {
      return out.toDartString();
    } finally {
      _free(out);
    }
  }

  @override
  String? pollEvent() {
    final fn = _pollEvent;
    if (fn == null) return null;
    final out = fn();
    if (out == nullptr) return null;
    try {
      return out.toDartString();
    } finally {
      _free(out);
    }
  }

  @override
  int stop() => _stop();

  @override
  String? lastError() => _readNullableString(_lastError);
}

/// Phone-side ping server lifecycle: start on an ephemeral port, stream the
/// nonce of each accepted ping, stop on dispose. Inject [openBridge] in tests
/// to avoid loading the .so on the host VM.
class PhoneServer {
  PhoneServer({BridgeHandle Function()? openBridge})
      : _openBridge = openBridge ?? FfiBridgeHandle.load;

  final BridgeHandle Function() _openBridge;
  BridgeHandle? _bridge;
  StreamController<String>? _ctrl;
  StreamController<String>? _featCtrl;
  int? _port;
  String? _fingerprint;

  /// Actual bound port after [startPhoneServer], null before start.
  int? get port => _port;

  /// Hex SHA-256 of the phone server TLS cert after [startPhoneServer].
  /// Advertised as `reply_fingerprint` so the Mac can re-pin after a phone
  /// reinstall or cert rotation without a fresh QR scan.
  String? get fingerprint => _fingerprint;

  /// Nonce per accepted ping. Broadcast: the page log and any listener share it.
  Stream<String> get onPing {
    _ctrl ??= StreamController<String>.broadcast();
    return _ctrl!.stream;
  }

  /// Raw JSON envelope per accepted Mac-initiated feature post (/notif,
  /// /clip, /settings). Broadcast: the page applies clipboard writes,
  /// settings adoption, and dismissal cancels.
  Stream<String> get onFeature {
    _featCtrl ??= StreamController<String>.broadcast();
    return _featCtrl!.stream;
  }

  /// Start the phone server with the pairing [token] on an ephemeral port.
  /// Returns the actual bound port so callers can advertise it as `reply_port`.
  /// When [identityStore] is given, the TLS identity is load-or-mint: a
  /// stored cert is reused (stable fingerprint across restarts); otherwise a
  /// fresh cert is minted and persisted. A corrupt stored identity falls back
  /// to mint-fresh and overwrites the entry.
  /// Throws ArgumentError on an empty token, StateError when the library or
  /// startup fails. Fail closed: no port is returned unless serving.
  Future<int> startPhoneServer(
      {required String token, PhoneIdentityStore? identityStore}) async {
    if (token.isEmpty) throw ArgumentError('token must not be empty');
    if (_bridge != null) throw StateError('phone server already started');
    final bridge = _openBridge();
    String? raw;
    String? startErr;
    if (identityStore != null) {
      raw = await _startWithStableIdentity(bridge, token, identityStore);
    } else {
      raw = bridge.start(token, 0);
    }
    if (raw == null) {
      try {
        startErr = bridge.lastError();
      } catch (_) {
        startErr = null;
      }
      final detail = startErr != null && startErr.isNotEmpty ? ': $startErr' : '';
      throw StateError('phone server failed to start$detail');
    }
    final parsed = _parseStartResult(raw);
    _bridge = bridge;
    _port = parsed.port;
    _fingerprint = parsed.fingerprint;
    _ctrl ??= StreamController<String>.broadcast();
    _featCtrl ??= StreamController<String>.broadcast();
    return parsed.port;
  }

  /// Drain any pending bridge events without running a background polling timer.
  void drainEvents() {
    final bridge = _bridge;
    if (bridge == null) return;
    final nonce = bridge.poll();
    if (nonce != null && nonce.isNotEmpty) _ctrl?.add(nonce);
    final event = bridge.pollEvent();
    if (event != null && event.isNotEmpty) _featCtrl?.add(event);
  }

  /// No-op: polling timers are permanently eliminated in favor of real-time WebSocket.
  void pausePolling() {}

  /// No-op: polling timers are permanently eliminated in favor of real-time WebSocket.
  void resumePolling() {}

  /// Load-or-mint helper: reuse the stored PEMs when they work, else mint
  /// fresh via [bridge.start] and persist the new PEMs. Returns the raw
  /// "port:fingerprint" result, or null when both paths fail.
  Future<String?> _startWithStableIdentity(
    BridgeHandle bridge,
    String token,
    PhoneIdentityStore identityStore,
  ) async {
    PhoneIdentity? stored;
    try {
      stored = await identityStore.load();
    } catch (_) {
      stored = null;
    }
    if (stored != null) {
      final raw = bridge.startWithCert(
          token, 0, stored.certPem, stored.keyPem);
      if (raw != null) return raw;
      // Corrupt/rotated PEMs: fall through to mint-fresh below.
    }
    final raw = bridge.start(token, 0);
    if (raw == null) return null;
    try {
      final cert = bridge.certPem();
      final key = bridge.keyPem();
      if (cert != null && cert.isNotEmpty && key != null && key.isNotEmpty) {
        await identityStore.save(
            PhoneIdentity(certPem: cert, keyPem: key));
      }
    } catch (_) {
      // Persistence is best-effort: the server is already up with a fresh
      // cert; the next launch will simply mint again.
    }
    return raw;
  }

  /// Stop the server. Idempotent.
  Future<void> stopPhoneServer() async {
    try {
      _bridge?.stop();
    } catch (_) {
      // Stopping must not throw: the server is gone either way.
    }
    _bridge = null;
    _port = null;
    _fingerprint = null;
    await _ctrl?.close();
    _ctrl = null;
    await _featCtrl?.close();
    _featCtrl = null;
  }

  /// Split the "actualPort:fingerprint" result. Throws StateError when the
  /// bridge returns a malformed string: never trust a port we cannot parse.
  /// Returns the port plus the fingerprint (empty when the bridge omits it,
  /// e.g. older fakes in tests — callers then omit reply_fingerprint).
  static ({int port, String fingerprint}) _parseStartResult(String raw) {
    final sep = raw.indexOf(':');
    if (sep <= 0) throw StateError('phone server returned a bad address');
    final port = int.tryParse(raw.substring(0, sep));
    if (port == null || port < 1 || port > 65535) {
      throw StateError('phone server returned a bad port');
    }
    final fingerprint = raw.substring(sep + 1).trim().toLowerCase();
    return (port: port, fingerprint: fingerprint);
  }
}
