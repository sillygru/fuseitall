// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Reading this as: primary paired shell for connection + devices + settings,
// following HIG 1/2/3/4/5/6/7/8/9/10/11/14.

import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../navigation/root_shell.dart';
import '../../net/go_server.dart';
import '../../net/phone_identity_store.dart';
import '../../result.dart';
import '../../version.dart';
import '../../widgets/error_card.dart';
import '../../widgets/update_banner.dart';
import '../clipboard/clipboard_sync.dart';
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
import '../settings/settings_page.dart';
import '../settings/settings_store.dart';
import 'proto_client.dart';

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
  final void Function(String message)? onRevoked;
  final PhoneServer? phoneServer;
  final Future<Result<Pong>> Function(
    PairQR pairing, {
    int? replyPort,
    String? replyFingerprint,
    DeviceFacts? facts,
  })
  pingFn;
  final Future<Result<String>> Function(
    PairQR pairing,
    String type,
    Map<String, Object?> payload,
  )
  featureFn;
  final Duration heartbeatInterval;
  final PhoneIdentityStore? identityStore;
  final DeviceFactsProvider? deviceFacts;
  final SettingsStore? settingsStore;
  final NotifListener? notifListener;
  final Future<String?> Function()? readClipboard;
  final Future<void> Function(String text)? writeClipboard;
  final Permissions? permissions;
  final MacLocator? locator;
  final dynamic clipWatcher;

  @override
  State<PingPage> createState() => _PingPageState();
}

class _PingPageState extends State<PingPage> with WidgetsBindingObserver {
  String? _updateMessage;
  String? _updateDetail;
  String? _error;
  String? _serverError;
  String? _clipInfo;
  String? _clipError;
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

  bool _revoked = false;
  Future<void> _revokedByMac() async {
    if (_revoked || !mounted) return;
    _revoked = true;
    try {
      await _locator.clear();
    } catch (_) {}
    await _permissions.stopLinkService();
    final revoke = widget.onRevoked ?? (_) => widget.onUnpair();
    revoke(
      'This Mac unpaired FuseItAll (its code changed). '
      'Scan its new QR to pair again.',
    );
  }

  Future<void> _unpair() async {
    try {
      await widget.featureFn(widget.pairing, 'unpair', <String, Object?>{});
    } catch (_) {}
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
      unawaited(_announcePresence());
    } catch (e) {
      if (!mounted) return;
      if (_phonePort != null) {
        debugPrint('phone server start raced but already listening on $_phonePort: $e');
        setState(() => _serverError = null);
        return;
      }
      debugPrint('phone server start failed: $e');
      setState(() => _serverError = '$e');
      if ('$e'.contains('already started')) {
        try {
          FfiBridgeHandle.load().stop();
          debugPrint('forced Go server stop for stale global');
        } catch (_) {}
      }
      Future.delayed(const Duration(seconds: 2), () {
        if (!mounted || _phonePort != null) return;
        if (_serverError != null) {
          debugPrint('retrying phone server start');
          unawaited(_startPhoneServer());
        }
      });
    }
  }

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
    unawaited(_server.stopPhoneServer());
    super.dispose();
  }

  void _startHeartbeat() {
    if (!mounted) return;
    _heartbeat?.cancel();
    _heartbeat = Timer.periodic(widget.heartbeatInterval, (_) async {
      await _flushFeatures();
      final facts = await _currentFacts();
      final port = _phonePort;
      final fp = _phoneFingerprint;
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
      final result = await widget.featureFn(widget.pairing, 'settings-sync', settings.toJson());
      if (!mounted) return;
      if (result case Ok()) {
        setState(() => _settingsDirty = false);
      }
    }
    if (settings != null && settings.notificationsEnabled) {
      final batch = _outbox.takePosts(5);
      for (final item in batch) {
        final result = await widget.featureFn(widget.pairing, 'notif-post', item.toJson());
        if (!mounted) return;
        if (result case Err()) {
          _outbox.requeuePosts([item]);
          break;
        }
      }
    }
    final dismissals = _outbox.takeDismissals();
    for (final id in dismissals) {
      final result = await widget.featureFn(widget.pairing, 'notif-dismiss', {'id': id});
      if (!mounted) return;
      if (result case Err()) {
        _outbox.requeueDismissals([id]);
        break;
      }
    }
  }

  Future<void> _reconnect() async {
    if (_reconnecting) return;
    setState(() {
      _reconnecting = true;
      _error = null;
    });
    final facts = await _currentFacts();
    final targets = MacLocator.orderedTargets(widget.pairing.host, _rememberedHosts);
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
    final scheme = Theme.of(context).colorScheme;
    final ok = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: const Text('Unpair Mac?'),
        content: Text('This removes the pairing with \'${widget.pairing.deviceName}\' and generates a new QR on the Mac. You\'ll need to scan again.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(c, false), child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: scheme.error, foregroundColor: scheme.onError),
            onPressed: () => Navigator.pop(c, true),
            child: const Text('Unpair'),
          ),
        ],
      ),
    );
    if (ok == true) await _unpair();
  }

  Future<void> _confirmDisconnect() async {
    final scheme = Theme.of(context).colorScheme;
    final ok = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: const Text('Disconnect?'),
        content: const Text('This clears the live connection. It will reconnect automatically when the Mac is reachable, or tap Reconnect.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(c, false), child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: scheme.error, foregroundColor: scheme.onError),
            onPressed: () => Navigator.pop(c, true),
            child: const Text('Disconnect'),
          ),
        ],
      ),
    );
    if (ok == true) await _unpair();
  }

  Future<void> _sendClipboardNow() async {
    if (_clipSending) return;
    setState(() {
      _clipSending = true;
      _clipInfo = null;
      _clipError = null;
    });
    try {
      String? text;
      try {
        final read = widget.readClipboard ??
            () async {
              final data = await Clipboard.getData('text/plain');
              return data?.text;
            };
        text = await read();
      } catch (_) {}
      if (text == null || text.isEmpty) {
        if (mounted) setState(() => _clipError = 'Clipboard is empty');
        return;
      }
      if (text.length > ClipState.maxLen) {
        if (mounted) setState(() => _clipError = 'Clipboard too large (max 256KB)');
        return;
      }
      final ts = _freshChangedAt();
      final next = _clip.setLocal(text, ts);
      if (next != null && next != _clip && mounted) setState(() => _clip = next);
      final sendAt = _clip.hasText ? _clip.changedAt : ts;
      final res = await widget.featureFn(widget.pairing, 'clip-push', {'text': text, 'changed_at': sendAt, 'origin': 'android'});
      if (!mounted) return;
      if (res case Ok()) {
        if (mounted) {
          setState(() {
            _clip = _clip.clearPending();
            _clipInfo = 'Clipboard sent to Mac';
          });
        }
      } else {
        final msg = (res as Err).failure.message;
        if (mounted) {
          setState(() {
            _clip = _clip.requeue();
            _clipError = 'Send failed: $msg';
          });
        }
      }
    } finally {
      if (mounted) setState(() => _clipSending = false);
    }
  }

  void _onNotificationsChanged(bool enabled) async {
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
  }

  Widget _buildHome(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final isWide = constraints.maxWidth > 700;
        final content = Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            if (_updateMessage != null) ...[
              UpdateBanner(message: _updateMessage!, detail: _updateDetail),
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
            if (_clipInfo != null) ...[
              const SizedBox(height: 8),
              SelectableText.rich(
                TextSpan(text: _clipInfo),
                style: TextStyle(color: Theme.of(context).colorScheme.primary),
              ),
            ],
            if (_clipError != null) ...[
              const SizedBox(height: 8),
              SelectableText.rich(
                TextSpan(text: _clipError),
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ],
            if (_error != null) ...[
              const SizedBox(height: 12),
              ErrorCard(title: 'Ping failed.', detail: _error!),
            ],
            const SizedBox(height: 12),
            EssentialServicesCard(status: _permStatus, permissions: _permissions),
            const SizedBox(height: 8),
            Text(
              'FuseItAll v$kAppVersion (build $kAppBuild)',
              style: Theme.of(context).textTheme.labelSmall?.copyWith(
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
            ),
          ],
        );

        if (isWide) {
          return Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 960),
              child: CustomScrollView(
                slivers: [
                  SliverPadding(
                    padding: const EdgeInsets.all(16),
                    sliver: SliverList.list(
                      children: [
                        Row(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Expanded(child: content),
                            const SizedBox(width: 16),
                            const VerticalDivider(width: 1),
                            const SizedBox(width: 16),
                            Expanded(
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.stretch,
                                children: [
                                  Text(
                                    'Status',
                                    style: Theme.of(context).textTheme.titleSmall?.copyWith(
                                          fontWeight: FontWeight.bold,
                                        ),
                                  ),
                                  const SizedBox(height: 8),
                                  Card(
                                    child: Padding(
                                      padding: const EdgeInsets.all(12),
                                      child: SelectableText(
                                        _isOnline ? 'Online — heartbeats active.' : 'Offline — tap Reconnect.',
                                        style: Theme.of(context).textTheme.bodyMedium,
                                      ),
                                    ),
                                  ),
                                ],
                              ),
                            ),
                          ],
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          );
        }

        return Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 960),
            child: CustomScrollView(
              slivers: [
                SliverPadding(
                  padding: const EdgeInsets.all(16),
                  sliver: SliverList.list(children: [content]),
                ),
              ],
            ),
          ),
        );
      },
    );
  }

  Widget _buildDevices(BuildContext context) => Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 960),
          child: CustomScrollView(
            slivers: [
              SliverPadding(
                padding: const EdgeInsets.all(16),
                sliver: SliverList.list(
                  children: [
                    PairedDevicesCard(
                      deviceName: widget.pairing.deviceName,
                      online: _isOnline,
                      onUnpair: _confirmUnpair,
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
      );

  Widget _buildSettings(BuildContext context) => Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 640),
          child: SettingsContent(
            deviceName: widget.pairing.deviceName,
            settings: _settings,
            onNotificationsChanged: _onNotificationsChanged,
            onUnpair: _confirmUnpair,
          ),
        ),
      );

  @override
  Widget build(BuildContext context) {
    return RootShell(
      home: _buildHome(context),
      devices: _buildDevices(context),
      settings: _buildSettings(context),
    );
  }
}
