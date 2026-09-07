// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../net/go_server.dart';
import '../../net/phone_identity_store.dart';
import '../../result.dart';
import '../../version.dart';
import '../clipboard/clipboard_sync.dart';
import '../connection/mac_locator.dart';
import '../device/device_info_provider.dart';
import '../home/connection_hero.dart';
import '../home/essential_services_card.dart';
import '../notifications/notif_listener.dart';
import '../notifications/notif_models.dart';
import '../pairing/pair_qr.dart';
import '../permissions/permissions.dart';
import '../settings/app_settings.dart';
import '../settings/settings_page.dart';
import '../settings/settings_store.dart';
import 'proto_client.dart';

// Screen 3/3: paired. Starts the phone-side ping server (its port is sent as
// payload `reply_port` so the Mac can reach back), one [Send ping to Mac]
// button, an RTT log, and a banner area that shows HTTP 426 update text
// verbatim when it arrives. Wide screens (>700dp) put actions left, log
// right.
class PingPage extends StatefulWidget {
  const PingPage({
    required this.pairing,
    required this.onUnpair,
    this.phoneServer,
    this.pingFn = sendPing,
    this.featureFn = sendFeature,
    this.heartbeatInterval = const Duration(seconds: 20),
    this.identityStore,
    this.deviceFacts,
    this.settingsStore,
    this.notifListener,
    this.readClipboard,
    this.writeClipboard,
    this.permissions,
    this.clipWatcher,
    this.locator,
    this.onRevoked,
    super.key,
  });

  final PairQR pairing;
  final VoidCallback onUnpair;

  /// Called when the Mac rejects our token (it forgot/rotated us): the
  /// shell wipes the pairing and shows the scan screen with [message].
  /// Defaults to [onUnpair] (silent) when unset, e.g. in tests.
  final void Function(String message)? onRevoked;

  /// Injectable seams for tests: a fake [PhoneServer] (no .so on host) and
  /// a fake ping sender (no network in widget tests).
  final PhoneServer? phoneServer;
  final Future<Result<Pong>> Function(
    PairQR pairing, {
    int? replyPort,
    String? replyFingerprint,
    DeviceFacts? facts,
  })
  pingFn;

  /// Feature sender (notifications, clipboard, settings-sync). Defaults to
  /// the TOFU HTTP client; tests inject a fake (no network in widget tests).
  final Future<Result<String>> Function(
    PairQR pairing,
    String type,
    Map<String, Object?> payload,
  )
  featureFn;

  /// Heartbeat period (20s in prod; shortened in widget tests).
  final Duration heartbeatInterval;

  /// Phone TLS identity store. Defaults to the real secure-storage store;
  /// tests inject a fake. Kept separate from [phoneServer] so widget tests
  /// can use a fake bridge without touching the keychain.
  final PhoneIdentityStore? identityStore;

  /// Self-reported identity (name/model/battery) advertised on every ping.
  /// Defaults to the live plugin-backed provider; tests inject a fake.
  final DeviceFactsProvider? deviceFacts;

  /// App settings persistence. Defaults to the secure-storage store.
  final SettingsStore? settingsStore;

  /// Native notification queue. Defaults to the MethodChannel bridge.
  final NotifListener? notifListener;

  /// Clipboard accessors. Defaults to the platform clipboard; tests inject
  /// fakes. Foreground-only on Android 10+: background reads return null.
  final Future<String?> Function()? readClipboard;
  final Future<void> Function(String text)? writeClipboard;

  /// Permissions bridge and remembered-host locator. Defaults are the
  /// real platform bridges; tests inject fakes.
  final Permissions? permissions;
  final MacLocator? locator;
  // Deprecated: auto clipboard watching removed; kept for compat.
  final dynamic clipWatcher;

  @override
  State<PingPage> createState() => _PingPageState();
}

class _PingPageState extends State<PingPage> with WidgetsBindingObserver {
  String? _updateMessage;
  String? _updateDetail;
  String? _error;
  String? _serverError;
  late final PhoneServer _server;
  late final DeviceFactsProvider _factsProvider;
  late final SettingsStore _settingsStore;
  late final NotifListener _notifListener;
  int? _phonePort;
  String? _phoneFingerprint;
  StreamSubscription<String>? _pingSub;
  StreamSubscription<String>? _featSub;
  Timer? _heartbeat;
  AppSettings? _settings;
  bool _settingsDirty = false;
  var _clip = const ClipState();
  final _outbox = NotifOutbox();
  late final Permissions _permissions;
  late final MacLocator _locator;
  PermissionStatus? _permStatus;
  List<String> _rememberedHosts = const [];
  bool _connected = false;
  bool _reconnecting = false;
  bool _clipSending = false;
  DateTime? _lastSuccessAt;
  int _consecutiveFailures = 0;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _server = widget.phoneServer ?? PhoneServer();
    _factsProvider = widget.deviceFacts ?? LiveDeviceFactsProvider();
    _settingsStore = widget.settingsStore ?? SettingsStore();
    _notifListener = widget.notifListener ?? NotifListener();
    _permissions = widget.permissions ?? Permissions();
    _locator = widget.locator ?? MacLocator();
    _refreshPermissions();
    _startLinkService();
    _loadLocatorHosts();
    _pingSub = _server.onPing.listen((nonce) {
      debugPrint('ping incoming nonce=$nonce');
      if (!mounted) return;
      _markSuccess();
      setState(() => _connected = true);
    });
    _featSub = _server.onFeature.listen((raw) {
      if (!mounted) return;
      _applyFeatureEvent(raw);
    });
    _loadSettings();
    _startPhoneServer();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      debugPrint('app resumed — refreshing permissions & presence');
      unawaited(_refreshPermissions());
      // Battery-efficient: only re-announce if stale (>10s since last success)
      final stale = _lastSuccessAt == null || DateTime.now().difference(_lastSuccessAt!).inSeconds > 10;
      if (stale && _phonePort != null) unawaited(_announcePresence());
    }
  }

  Future<void> _loadSettings() async {
    final stored = await _settingsStore.load();
    if (!mounted) return;
    setState(() => _settings = stored);
  }

  Future<void> _refreshPermissions() async {
    final status = await _permissions.status();
    if (!mounted) return;
    setState(() => _permStatus = status);
  }

  Future<void> _startLinkService() async {
    await _permissions.startLinkService();
  }

  Future<void> _loadLocatorHosts() async {
    final hosts = await _locator.load();
    if (!mounted) return;
    setState(() => _rememberedHosts = hosts);
  }

  /// Clone the pairing for one dial attempt at an alternate host. Auth
  /// identity (token/fingerprint) is unchanged — only the route differs.
  PairQR _pairingForHost(String host) => PairQR(
    v: widget.pairing.v,
    deviceName: widget.pairing.deviceName,
    platform: widget.pairing.platform,
    host: host,
    port: widget.pairing.port,
    fingerprint: widget.pairing.fingerprint,
    pubkey: widget.pairing.pubkey,
    token: widget.pairing.token,
    code: widget.pairing.code,
  );

  /// The Mac rejected our token (it forgot us and rotated): drop local
  /// state and hand the shell a notice for the scan screen. Runs once;
  /// the shell's reset disposes this page, cancelling timers.
  bool _revoked = false;
  Future<void> _revokedByMac() async {
    if (_revoked || !mounted) return;
    _revoked = true;
    try {
      await _locator.clear();
    } catch (_) {}
    await _permissions.stopLinkService();
    final revoke =
        widget.onRevoked ??
        (_) {
          widget.onUnpair();
        };
    revoke(
      'This Mac unpaired FuseItAll (its code changed). '
      'Scan its new QR to pair again.',
    );
  }

  /// User-initiated unpair: best-effort goodbye so the Mac drops us
  /// immediately, then the local wipe regardless of the reply.
  Future<void> _unpair() async {
    try {
      await widget.featureFn(widget.pairing, 'unpair', <String, Object?>{});
    } catch (_) {
      // Best-effort: the local wipe below is what matters.
    }
    await _permissions.stopLinkService();
    widget.onUnpair();
  }

  int _nowUnix() => DateTime.now().toUtc().millisecondsSinceEpoch ~/ 1000;

  int _freshChangedAt() {
    final now = _nowUnix();
    if (_clip.hasText && now <= _clip.changedAt) return _clip.changedAt + 1;
    return now;
  }

  Future<void> _writeClipboardText(String text) async {
    // Try injected writer, then Flutter clipboard, then native MethodChannel.
    if (widget.writeClipboard != null) {
      try {
        await widget.writeClipboard!(text);
        return;
      } catch (_) {}
    }
    try {
      await Clipboard.setData(ClipboardData(text: text));
      return;
    } catch (_) {}
    try {
      const ch = MethodChannel('fuseitall/clipboard');
      await ch.invokeMethod('writeText', {'text': text});
    } catch (_) {}
  }

  /// Apply one Mac-initiated feature envelope from the bridge queue:
  /// clipboard writes, settings adoption (last-writer-wins), and dismissal
  /// cancels. Unknown types are ignored (forward tolerance).
  /// Any accepted inbound proves Mac reached us — mark Online like onPing.
  Future<void> _applyFeatureEvent(String raw) async {
    dynamic decoded;
    try {
      decoded = jsonDecode(raw);
    } catch (_) {
      return;
    }
    if (decoded is! Map<String, dynamic>) return;
    final type = decoded['type'];
    final payload = decoded['payload'];
    if (payload is! Map<String, dynamic>) return;
    _markSuccess();
    if (mounted) setState(() => _connected = true);
    final settings = _settings;
    switch (type) {
      case 'clip-push':
        final ca = payload['changed_at'];
        final nextChangedAt = ca is num ? ca.toInt() : 0;
        final next = _clip.applyRemote(
          next: '${payload['text'] ?? ''}',
          nextChangedAt: nextChangedAt,
          nextOrigin: '${payload['origin']}',
        );
        if (next == null || !mounted) return;
        setState(() => _clip = next);
        try {
          await _writeClipboardText(next.text);
        } catch (_) {}
      case 'settings-sync':
        final remote = AppSettings.fromJson(payload);
        final local = settings ?? AppSettings.defaults(nowUnix: _nowUnix());
        if (!remoteSettingsWins(local, remote)) return;
        try {
          await _settingsStore.save(remote);
        } catch (_) {
          return;
        }
        if (!mounted) return;
        setState(() {
          _settings = remote;
          _settingsDirty = false;
        });
      case 'notif-dismiss':
        final id = NotifItem.cleanId(payload['id'] as String?);
        if (id != null) await _notifListener.dismiss(id);
    }
  }

  /// Fail-soft facts read: the provider never throws by contract, but this
  /// seam also guards third-party plugin crashes so a battery read can never
  /// break presence.
  Future<DeviceFacts?> _currentFacts() async {
    try {
      return await _factsProvider.currentFacts();
    } catch (_) {
      return null;
    }
  }

  Future<void> _startPhoneServer() async {
    try {
      final store = widget.identityStore ?? PhoneIdentityStore();
      final port = await _server.startPhoneServer(
        token: widget.pairing.token,
        identityStore: store,
      );
      if (!mounted) return;
      setState(() {
        _phonePort = port;
        _phoneFingerprint = _server.fingerprint;
        _serverError = null;
      });
      _startHeartbeat();
      // Immediate re-announce: without this the Mac dials the stale port
      // for up to one heartbeat period (connection refused) after every
      // phone restart. Failures only land in the log, like the heartbeat.
      unawaited(_announcePresence());
    } catch (e) {
      if (!mounted) return;
      // “Bad state: already started” is a transient double-start on
      // hot-restart: if port is already known, treat as success.
      if (_phonePort != null) {
        debugPrint('phone server start raced but already listening on $_phonePort: $e');
        setState(() => _serverError = null);
        return;
      }
      debugPrint('phone server start failed: $e');
      setState(() => _serverError = '$e');
      // Auto-retry once after 2s — handles transient “already started” race
      // where stop hadn’t fully cleared. Don’t loop forever.
      // If Go global still holds old server (hot-restart), force clear it.
      if ('$e'.contains('already started')) {
        try {
          // ignore: avoid_catches_without_on_clauses
          FfiBridgeHandle.load().stop();
          debugPrint('forced Go server stop for stale global');
        } catch (_) {}
      }
      Future.delayed(const Duration(seconds: 2), () {
        if (!mounted || _phonePort != null) return;
        // Only retry if we still show error (user didn’t navigate away).
        if (_serverError != null) {
          debugPrint('retrying phone server start');
          unawaited(_startPhoneServer());
        }
      });
    }
  }

  /// One immediate phone→Mac ping after server start so the Mac learns the
  /// current reply_port/reply_fingerprint without waiting for the heartbeat.
  /// Uses orderedTargets (QR host + remembered) to heal DHCP changes without
  /// manual reconnect — fixes “need to press ping to show connected”.
  Future<void> _announcePresence() async {
    final port = _phonePort;
    if (port == null) return;
    final facts = await _currentFacts();
    final targets = MacLocator.orderedTargets(widget.pairing.host, _rememberedHosts);
    Result<Pong>? best;
    String? winner;
    for (final host in targets) {
      final res = await widget.pingFn(
        _pairingForHost(host),
        replyPort: port,
        replyFingerprint: _phoneFingerprint,
        facts: facts,
      );
      if (!mounted) return;
      // AuthFailure is terminal — Mac rotated token, don't keep trying hosts.
      if (res case Err(failure: AuthFailure())) {
        best = res;
        break;
      }
      if (res case Ok()) {
        best = res;
        winner = host;
        break;
      }
      best = res;
    }
    final result = best;
    if (result == null) return;
    if (!mounted) return;
    if (result case Err(failure: UpdateRequired(message: final m, requiredVersion: final req, currentVersion: final cur))) {
      setState(() {
        _updateMessage = m;
        _updateDetail = req.isNotEmpty || cur.isNotEmpty
            ? 'Requires ${req.isNotEmpty ? req : 'newer'}${cur.isNotEmpty ? ', current $cur' : ''} (this device v$kAppVersion)'
            : null;
      });
      debugPrint('announce update required');
      return;
    }
    if (result case Err(failure: AuthFailure(message: final m))) {
      setState(() => _connected = false);
      debugPrint('announce unpaired by Mac: $m');
      unawaited(_revokedByMac());
      return;
    }
    if (result case Ok()) {
      final w = winner ?? widget.pairing.host;
      unawaited(_locator.remember(w));
      // Refresh cached hosts for next heartbeat without waiting for next load.
      unawaited(_loadLocatorHosts());
    }
    debugPrint(result is Ok ? 'announced presence ok via ${winner ?? widget.pairing.host}' : 'announce failed: ${(result as Err).failure.message}');
    if (result is Ok) {
      _markSuccess();
      if (mounted) setState(() => _connected = true);
    } else {
      if (mounted) setState(() => _connected = false);
    }
  }

  bool get _isOnline {
    if (!_connected) return false;
    final last = _lastSuccessAt;
    if (last != null) {
      if (DateTime.now().difference(last).inSeconds > 70) return false;
      return true;
    }
    if (_phonePort == null) return false;
    return true;
  }

  void _markSuccess() {
    _lastSuccessAt = DateTime.now();
    _consecutiveFailures = 0;
  }

  void _markFailure() {
    _consecutiveFailures++;
    // Debounce: single transient failure (freeform, Doze) shouldn't flip offline.
    // Require 2 consecutive failures AND age > 10s to go offline.
    if (_consecutiveFailures >= 2) {
      final last = _lastSuccessAt;
      final stale = last == null || DateTime.now().difference(last).inSeconds > 10;
      if (stale) _connected = false;
    }
    debugPrint('heartbeat failure $_consecutiveFailures, connected=$_connected');
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _heartbeat?.cancel();
    _heartbeat = null;
    _pingSub?.cancel();
    _featSub?.cancel();
    // Always stop: the page owns the server session (injected fakes in
    // tests included), so no poll timer outlives the page.
    unawaited(_server.stopPhoneServer());
    super.dispose();
  }

  /// 20s heartbeat keeps the Mac's presence fresh and re-advertises the
  /// current reply_port across phone DHCP/port changes, then flushes
  /// queued feature syncs (settings, clipboard, notifications). Failures
  /// only append to the log: never the update banner, never the error
  /// block. Queued items stay queued for the next round on failure.
  /// Now tries orderedTargets so stale QR host doesn't require manual ping.
  void _startHeartbeat() {
    if (!mounted) return;
    _heartbeat?.cancel();
    _heartbeat = Timer.periodic(widget.heartbeatInterval, (_) async {
      await _flushFeatures();
      final facts = await _currentFacts();
      final port = _phonePort;
      final fp = _phoneFingerprint;
      // Try QR host then remembered hosts (DHCP-proof). Single ordered walk,
      // not full /24 sweep — explicit Reconnect does sweep.
      final targets = MacLocator.orderedTargets(widget.pairing.host, _rememberedHosts);
      Result<Pong>? best;
      String? winner;
      for (final host in targets) {
        final res = await widget.pingFn(
          _pairingForHost(host),
          replyPort: port,
          replyFingerprint: fp,
          facts: facts,
        );
        if (!mounted) return;
        if (res case Err(failure: AuthFailure())) {
          best = res;
          break;
        }
        if (res case Ok()) {
          best = res;
          winner = host;
          break;
        }
        best = res;
      }
      final result = best;
      if (result == null || !mounted) return;
      if (result case Err(failure: UpdateRequired(message: final m, requiredVersion: final req, currentVersion: final cur))) {
        setState(() {
          _updateMessage = m;
          _updateDetail = req.isNotEmpty || cur.isNotEmpty
              ? 'Requires ${req.isNotEmpty ? req : 'newer'}${cur.isNotEmpty ? ', current $cur' : ''} (this device v$kAppVersion)'
              : null;
        });
        debugPrint('heartbeat update required');
        return;
      }
      if (result case Err(failure: AuthFailure(message: final m))) {
        setState(() => _connected = false);
        debugPrint('unpaired by Mac: $m');
        unawaited(_revokedByMac());
        return;
      }
      if (result case Ok()) {
        final w = winner ?? widget.pairing.host;
        // Remember winning host so next tick starts there.
        unawaited(_locator.remember(w));
        unawaited(_loadLocatorHosts());
      }
      debugPrint(result is Ok ? 'heartbeat ok via ${winner ?? widget.pairing.host}' : 'heartbeat failed: ${(result as Err).failure.message}');
      if (result is Ok) {
        _markSuccess();
        if (mounted) setState(() => _connected = true);
      } else {
        if (mounted) setState(() => _markFailure());
      }
    });
  }

  /// One flush round: drain the native listener into the outbox, send
  /// settings (if dirty), queued posts, and dismissals.
  /// Clipboard manual sends are direct; no polling here.
  Future<void> _flushFeatures() async {
    final settings = _settings;
    final drained = await _notifListener.drain();
    for (final item in drained.posts) {
      _outbox.queuePost(item);
    }
    for (final id in drained.removals) {
      _outbox.queueDismiss(id);
    }
    if (settings != null && _settingsDirty) {
      final result = await widget.featureFn(
        widget.pairing,
        'settings-sync',
        settings.toJson(),
      );
      if (!mounted) return;
      if (result case Ok()) {
        setState(() => _settingsDirty = false);
      }
    }
    if (settings != null && settings.notificationsEnabled) {
      final batch = _outbox.takePosts(5);
      for (final item in batch) {
        final result = await widget.featureFn(
          widget.pairing,
          'notif-post',
          item.toJson(),
        );
        if (!mounted) return;
        if (result case Err()) {
          _outbox.requeuePosts([item]);
          break;
        }
      }
    }
    final dismissals = _outbox.takeDismissals();
    for (final id in dismissals) {
      final result = await widget.featureFn(widget.pairing, 'notif-dismiss', {
        'id': id,
      });
      if (!mounted) return;
      if (result case Err()) {
        _outbox.requeueDismissals([id]);
        break;
      }
    }
  }

  /// Resilient reconnect: try the QR host, then remembered IPs (DHCP-proof),
  /// then a bounded /24 sweep — first 200 wins and is remembered. Failures
  /// land in the log + error card, never silently.
  Future<void> _reconnect() async {
    if (_reconnecting) return;
    setState(() {
      _reconnecting = true;
      _error = null;
    });
    final facts = await _currentFacts();
    final targets = MacLocator.orderedTargets(
      widget.pairing.host,
      _rememberedHosts,
    );
    Result<Pong>? firstOk;
    String? winner;
    var revoked = false;
    Future<bool> tryHost(String host, {required bool sweep}) async {
      final result = await widget.pingFn(
        _pairingForHost(host),
        replyPort: _phonePort,
        replyFingerprint: _phoneFingerprint,
        facts: facts,
      );
      if (!mounted) return true;
      switch (result) {
        case Ok():
          firstOk = result;
          winner = host;
          _lastSuccessAt = DateTime.now();
          setState(() => _connected = true);
          debugPrint(sweep ? 'reconnected via sweep $host' : 'reconnected via $host');
          return true;
        case Err(failure: AuthFailure()):
          // The Mac knows us no longer: stop sweeping, revoke instead.
          revoked = true;
          return true;
        case Err():
          return false;
      }
    }

    for (final host in targets) {
      if (await tryHost(host, sweep: false)) break;
      if (!mounted) return;
    }
    if (firstOk == null && !revoked) {
      for (final host in MacLocator.sweepTargets(widget.pairing.host)) {
        if (await tryHost(host, sweep: true)) break;
        if (!mounted) return;
      }
    }
    if (revoked) {
      if (!mounted) return;
      setState(() {
        _connected = false;
        _reconnecting = false;
      });
      debugPrint('unpaired by Mac during reconnect');
      unawaited(_revokedByMac());
      return;
    }
    final won = winner;
    if (won != null) {
      await _locator.remember(won);
      await _loadLocatorHosts();
      unawaited(_flushFeatures());
    } else if (mounted) {
      setState(() {
        _connected = false;
        _error = 'Mac not found on this Wi-Fi. Make sure both devices share one network and the Mac app is open, then try again.';
      });
      debugPrint('reconnect failed on ${targets.length} hosts');
    }
    if (!mounted) return;
    setState(() => _reconnecting = false);
    unawaited(_refreshPermissions());
  }

  Future<void> _confirmUnpair() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: const Text('Unpair Mac?'),
        content: Text('This removes the pairing with \'${widget.pairing.deviceName}\' and generates a new QR on the Mac. You\'ll need to scan again.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(c, false), child: const Text('Cancel')),
          FilledButton(onPressed: () => Navigator.pop(c, true), child: const Text('Unpair')),
        ],
      ),
    );
    if (ok == true) await _unpair();
  }

  Future<void> _confirmDisconnect() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: const Text('Disconnect?'),
        content: const Text('This clears the live connection. It will reconnect automatically when the Mac is reachable, or tap Reconnect.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(c, false), child: const Text('Cancel')),
          FilledButton(onPressed: () => Navigator.pop(c, true), child: const Text('Disconnect')),
        ],
      ),
    );
    if (ok == true) await _unpair();
  }

  Future<void> _sendClipboardNow() async {
    if (_clipSending) return;
    setState(() => _clipSending = true);
    try {
      String? text;
      try {
        final read = widget.readClipboard ?? () async {
          final data = await Clipboard.getData('text/plain');
          return data?.text;
        };
        text = await read();
      } catch (_) {}
      if (text == null || text.isEmpty) {
        if (mounted) ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Clipboard is empty')));
        return;
      }
      if (text.length > ClipState.maxLen) {
        if (mounted) ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Clipboard too large (max 256KB)')));
        return;
      }
      final ts = _freshChangedAt();
      final next = _clip.setLocal(text, ts);
      if (next != null && next != _clip && mounted) setState(() => _clip = next);
      final sendAt = _clip.hasText ? _clip.changedAt : ts;
      final res = await widget.featureFn(widget.pairing, 'clip-push', {'text': text, 'changed_at': sendAt, 'origin': 'android'});
      if (!mounted) return;
      if (res case Ok()) {
        ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Clipboard sent to Mac')));
        if (mounted) setState(() => _clip = _clip.clearPending());
      } else {
        final msg = (res as Err).failure.message;
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Send failed: $msg')));
        if (mounted) setState(() => _clip = _clip.requeue());
      }
    } finally {
      if (mounted) setState(() => _clipSending = false);
    }
  }

  void _openSettings() {
    Navigator.of(context).push(MaterialPageRoute(
      builder: (_) => SettingsPage(
        deviceName: widget.pairing.deviceName,
        settings: _settings,
        onNotificationsChanged: (enabled) async {
          final cur = _settings ?? AppSettings.defaults(nowUnix: _nowUnix());
          final next = cur.withNotifications(enabled, nowUnix: _nowUnix());
          try {
            await _settingsStore.save(next);
          } catch (_) {
            return;
          }
          if (!mounted) return;
          setState(() {
            _settings = next;
            _settingsDirty = true;
          });
          unawaited(_flushFeatures());
        },
        onUnpair: () {
          Navigator.of(context).pop();
          _confirmUnpair();
        },
      ),
    ));
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: Text('Paired: ${widget.pairing.deviceName}'),
      actions: [
        IconButton(
          icon: const Icon(Icons.settings_outlined),
          tooltip: 'Settings',
          onPressed: _openSettings,
        ),
      ],
    ),
    body: Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 960),
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [_actionColumn(context)],
        ),
      ),
    ),
  );

  Widget _actionColumn(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.stretch,
    mainAxisSize: MainAxisSize.min,
    children: [
      if (_updateMessage != null) ...[
        _updateBanner(_updateMessage!, _updateDetail),
        const SizedBox(height: 12),
      ],
      ConnectionHero(
        deviceName: widget.pairing.deviceName,
        connected: _isOnline,
        subtitle: '',
        onDisconnect: _confirmDisconnect,
        onReconnect: _reconnect,
        sending: _reconnecting,
        onSendClipboard: _sendClipboardNow,
        clipSending: _clipSending,
      ),
      if (_error != null) ...[const SizedBox(height: 12), _errorCard(context)],
      const SizedBox(height: 12),
      EssentialServicesCard(status: _permStatus, permissions: _permissions),
      const SizedBox(height: 8),
      Text('FuseItAll v$kAppVersion (build $kAppBuild)', style: Theme.of(context).textTheme.labelSmall),
    ],
  );

  Widget _errorCard(BuildContext context) => Card(
    color: Theme.of(context).colorScheme.errorContainer,
    child: Padding(
      padding: const EdgeInsets.all(12),
      child: SelectableText.rich(
        TextSpan(
          style: TextStyle(
            color: Theme.of(context).colorScheme.onErrorContainer,
          ),
          children: [
            const TextSpan(
              text: 'Ping failed.\n',
              style: TextStyle(fontWeight: FontWeight.bold),
            ),
            TextSpan(text: _error),
          ],
        ),
      ),
    ),
  );



  Widget _updateBanner(String message, String? detail) {
    final colorScheme = Theme.of(context).colorScheme;
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: colorScheme.tertiaryContainer,
        borderRadius: BorderRadius.circular(16),
      ),
      child: SelectableText.rich(
        key: const Key('updateBannerText'),
        TextSpan(
          style: TextStyle(color: colorScheme.onTertiaryContainer),
          children: [
            const TextSpan(
              text: 'Update required\n',
              style: TextStyle(fontWeight: FontWeight.bold),
            ),
            if (detail != null) TextSpan(text: '$detail\n'),
            TextSpan(text: message),
          ],
        ),
      ),
    );
  }
}
