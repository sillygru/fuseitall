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
import '../clipboard/clipboard_watcher.dart';
import '../files/file_sync.dart';
import '../files/file_system.dart';
import 'dart:io' show Directory, File;
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
import '../../net/phone_transport.dart';
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
  StreamSubscription<dynamic>? _clipWatcherSub;
  StreamSubscription<dynamic>? _notifSub;
  Timer? _heartbeat;
  Timer? _clipDebounce;
  String _clipPendingText = '';
  DateTime? _ignoreClipUntil;
  AppSettings? _settings;
  bool _settingsDirty = false;
  var _clip = const ClipState();
  final _outbox = NotifOutbox();
  late final Permissions _permissions;
  late final MacLocator _locator;
  PhoneTransport get _transport => PhoneTransport(
        base: widget.pairing,
        locator: _locator,
        pingFn: widget.pingFn,
        featureFn: widget.featureFn,
      );
  PermissionStatus? _permStatus;
  List<String> _rememberedHosts = const [];
  bool _connected = false;
  bool _reconnecting = false;
  bool _clipSending = false;
  DateTime? _lastSuccessAt;
  int _consecutiveFailures = 0;
  int _serverRetries = 0;
  Timer? _serverRetryTimer;
  static const int _maxServerRetries = 3;
  Timer? _notifFlushTimer;
  final _sentIconPackages = <String>{};
  bool _flushing = false;
  FileSync? _fileSync;
  late FileSystem _fileSystem;
  bool _fsExternal = false;

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
    // File system: external storage when All Files Access granted, else private fallback.
    // Injected synchronously for tests; async external-root resolution happens after.
    _fileSystem = AppFileSystem(Directory.systemTemp.createTempSync('fuse-files-').path);
    _fsExternal = false;
    _fileSync = _buildFileSync(_fileSystem);
    _initFileSystem();
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
    _startClipboardWatcher();
    _startNotifWatcher();
    _startPhoneServer();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      debugPrint('app resumed — refreshing permissions & presence');
      unawaited(_refreshPermissions());
      unawaited(_refreshFileSystemIfNeeded());
      _startClipboardWatcher();
      _startNotifWatcher();
      unawaited(_drainToOutbox().then((_) {
        if (_outbox.posts.isNotEmpty || _outbox.dismissals.isNotEmpty) {
          _scheduleNotifImmediate();
        }
      }));
      final stale = _lastSuccessAt == null || DateTime.now().difference(_lastSuccessAt!).inSeconds > 10;
      if (stale) {
        if (_phonePort != null) {
          unawaited(_announcePresence());
        } else if (_serverError != null && _serverRetries >= _maxServerRetries) {
          // Degraded but stale: give the listener one more chance, then
          // continue announcing without it.
          unawaited(_retryPhoneServer());
        } else if (_phonePort == null) {
          unawaited(_announcePresence());
        }
      }
    }
  }

  FileSync _buildFileSync(FileSystem fs) => FileSync(
        fs: fs,
        sendFeature: (type, payload) async {
          final res = await _transport.sendFeatureWithFallback(type, payload);
          if (res.result is Err) throw Exception((res.result as Err).failure.message);
        },
      );

  Future<void> _rebindFileSystem(String root) async {
    final dir = Directory(root);
    if (!await dir.exists()) return;
    final fs = AppFileSystem(root);
    final sync = _buildFileSync(fs);
    if (mounted) {
      setState(() {
        _fileSystem = fs;
        _fileSync = sync;
        _fsExternal = true;
      });
    } else {
      _fileSystem = fs;
      _fileSync = sync;
      _fsExternal = true;
    }
    debugPrint('file system rooted at external: $root');
  }

  Future<void> _rebindToFallback() async {
    final tmp = Directory.systemTemp.createTempSync('fuse-files-').path;
    final fs = AppFileSystem(tmp);
    final sync = _buildFileSync(fs);
    if (mounted) {
      setState(() {
        _fileSystem = fs;
        _fileSync = sync;
        _fsExternal = false;
      });
    } else {
      _fileSystem = fs;
      _fileSync = sync;
      _fsExternal = false;
    }
    debugPrint('file system using private fallback (all-files not granted)');
  }

  String _canonicalPath(String p) {
    try {
      final f = File(p);
      if (f.existsSync()) return f.resolveSymbolicLinksSync();
    } catch (_) {}
    try {
      final d = Directory(p);
      if (d.existsSync()) return d.resolveSymbolicLinksSync();
    } catch (_) {}
    try {
      return File(p).absolute.path;
    } catch (_) {
      return p;
    }
  }

  Future<void> _initFileSystem() async {
    try {
      final granted = await _permissions.isAllFilesAccessGranted();
      if (granted) {
        final ext = await _permissions.getExternalRoot();
        final root = (ext != null && ext.isNotEmpty) ? ext : '/storage/emulated/0';
        final dir = Directory(root);
        if (await dir.exists()) {
          await _rebindFileSystem(root);
          return;
        }
      }
      await _rebindToFallback();
    } catch (e) {
      debugPrint('init file system failed: $e');
    }
  }

  Future<void> _refreshFileSystemIfNeeded() async {
    try {
      final granted = await _permissions.isAllFilesAccessGranted();
      if (granted && !_fsExternal) {
        await _initFileSystem();
        return;
      }
      if (!granted && _fsExternal) {
        await _rebindToFallback();
        return;
      }
      if (granted && _fsExternal) {
        // External root may be symlink-resolved differently (e.g. /sdcard -> /storage/emulated/0)
        final ext = await _permissions.getExternalRoot();
        final expected = (ext != null && ext.isNotEmpty) ? ext : '/storage/emulated/0';
        final current = _fileSystem is AppFileSystem ? (_fileSystem as AppFileSystem).rootPath : '';
        if (_canonicalPath(current) != _canonicalPath(expected)) {
          await _rebindFileSystem(expected);
        }
      }
    } catch (_) {}
  }

  Future<void> _loadSettings() async {
    final stored = await _settingsStore.load();
    if (!mounted) return;
    setState(() => _settings = stored);
  }

  // Pending auto image staging (separate from text to avoid mixing).
  String _clipPendingB64 = '';
  String _clipPendingMime = '';
  String _clipPendingFilename = '';

  void _startClipboardWatcher() {
    try {
      final watcher = widget.clipWatcher is ClipboardWatcher
          ? widget.clipWatcher as ClipboardWatcher
          : ClipboardWatcher();
      _clipWatcherSub?.cancel();
      // Listen to full clipChanges (String text or Map image) for image support,
      // fallback to legacy string stream if needed.
      try {
        _clipWatcherSub = watcher.clipChanges.listen((event) {
          if (event is Map) {
            final kind = '${event['kind']}';
            if (kind == 'image') {
              _onClipboardWatcherImage('${event['image_b64'] ?? ''}', '${event['mime'] ?? ''}', '${event['filename'] ?? ''}');
              return;
            }
          }
          if (event is String) _onClipboardWatcherText(event);
        }, onError: (_) {});
      } catch (_) {
        _clipWatcherSub = watcher.changes.listen((text) {
          _onClipboardWatcherText(text);
        }, onError: (_) {});
      }
    } catch (_) {}
  }

  void _startNotifWatcher() {
    try {
      _notifSub?.cancel();
      _notifSub = _notifListener.notifEvents.listen((event) {
        final (:post, :removal) = NotifListener.parseEvent(event);
        if (removal != null) {
          _outbox.queueDismiss(removal);
          _scheduleNotifImmediate();
        } else if (post != null) {
          _outbox.queuePost(post);
          _scheduleNotifImmediate();
        }
      }, onError: (_) {});
    } catch (_) {}
  }

  void _onClipboardWatcherText(String text) {
    final trimmed = text.trim();
    if (trimmed.isEmpty) return;
    if (trimmed.length > ClipState.maxLen) return;
    if (_ignoreClipUntil != null && DateTime.now().isBefore(_ignoreClipUntil!) && trimmed == _clip.text) return;
    final settings = _settings;
    if (settings != null) {
      if (!AppSettings.allowsSend(settings.clipboardMode, 'android')) return;
    }
    // Queue even when offline — heartbeat will flush pending.
    _clipPendingText = trimmed;
    _clipPendingB64 = '';
    _clipPendingMime = '';
    _clipPendingFilename = '';
    _clipDebounce?.cancel();
    _clipDebounce = Timer(const Duration(milliseconds: 350), () {
      _flushClipboardAuto();
    });
  }

  void _onClipboardWatcherImage(String b64, String mime, [String filename = '']) {
    final trimmed = b64.trim();
    if (trimmed.isEmpty) return;
    if (!ClipState.validImage(trimmed, mime)) return;
    // Auto throttle: large images need manual Send.
    try {
      final raw = base64Decode(trimmed);
      if (raw.length > ClipState.autoImageRaw) return;
    } catch (_) {
      return;
    }
    if (_ignoreClipUntil != null && DateTime.now().isBefore(_ignoreClipUntil!) && trimmed == _clip.imageB64) return;
    final settings = _settings;
    if (settings != null) {
      if (!AppSettings.allowsSend(settings.clipboardMode, 'android')) return;
    }
    _clipPendingB64 = trimmed;
    _clipPendingMime = ClipState.normalizeMime(mime) ?? mime;
    _clipPendingFilename = ClipState.sanitizeFilename(filename) ?? '';
    _clipPendingText = '';
    _clipDebounce?.cancel();
    _clipDebounce = Timer(const Duration(milliseconds: 350), () {
      _flushClipboardAuto();
    });
  }

  Future<void> _flushClipboardAuto() async {
    // Image path
    if (_clipPendingB64.isNotEmpty) {
      final b64 = _clipPendingB64;
      final mime = _clipPendingMime;
      final filename = _clipPendingFilename;
      _clipPendingB64 = '';
      _clipPendingMime = '';
      _clipPendingFilename = '';
      _clipPendingText = '';
      final settings = _settings;
      if (settings != null && !AppSettings.allowsSend(settings.clipboardMode, 'android')) return;
      if (!ClipState.validImage(b64, mime)) return;
      final ts = _freshChangedAt();
      final next = _clip.setLocalImageWithFilename(b64, mime, filename, ts);
      if (next == null || identical(next, _clip)) return;
      if (mounted) setState(() => _clip = next);
      if (!_isOnline) {
        debugPrint('clipboard watcher queued offline image, pending for heartbeat');
        return;
      }
      final pending = _clip.takePending();
      if (pending == null) return;
      final (:result, :winner) = await _transport.sendFeatureWithFallback('clip-push', pending);
      final res = result;
      if (!mounted) return;
      if (res case Ok()) {
        debugPrint('clip-push image ok via ${winner ?? 'unknown'}');
        setState(() => _clip = _clip.clearPending());
      } else if (res case Err(failure: UpdateRequired(message: final m, requiredVersion: final req, currentVersion: final cur))) {
        setState(() {
          _updateMessage = m;
          _updateDetail = req.isNotEmpty || cur.isNotEmpty
              ? 'Requires ${req.isNotEmpty ? req : 'newer'}${cur.isNotEmpty ? ', current $cur' : ''} (this device v$kAppVersion)'
              : null;
          _clip = _clip.requeue();
        });
      } else if (res case Err(failure: AuthFailure())) {
        setState(() => _clip = _clip.requeue());
        unawaited(_revokedByMac());
      } else {
        debugPrint('clip-push image failed: ${(res as Err).failure.message}');
        setState(() => _clip = _clip.requeue());
      }
      return;
    }
    final text = _clipPendingText;
    _clipPendingText = '';
    if (text.isEmpty) return;
    final settings = _settings;
    if (settings != null && !AppSettings.allowsSend(settings.clipboardMode, 'android')) return;
    if (text.length > ClipState.maxLen) return;
    final ts = _freshChangedAt();
    final next = _clip.setLocal(text, ts);
    if (next == null || identical(next, _clip)) return;
    if (mounted) setState(() => _clip = next);
    if (!_isOnline) {
      debugPrint('clipboard watcher queued offline, pending for heartbeat');
      return;
    }
    final pending = _clip.takePending();
    if (pending == null) return;
    final (:result, winner: w2) = await _transport.sendFeatureWithFallback('clip-push', pending);
    final res2 = result;
    if (!mounted) return;
    if (res2 case Ok()) {
      debugPrint('clip-push text ok via ${w2 ?? 'unknown'}');
      setState(() => _clip = _clip.clearPending());
    } else if (res2 case Err(failure: UpdateRequired(message: final m, requiredVersion: final req, currentVersion: final cur))) {
      setState(() {
        _updateMessage = m;
        _updateDetail = req.isNotEmpty || cur.isNotEmpty
            ? 'Requires ${req.isNotEmpty ? req : 'newer'}${cur.isNotEmpty ? ', current $cur' : ''} (this device v$kAppVersion)'
            : null;
        _clip = _clip.requeue();
      });
    } else if (res2 case Err(failure: AuthFailure())) {
      setState(() => _clip = _clip.requeue());
      unawaited(_revokedByMac());
    } else {
      debugPrint('clip-push text failed: ${(res2 as Err).failure.message}');
      setState(() => _clip = _clip.requeue());
    }
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
      await _transport.sendFeatureWithFallback('unpair', <String, Object?>{});
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

  Future<void> _writeClipboardImage(String b64, String mime, [String filename = '']) async {
    try {
      const ch = MethodChannel('fuseitall/clipboard');
      final args = <String, dynamic>{'image_b64': b64, 'mime': mime};
      if (filename.isNotEmpty) args['filename'] = filename;
      await ch.invokeMethod('writeImage', args);
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
    // Files first: file Sync handles its own types before other switches.
    final fileHandled = await _fileSync?.handleEvent(decoded) ?? false;
    if (fileHandled) return;
    final settings = _settings;
    switch (type) {
      case 'clip-push':
        final ca = payload['changed_at'];
        final nextChangedAt = ca is num ? ca.toInt() : 0;
        final incomingOrigin = '${payload['origin']}';
        final allow = settings == null || AppSettings.allowsReceive(settings.clipboardMode, incomingOrigin);
        if (!allow) return;
        final kind = '${payload['kind'] ?? 'text'}';
        if (kind == 'image') {
          final next = _clip.applyRemote(
            next: '',
            nextChangedAt: nextChangedAt,
            nextOrigin: incomingOrigin,
            nextKind: 'image',
            nextMime: '${payload['mime'] ?? ''}',
            nextImageB64: '${payload['image_b64'] ?? ''}',
            nextFilename: '${payload['filename'] ?? ''}',
          );
          if (next == null || !mounted) return;
          setState(() => _clip = next);
          _ignoreClipUntil = DateTime.now().add(const Duration(milliseconds: 800));
          try {
            await _writeClipboardImage(next.imageB64, next.mime, next.filename);
          } catch (_) {}
        } else {
          final next = _clip.applyRemote(
            next: '${payload['text'] ?? ''}',
            nextChangedAt: nextChangedAt,
            nextOrigin: incomingOrigin,
          );
          if (next == null || !mounted) return;
          setState(() => _clip = next);
          _ignoreClipUntil = DateTime.now().add(const Duration(milliseconds: 800));
          try {
            await _writeClipboardText(next.text);
          } catch (_) {}
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
    // Preemptive clear: hot-restart kills Dart isolate but keeps the
    // Android process + libfuseitall.so globals (httpServer != nil).
    // Without this, a fresh isolate's first start always hits
    // "already started" and falls into backoff. Stop is idempotent
    // (returns 1 when idle), so cold starts pay ~0.
    if (_phonePort == null && _serverRetries == 0) {
      try {
        FfiBridgeHandle.load().stop();
      } catch (_) {}
    }
    try {
      final store = widget.identityStore ?? PhoneIdentityStore();
      final port = await _server.startPhoneServer(
        token: widget.pairing.token,
        identityStore: store,
      );
      if (!mounted) return;
      _serverRetryTimer?.cancel();
      _serverRetryTimer = null;
      setState(() {
        _phonePort = port;
        _phoneFingerprint = _server.fingerprint;
        _serverError = null;
        _serverRetries = 0;
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
      final msg = '$e';
      final isAlreadyStarted = msg.contains('already started');
      // Stale-global fast path must work even with old .so where detail
      // is missing (generic "failed to start"). On first failure, always
      // try one immediate clear+retry before falling to backoff/degraded.
      if (_serverRetries == 0) {
        final shouldForceClear = isAlreadyStarted || !msg.contains(':');
        if (shouldForceClear) {
          try {
            FfiBridgeHandle.load().stop();
            debugPrint('forced Go server stop for stale global (retry 1)');
          } catch (_) {}
          if (isAlreadyStarted) {
            debugPrint('retrying phone server start after stale-global clear');
          } else {
            debugPrint('retrying phone server start after preemptive clear (generic failure)');
          }
          _serverRetries = 1;
          Future.microtask(() {
            if (!mounted || _phonePort != null) return;
            unawaited(_startPhoneServer());
          });
          setState(() => _serverError = msg);
          return;
        }
      } else if (isAlreadyStarted) {
        // Rare: second "already started" after a clear — clear again once.
        try {
          FfiBridgeHandle.load().stop();
          debugPrint('forced Go server stop for stale global (retry $_serverRetries)');
        } catch (_) {}
      }
      debugPrint('phone server start failed: $e');
      setState(() => _serverError = msg);
      _scheduleServerRetry();
    }
  }

  void _scheduleServerRetry() {
    if (_serverRetries >= _maxServerRetries) {
      debugPrint('phone server retries exhausted ($_serverRetries/$_maxServerRetries) — degraded mode, heartbeat without reply_port');
      // Degraded: phone→Mac still works; Mac→phone pushes are unavailable
      // until the user retries. Start the heartbeat so presence heals even
      // without the listener.
      _startHeartbeat();
      unawaited(_announcePresence());
      return;
    }
    _serverRetries++;
    // Exponential backoff: 2s, 4s, 8s (+ jitter via micro-work).
    final delay = Duration(seconds: 1 << _serverRetries);
    debugPrint('scheduling phone server retry $_serverRetries/$_maxServerRetries in ${delay.inSeconds}s');
    _serverRetryTimer?.cancel();
    _serverRetryTimer = Timer(delay, () {
      if (!mounted || _phonePort != null) return;
      debugPrint('retrying phone server start ($_serverRetries/$_maxServerRetries)');
      unawaited(_startPhoneServer());
    });
  }

  Future<void> _retryPhoneServer() async {
    _serverRetryTimer?.cancel();
    _serverRetryTimer = null;
    _serverRetries = 0;
    setState(() => _serverError = null);
    try {
      FfiBridgeHandle.load().stop();
    } catch (_) {}
    await _startPhoneServer();
  }

  Future<void> _announcePresence() async {
    final port = _phonePort;
    final fp = _phoneFingerprint;
    final facts = await _currentFacts();
    final (:result, :winner) = await _transport.pingWithFallback(
      replyPort: port,
      replyFingerprint: fp,
      facts: facts,
      rememberedHosts: _rememberedHosts,
    );
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
    debugPrint(result is Ok
        ? 'announced presence ok via ${winner ?? widget.pairing.host}${port == null ? ' (no reply_port)' : ''}'
        : 'announce failed: ${(result as Err).failure.message}');
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
    _clipDebounce?.cancel();
    _clipDebounce = null;
    _notifFlushTimer?.cancel();
    _notifFlushTimer = null;
    _serverRetryTimer?.cancel();
    _serverRetryTimer = null;
    _pingSub?.cancel();
    _featSub?.cancel();
    _clipWatcherSub?.cancel();
    _notifSub?.cancel();
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
      final (:result, :winner) = await _transport.pingWithFallback(
        replyPort: port,
        replyFingerprint: fp,
        facts: facts,
        rememberedHosts: _rememberedHosts,
      );
      if (!mounted) return;
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
    if (_flushing) return;
    _flushing = true;
    try {
      await _drainToOutbox();
      // Opportunistic fast path: try to deliver immediately even when
      // _isOnline is false — transports will attempt fallback and requeue
      // on failure. Heartbeat remains retry. No battery exemption needed.
      if (_outbox.posts.isNotEmpty || _outbox.dismissals.isNotEmpty) {
        _scheduleNotifImmediate();
      }
      await _flushSettings();
      await _flushNotifs();
      await _flushDismissals();
      await _flushClip();
    } finally {
      _flushing = false;
    }
  }

  Future<void> _drainToOutbox() async {
    final drained = await _notifListener.drain();
    for (final item in drained.posts) {
      _outbox.queuePost(item);
    }
    for (final id in drained.removals) {
      _outbox.queueDismiss(id);
    }
    if (drained.posts.isNotEmpty || drained.removals.isNotEmpty) {
      debugPrint(
          'notif drain posts=${drained.posts.length} removals=${drained.removals.length} outbox posts=${_outbox.posts.length} dismissals=${_outbox.dismissals.length}');
    }
  }

  Future<void> _flushSettings() async {
    final settings = _settings;
    if (settings == null || !_settingsDirty) return;
    final (:result, :winner) =
        await _transport.sendFeatureWithFallback('settings-sync', settings.toJson());
    if (!mounted) return;
    if (result case Ok()) {
      setState(() => _settingsDirty = false);
      debugPrint('settings-sync ok via ${winner ?? 'unknown'}');
    } else if (result
        case Err(failure: UpdateRequired(message: final m, requiredVersion: final req, currentVersion: final cur))) {
      setState(() {
        _updateMessage = m;
        _updateDetail = req.isNotEmpty || cur.isNotEmpty
            ? 'Requires ${req.isNotEmpty ? req : 'newer'}${cur.isNotEmpty ? ', current $cur' : ''} (this device v$kAppVersion)'
            : null;
      });
      debugPrint('settings-sync update required: $m');
    } else if (result case Err(failure: AuthFailure(message: final m))) {
      debugPrint('settings-sync auth failure: $m');
      unawaited(_revokedByMac());
    } else {
      debugPrint('settings-sync failed: ${(result as Err).failure.message}');
    }
  }

  Future<void> _flushNotifs() async {
    final settings = _settings;
    final allowNotifs = settings == null || settings.notificationsEnabled;
    if (!allowNotifs) {
      if (_outbox.posts.isNotEmpty) debugPrint('notif flush skipped: notifications disabled');
      return;
    }
    final batch = _outbox.takePosts(10);
    for (final item in batch) {
      final payload = _payloadWithIconDedupe(item);
      final (:result, :winner) = await _transport.sendFeatureWithFallback('notif-post', payload);
      if (!mounted) return;
      if (result case Ok()) {
        if (item.packageName.isNotEmpty && item.iconB64.isNotEmpty) {
          _sentIconPackages.add(item.packageName);
        }
        debugPrint('notif-post ok id=${item.id} via ${winner ?? 'unknown'}');
      } else if (result
          case Err(failure: UpdateRequired(message: final m, requiredVersion: final req, currentVersion: final cur))) {
        setState(() {
          _updateMessage = m;
          _updateDetail = req.isNotEmpty || cur.isNotEmpty
              ? 'Requires ${req.isNotEmpty ? req : 'newer'}${cur.isNotEmpty ? ', current $cur' : ''} (this device v$kAppVersion)'
              : null;
        });
        debugPrint('notif-post update required id=${item.id}: $m');
        break;
      } else if (result case Err(failure: AuthFailure(message: final m))) {
        debugPrint('notif-post auth failure id=${item.id}: $m');
        _outbox.requeuePosts([item]);
        unawaited(_revokedByMac());
        break;
      } else {
        debugPrint('notif-post failed id=${item.id}: ${(result as Err).failure.message} queue=${_outbox.posts.length + 1}');
        _outbox.requeuePosts([item]);
        break;
      }
    }
  }

  Future<void> _flushDismissals() async {
    final dismissals = _outbox.takeDismissals();
    for (final id in dismissals) {
      final (:result, :winner) =
          await _transport.sendFeatureWithFallback('notif-dismiss', {'id': id});
      if (!mounted) return;
      if (result case Ok()) {
        debugPrint('notif-dismiss ok id=$id via ${winner ?? 'unknown'}');
      } else if (result case Err(failure: UpdateRequired(message: final m))) {
        debugPrint('notif-dismiss update required id=$id: $m');
        break;
      } else if (result case Err(failure: AuthFailure(message: final m))) {
        debugPrint('notif-dismiss auth failure id=$id: $m');
        _outbox.requeueDismissals([id]);
        unawaited(_revokedByMac());
        break;
      } else {
        debugPrint('notif-dismiss failed id=$id: ${(result as Err).failure.message}');
        _outbox.requeueDismissals([id]);
        break;
      }
    }
  }

  Future<void> _flushClip() async {
    final settings = _settings;
    final pendingClip = _clip.takePending();
    if (pendingClip == null) return;
    final modeAllows = settings == null || AppSettings.allowsSend(settings.clipboardMode, 'android');
    if (!modeAllows) {
      if (mounted) setState(() => _clip = _clip.clearPending());
      return;
    }
    final (:result, winner: _) = await _transport.sendFeatureWithFallback('clip-push', pendingClip);
    final res = result;
    if (!mounted) return;
    if (res case Ok()) {
      setState(() => _clip = _clip.clearPending());
    } else if (res
        case Err(failure: UpdateRequired(message: final m, requiredVersion: final req, currentVersion: final cur))) {
      setState(() {
        _updateMessage = m;
        _updateDetail = req.isNotEmpty || cur.isNotEmpty
            ? 'Requires ${req.isNotEmpty ? req : 'newer'}${cur.isNotEmpty ? ', current $cur' : ''} (this device v$kAppVersion)'
            : null;
      });
      setState(() => _clip = _clip.requeue());
    } else if (res case Err(failure: AuthFailure())) {
      setState(() => _clip = _clip.requeue());
      unawaited(_revokedByMac());
    } else {
      setState(() => _clip = _clip.requeue());
    }
  }

  Map<String, dynamic> _payloadWithIconDedupe(NotifItem item) {
    final base = item.toJson();
    if (item.packageName.isNotEmpty && item.iconB64.isNotEmpty && _sentIconPackages.contains(item.packageName)) {
      base.remove('app_icon_b64');
    }
    return base;
  }

  void _scheduleNotifImmediate() {
    _notifFlushTimer?.cancel();
    _notifFlushTimer = Timer(const Duration(milliseconds: 150), () async {
      if (!mounted || _flushing) return;
      if (_outbox.posts.isEmpty && _outbox.dismissals.isEmpty) return;
      final settings = _settings;
      if (settings != null && !settings.notificationsEnabled) {
        debugPrint('notif immediate skipped: disabled');
        return;
      }
      final batch = _outbox.takePosts(10);
      if (batch.isEmpty) return;
      for (final item in batch) {
        final payload = _payloadWithIconDedupe(item);
        final (:result, :winner) =
            await _transport.sendFeatureWithFallback('notif-post', payload);
        if (!mounted) return;
        if (result case Ok()) {
          if (item.packageName.isNotEmpty && item.iconB64.isNotEmpty) {
            _sentIconPackages.add(item.packageName);
          }
          debugPrint('notif immediate ok id=${item.id} via ${winner ?? 'unknown'}');
        } else if (result case Err(failure: UpdateRequired(message: final m))) {
          debugPrint('notif immediate update required id=${item.id}: $m');
          break;
        } else if (result case Err(failure: AuthFailure(message: final m))) {
          debugPrint('notif immediate auth failure id=${item.id}: $m');
          _outbox.requeuePosts([item]);
          unawaited(_revokedByMac());
          break;
        } else {
          debugPrint('notif immediate failed id=${item.id}: ${(result as Err).failure.message}');
          _outbox.requeuePosts([item]);
          break;
        }
      }
      // Dismissals also flush fast (fire-and-forget).
      final dismissals = _outbox.takeDismissals();
      for (final id in dismissals) {
        final (:result, :winner) =
            await _transport.sendFeatureWithFallback('notif-dismiss', {'id': id});
        if (!mounted) return;
        if (result case Ok()) {
          debugPrint('notif-dismiss immediate ok id=$id via ${winner ?? 'unknown'}');
        } else {
          _outbox.requeueDismissals([id]);
          break;
        }
      }
    });
  }

  Future<void> _reconnect() async {
    if (_reconnecting) return;
    setState(() {
      _reconnecting = true;
      _error = null;
    });
    final facts = await _currentFacts();
    Result<Pong>? firstOk;
    String? winner;
    var revoked = false;
    // Ordered fallback via canonical transport (primary + remembered).
    final ordered = await _transport.pingWithFallback(
      replyPort: _phonePort,
      replyFingerprint: _phoneFingerprint,
      facts: facts,
      rememberedHosts: _rememberedHosts,
    );
    if (!mounted) return;
    switch (ordered.result) {
      case Ok():
        firstOk = ordered.result;
        winner = ordered.winner;
        _lastSuccessAt = DateTime.now();
        if (mounted) setState(() => _connected = true);
        debugPrint('reconnected via ${winner ?? widget.pairing.host}');
      case Err(failure: AuthFailure()):
        revoked = true;
      case Err():
        // fall through to sweep
        break;
    }
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
      debugPrint('reconnect failed: no host accepted');
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
      // Try image first via MethodChannel readImage, fallback to text.
      String? b64;
      String mime = 'image/png';
      String filename = '';
      try {
        const ch = MethodChannel('fuseitall/clipboard');
        final res = await ch.invokeMethod('readImage');
        if (res is Map) {
          b64 = res['image_b64'] as String?;
          mime = (res['mime'] as String?) ?? 'image/png';
          filename = (res['filename'] as String?) ?? '';
        }
      } catch (_) {}
      if (b64 != null && b64.isNotEmpty) {
        if (!ClipState.validImage(b64, mime)) {
          if (mounted) setState(() => _clipError = 'Clipboard image too large or invalid (max 5 MiB)');
          return;
        }
        final ts = _freshChangedAt();
        final next = _clip.setLocalImageWithFilename(b64, mime, filename, ts);
        if (next != null && next != _clip && mounted) setState(() => _clip = next);
        final pending = _clip.takePending();
        if (pending == null) {
          if (mounted) setState(() => _clipError = 'Clipboard image pending empty');
          return;
        }
        final (:result, winner: wImg) = await _transport.sendFeatureWithFallback('clip-push', pending);
        final res = result;
        if (!mounted) return;
        if (res case Ok()) {
          debugPrint('clip-push image manual ok via ${wImg ?? 'unknown'}');
          if (mounted) {
            setState(() {
              _clip = _clip.clearPending();
              _clipInfo = 'Clipboard image sent to Mac';
            });
          }
        } else {
          final msg = (res as Err).failure.message;
          if (res case Err(failure: UpdateRequired())) {
            if (mounted) {
              setState(() {
                _updateMessage = (res as Err).failure.message;
                _clip = _clip.requeue();
                _clipError = 'Send failed: $msg';
              });
            }
          } else if (res case Err(failure: AuthFailure())) {
            if (mounted) {
              setState(() {
                _clip = _clip.requeue();
                _clipError = 'Send failed: $msg';
              });
            }
            unawaited(_revokedByMac());
          } else {
            if (mounted) {
              setState(() {
                _clip = _clip.requeue();
                _clipError = 'Send failed: $msg';
              });
            }
          }
        }
        return;
      }
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
      final pending = _clip.takePending();
      if (pending == null) {
        if (mounted) setState(() => _clipError = 'Clipboard pending empty');
        return;
      }
      final (:result, winner: wTxt) = await _transport.sendFeatureWithFallback('clip-push', pending);
      final res = result;
      if (!mounted) return;
      if (res case Ok()) {
        debugPrint('clip-push text manual ok via ${wTxt ?? 'unknown'}');
        if (mounted) {
          setState(() {
            _clip = _clip.clearPending();
            _clipInfo = 'Clipboard sent to Mac';
          });
        }
      } else {
        final msg = (res as Err).failure.message;
        if (res case Err(failure: UpdateRequired())) {
          if (mounted) {
            setState(() {
              _updateMessage = (res as Err).failure.message;
              _clip = _clip.requeue();
              _clipError = 'Send failed: $msg';
            });
          }
        } else if (res case Err(failure: AuthFailure())) {
          if (mounted) {
            setState(() {
              _clip = _clip.requeue();
              _clipError = 'Send failed: $msg';
            });
          }
          unawaited(_revokedByMac());
        } else {
          if (mounted) {
            setState(() {
              _clip = _clip.requeue();
              _clipError = 'Send failed: $msg';
            });
          }
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

  void _onClipboardModeChanged(String mode) async {
    final cur = _settings ?? AppSettings.defaults(nowUnix: _nowUnix());
    final next = cur.withClipboardMode(mode, nowUnix: _nowUnix());
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
            if (_serverError != null) ...[
              const SizedBox(height: 12),
              _serverDegradedCard(context),
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
            onClipboardModeChanged: _onClipboardModeChanged,
            onUnpair: _confirmUnpair,
          ),
        ),
      );

  Widget _serverDegradedCard(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final isRetrying = _serverRetryTimer != null && _serverRetryTimer!.isActive;
    final detail = _serverError ?? 'Phone listener failed to start.';
    // Show the real Go error (bind, already started, missing .so) verbatim;
    // it never contains secrets (port/bind state only).
    return Card(
      color: scheme.surfaceContainerHighest,
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            SelectableText.rich(
              TextSpan(
                style: TextStyle(color: scheme.onSurface),
                children: [
                  const TextSpan(
                    text: 'Phone listener unavailable\n',
                    style: TextStyle(fontWeight: FontWeight.bold),
                  ),
                  TextSpan(
                    text: 'Phone → Mac still works. Mac → phone clipboard and settings will resume after the listener restarts.\n',
                  ),
                  TextSpan(
                    text: detail,
                    style: TextStyle(color: scheme.onSurfaceVariant, fontSize: 12),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 8),
            Align(
              alignment: Alignment.centerLeft,
              child: FilledButton(
                key: const Key('retryPhoneServer'),
                onPressed: isRetrying ? null : _retryPhoneServer,
                child: Text(isRetrying ? 'Retrying…' : 'Retry listener'),
              ),
            ),
          ],
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return RootShell(
      home: _buildHome(context),
      devices: _buildDevices(context),
      settings: _buildSettings(context),
    );
  }
}
