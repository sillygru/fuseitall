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
import '../clipboard/clipboard_watcher.dart';
import '../connection/mac_locator.dart';
import '../device/device_info_provider.dart';
import '../home/connection_hero.dart';
import '../home/essential_services_card.dart';
import '../home/paired_devices_card.dart';
import '../notifications/notif_listener.dart';
import '../notifications/notif_models.dart';
import '../pairing/pair_qr.dart';
import '../permissions/permissions.dart';
import '../settings/app_settings.dart';
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

  /// Permissions bridge, clipboard event watcher, and remembered-host
  /// locator. Defaults are the real platform bridges; tests inject fakes.
  final Permissions? permissions;
  final ClipboardWatcher? clipWatcher;
  final MacLocator? locator;

  @override
  State<PingPage> createState() => _PingPageState();
}

class _PingPageState extends State<PingPage> with WidgetsBindingObserver {
  String? _updateMessage;
  String? _updateDetail;
  String? _error;
  String? _serverError;
  bool _sending = false;
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
  StreamSubscription<String>? _clipSub;
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
    _subscribeClipboard();
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

  /// Event-driven clipboard: native change events push instantly instead of
  /// waiting for the next poll. Each event marks local state and flushes.
  void _subscribeClipboard() {
    try {
      final watcher = widget.clipWatcher ?? ClipboardWatcher();
      _clipSub = watcher.changes.listen((text) async {
        if (text.isEmpty || text.length > ClipState.maxLen) return;
        final next = _clip.setLocal(text, _nowUnix());
        if (next == null || next == _clip || !mounted) return;
        setState(() => _clip = next);
        await _flushFeatures();
      }, onError: (_) {});
    } catch (_) {
      // Watcher unavailable (tests, old builds): foreground poll covers it.
    }
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

  /// Apply one Mac-initiated feature envelope from the bridge queue:
  /// clipboard writes (mode-gated), settings adoption (last-writer-wins),
  /// and dismissal cancels. Unknown types are ignored (forward tolerance).
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
    final settings = _settings;
    switch (type) {
      case 'clip-push':
        if (settings == null) return;
        if (!shouldAcceptRemoteClip(
          settings.clipboardMode,
          '${payload['origin']}',
        )) {
          return;
        }
        final next = _clip.applyRemote(
          next: '${payload['text'] ?? ''}',
          nextChangedAt: payload['changed_at'] is int
              ? payload['changed_at'] as int
              : 0,
          nextOrigin: '${payload['origin']}',
        );
        if (next == null || !mounted) return;
        setState(() => _clip = next);
        try {
          final write =
              widget.writeClipboard ??
              (text) => Clipboard.setData(ClipboardData(text: text));
          await write(next.text);
        } catch (_) {
          // Clipboard write failed: keep state, the next push retries.
        }
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
      case 'clip-request':
        // Mac pull: answer with our latest clipboard out of band.
        if (settings == null || !_clip.hasText) return;
        if (!clipDirectionAllows(
          settings.clipboardMode,
          AppSettings.phoneToMac,
        )) {
          return;
        }
        final result = await widget.featureFn(widget.pairing, 'clip-push', {
          'text': _clip.text,
          'changed_at': _clip.changedAt,
          'origin': 'android',
        });
        if (!mounted) return;
        setState(() {
          if (result case Ok()) {
            _clip = _clip.clearPending();
          } else {
            _clip = _clip.requeue();
          }
        });
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
      });
      _startHeartbeat();
      // Immediate re-announce: without this the Mac dials the stale port
      // for up to one heartbeat period (connection refused) after every
      // phone restart. Failures only land in the log, like the heartbeat.
      unawaited(_announcePresence());
    } catch (e) {
      if (!mounted) return;
      setState(() => _serverError = '$e');
    }
  }

  /// One immediate phone→Mac ping after server start so the Mac learns the
  /// current reply_port/reply_fingerprint without waiting for the heartbeat.
  Future<void> _announcePresence() async {
    final port = _phonePort;
    if (port == null) return;
    final facts = await _currentFacts();
    final result = await widget.pingFn(
      widget.pairing,
      replyPort: port,
      replyFingerprint: _phoneFingerprint,
      facts: facts,
    );
    if (!mounted) return;
    if (result case Ok()) {
      unawaited(_locator.remember(widget.pairing.host));
      if (!mounted) return;
    }
    debugPrint(result is Ok ? 'announced presence ok' : 'announce failed: ${(result as Err).failure.message}');
    if (result is Ok) {
      _markSuccess();
      setState(() => _connected = true);
    } else {
      setState(() => _connected = false);
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
    _clipSub?.cancel();
    _clipSub = null;
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
  void _startHeartbeat() {
    if (!mounted) return;
    _heartbeat?.cancel();
    _heartbeat = Timer.periodic(widget.heartbeatInterval, (_) async {
      await _flushFeatures();
      final facts = await _currentFacts();
      final result = await widget.pingFn(
        widget.pairing,
        replyPort: _phonePort,
        replyFingerprint: _phoneFingerprint,
        facts: facts,
      );
      if (!mounted) return;
      if (result case Err(failure: AuthFailure(message: final m))) {
        // The Mac rotated its token while we were idle: revoke rather
        // than logging 403s forever.
        setState(() => _connected = false);
        debugPrint('unpaired by Mac: $m');
        unawaited(_revokedByMac());
        return;
      }
      debugPrint(result is Ok ? 'heartbeat ok' : 'heartbeat failed: ${(result as Err).failure.message}');
      if (result is Ok) {
        _markSuccess();
        setState(() => _connected = true);
      } else {
        setState(() => _markFailure());
      }
    });
  }

  /// One flush round: drain the native listener into the outbox, poll the
  /// local clipboard, then send settings (if dirty), clipboard (if pending
  /// and the mode allows outbound flow), queued posts, and dismissals.
  Future<void> _flushFeatures() async {
    final settings = _settings;
    if (settings == null) return;
    // Native listener -> outbox (queued when offline, capped at 50).
    final drained = await _notifListener.drain();
    for (final item in drained.posts) {
      _outbox.queuePost(item);
    }
    for (final id in drained.removals) {
      _outbox.queueDismiss(id);
    }
    // Local clipboard poll (foreground-only; background reads return null).
    await _pollLocalClipboard(settings);
    // Settings first: the Mac gates clipboard/notifications on the winner.
    if (_settingsDirty) {
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
    // Clipboard out (phone_to_mac or two_way only).
    final pending = _clip.takePending();
    if (pending != null &&
        clipDirectionAllows(settings.clipboardMode, AppSettings.phoneToMac)) {
      final result = await widget.featureFn(widget.pairing, 'clip-push', {
        'text': pending.text,
        'changed_at': pending.changedAt,
        'origin': 'android',
      });
      if (!mounted) return;
      setState(() {
        if (result case Ok()) {
          _clip = _clip.clearPending();
        } else {
          _clip = _clip.requeue();
        }
      });
    }
    // Queued notification posts (5 per round so one burst never blocks).
    if (settings.notificationsEnabled) {
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
    // Dismissals (both directions converge; failures requeue).
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

  Future<void> _pollLocalClipboard(AppSettings settings) async {
    String? text;
    try {
      final read =
          widget.readClipboard ??
          () async {
            final data = await Clipboard.getData('text/plain');
            return data?.text;
          };
      text = await read();
    } catch (_) {
      return;
    }
    if (text == null || text.isEmpty || !mounted) return;
    if (text.length > ClipState.maxLen) return;
    final next = _clip.setLocal(text, _nowUnix());
    if (next != null && next != _clip) {
      setState(() => _clip = next);
    }
  }

  Future<void> _sendPing() async {
    setState(() {
      _sending = true;
      _error = null;
    });
    final watch = Stopwatch()..start();
    final facts = await _currentFacts();
    final result = await widget.pingFn(
      widget.pairing,
      replyPort: _phonePort,
      replyFingerprint: _phoneFingerprint,
      facts: facts,
    );
    watch.stop();
    if (!mounted) return;
    if (result case Ok()) {
      unawaited(_locator.remember(widget.pairing.host));
    }
    if (result is Ok) _lastSuccessAt = DateTime.now();
    setState(() {
      _sending = false;
      switch (result) {
        case Ok():
          _connected = true;
          debugPrint('pong ok rtt=${watch.elapsedMilliseconds}ms');
        case Err(
          failure: UpdateRequired(
            message: final m,
            requiredVersion: final req,
            currentVersion: final cur,
          ),
        ):
          _updateMessage = m;
          _updateDetail = req.isNotEmpty || cur.isNotEmpty
              ? 'Requires ${req.isNotEmpty ? req : 'newer'}'
                    '${cur.isNotEmpty ? ', current $cur' : ''} (this device v$kAppVersion)'
              : null;
          debugPrint('update required');
        case Err(failure: AuthFailure(message: final m)):
          _connected = false;
          debugPrint('unpaired by Mac: $m');
          if (mounted) unawaited(_revokedByMac());
        case Err(failure: final f):
          _connected = false;
          _error = f.message;
          debugPrint('ping failed: ${f.message}');
      }
    });
    unawaited(_refreshPermissions());
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
      final next = _clip.setLocal(text, _nowUnix());
      if (next != null && next != _clip && mounted) setState(() => _clip = next);
      await _flushFeatures();
      // Force push even if poll already queued it — ensure immediate send
      final s = _settings;
      if (s != null && ClipState.validText(text)) {
        final res = await widget.featureFn(widget.pairing, 'clip-push', {'text': text, 'changed_at': _nowUnix(), 'origin': 'android'});
        if (!mounted) return;
        if (res case Ok()) {
          ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Clipboard sent to Mac')));
          setState(() => _clip = _clip.clearPending());
        } else {
          final msg = (res as Err).failure.message;
          ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Send failed: $msg')));
        }
      }
    } finally {
      if (mounted) setState(() => _clipSending = false);
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: Text('Paired: ${widget.pairing.deviceName}'),
      actions: [
        IconButton(
          icon: const Icon(Icons.link_off),
          tooltip: 'Unpair',
          onPressed: _confirmUnpair,
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
        subtitle: _phonePort != null
            ? 'Phone server listening on port $_phonePort'
            : _serverError != null
            ? 'Phone server failed to start: $_serverError'
            : 'Starting phone server…',
        onDisconnect: _confirmDisconnect,
        onReconnect: _reconnect,
        sending: _reconnecting || _sending,
        onSendClipboard: _sendClipboardNow,
        clipSending: _clipSending,
      ),
      const SizedBox(height: 12),
      FilledButton.icon(
        onPressed: _sending ? null : _sendPing,
        icon: _sending
            ? const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2))
            : const Icon(Icons.bolt),
        label: Text(_sending ? 'Pinging…' : 'Send ping to Mac'),
      ),
      if (_error != null) ...[const SizedBox(height: 12), _errorCard(context)],
      const SizedBox(height: 12),
      PairedDevicesCard(
        deviceName: widget.pairing.deviceName,
        online: _isOnline,
        onUnpair: _confirmUnpair,
      ),
      const SizedBox(height: 12),
      _clipboardCard(context),
      const SizedBox(height: 12),
      EssentialServicesCard(status: _permStatus, permissions: _permissions),
      const SizedBox(height: 8),
      Text('FuseItAll v$kAppVersion (build $kAppBuild)', style: Theme.of(context).textTheme.labelSmall),
    ],
  );

  Widget _clipboardCard(BuildContext context) {
    final settings = _settings;
    final mode = settings?.clipboardMode ?? AppSettings.twoWay;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(Icons.content_paste, size: 18),
                const SizedBox(width: 8),
                const Expanded(child: Text('Clipboard', style: TextStyle(fontWeight: FontWeight.bold))),
                IconButton(
                  tooltip: 'Clipboard settings',
                  icon: const Icon(Icons.settings_outlined, size: 20),
                  onPressed: () async {
                    final sel = await showModalBottomSheet<String>(
                      context: context,
                      showDragHandle: true,
                      shape: const RoundedRectangleBorder(borderRadius: BorderRadius.vertical(top: Radius.circular(20))),
                      builder: (ctx) => SafeArea(
                        child: Padding(
                          padding: const EdgeInsets.fromLTRB(16, 8, 16, 24),
                          child: Column(
                            mainAxisSize: MainAxisSize.min,
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              const Text('Clipboard sync', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 16)),
                              const SizedBox(height: 12),
                              for (final m in AppSettings.validModes)
                                RadioListTile<String>(
                                  title: Text(clipboardModeLabel(m)),
                                  value: m,
                                  groupValue: mode,
                                  onChanged: (v) => Navigator.pop(ctx, v),
                                  dense: true,
                                ),
                              const Divider(),
                              SwitchListTile(
                                title: const Text('Phone notifications'),
                                subtitle: const Text('Mirror to Mac'),
                                value: settings?.notificationsEnabled ?? true,
                                onChanged: (enabled) async {
                                  final next = (settings ?? AppSettings.defaults(nowUnix: _nowUnix())).withNotifications(enabled, nowUnix: _nowUnix());
                                  try { await _settingsStore.save(next); } catch (_) { return; }
                                  if (!mounted) return;
                                  setState(() { _settings = next; _settingsDirty = true; });
                                  unawaited(_flushFeatures());
                                  if (context.mounted) Navigator.pop(ctx);
                                },
                              ),
                            ],
                          ),
                        ),
                      ),
                    );
                    if (sel == null || sel == mode) return;
                    final next = (settings ?? AppSettings.defaults(nowUnix: _nowUnix())).withMode(sel, nowUnix: _nowUnix());
                    try { await _settingsStore.save(next); } catch (_) { return; }
                    if (!mounted) return;
                    setState(() { _settings = next; _settingsDirty = true; });
                    unawaited(_flushFeatures());
                  },
                ),
              ],
            ),
            const SizedBox(height: 4),
            Text('Mode: ${clipboardModeLabel(mode)}', style: Theme.of(context).textTheme.labelSmall),
            const SizedBox(height: 8),
            SelectableText(
              _clip.hasText ? 'Clipboard: ${clipPreview(_clip.text)}${_clip.pending ? ' (sending…)' : ''}' : 'Clipboard: nothing synced yet.',
              style: Theme.of(context).textTheme.bodySmall,
            ),
            const SizedBox(height: 8),
            FilledButton.tonalIcon(
              onPressed: _clipSending ? null : _sendClipboardNow,
              icon: _clipSending ? const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2)) : const Icon(Icons.send, size: 16),
              label: Text(_clipSending ? 'Sending…' : 'Send clipboard now'),
            ),
            if (_outbox.posts.isNotEmpty || _outbox.dismissals.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text('Queued: ${_outbox.posts.length} notifications, ${_outbox.dismissals.length} dismissals${_settingsDirty ? ', settings' : ''}.', style: Theme.of(context).textTheme.labelSmall),
              ),
          ],
        ),
      ),
    );
  }

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
