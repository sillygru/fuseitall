// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'dart:async';

import 'package:flutter/material.dart';

import '../../net/go_server.dart';
import '../../net/phone_identity_store.dart';
import '../../result.dart';
import '../device/device_info_provider.dart';
import '../pairing/pair_qr.dart';
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
    this.heartbeatInterval = const Duration(seconds: 20),
    this.identityStore,
    this.deviceFacts,
    super.key,
  });

  final PairQR pairing;
  final VoidCallback onUnpair;

  /// Injectable seams for tests: a fake [PhoneServer] (no .so on host) and
  /// a fake ping sender (no network in widget tests).
  final PhoneServer? phoneServer;
  final Future<Result<Pong>> Function(PairQR pairing,
      {int? replyPort,
      String? replyFingerprint,
      DeviceFacts? facts}) pingFn;

  /// Heartbeat period (20s in prod; shortened in widget tests).
  final Duration heartbeatInterval;

  /// Phone TLS identity store. Defaults to the real secure-storage store;
  /// tests inject a fake. Kept separate from [phoneServer] so widget tests
  /// can use a fake bridge without touching the keychain.
  final PhoneIdentityStore? identityStore;

  /// Self-reported identity (name/model/battery) advertised on every ping.
  /// Defaults to the live plugin-backed provider; tests inject a fake.
  final DeviceFactsProvider? deviceFacts;

  @override
  State<PingPage> createState() => _PingPageState();
}

class _PingPageState extends State<PingPage> {
  final _log = <String>[];
  String? _updateMessage;
  String? _error;
  String? _serverError;
  bool _sending = false;
  late final PhoneServer _server;
  late final DeviceFactsProvider _factsProvider;
  int? _phonePort;
  String? _phoneFingerprint;
  StreamSubscription<String>? _pingSub;
  Timer? _heartbeat;

  @override
  void initState() {
    super.initState();
    _server = widget.phoneServer ?? PhoneServer();
    _factsProvider = widget.deviceFacts ?? LiveDeviceFactsProvider();
    _pingSub = _server.onPing.listen((nonce) {
      if (!mounted) return;
      setState(() => _log.insert(0, 'incoming ping nonce=$nonce'));
    });
    _startPhoneServer();
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
          token: widget.pairing.token, identityStore: store);
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
    final result = await widget.pingFn(widget.pairing,
        replyPort: port,
        replyFingerprint: _phoneFingerprint,
        facts: facts);
    if (!mounted) return;
    setState(() {
      switch (result) {
        case Ok(value: final pong):
          _log.insert(0, 'announced presence nonce=${pong.nonce}');
        case Err(failure: final f):
          _log.insert(0, 'announce failed: ${f.message}');
      }
    });
  }

  @override
  void dispose() {
    _heartbeat?.cancel();
    _heartbeat = null;
    _pingSub?.cancel();
    // Always stop: the page owns the server session (injected fakes in
    // tests included), so no poll timer outlives the page.
    unawaited(_server.stopPhoneServer());
    super.dispose();
  }

  /// 20s heartbeat keeps the Mac's presence fresh and re-advertises the
  /// current reply_port across phone DHCP/port changes. Failures only
  /// append to the log: never the update banner, never the error block.
  void _startHeartbeat() {
    if (!mounted) return;
    _heartbeat?.cancel();
    _heartbeat = Timer.periodic(widget.heartbeatInterval, (_) async {
      final facts = await _currentFacts();
      final result = await widget.pingFn(widget.pairing,
          replyPort: _phonePort,
          replyFingerprint: _phoneFingerprint,
          facts: facts);
      if (!mounted) return;
      setState(() {
        switch (result) {
          case Ok(value: final pong):
            _log.insert(0, 'heartbeat pong nonce=${pong.nonce}');
          case Err(failure: final f):
            _log.insert(0, 'heartbeat failed: ${f.message}');
        }
      });
    });
  }

  Future<void> _sendPing() async {
    setState(() {
      _sending = true;
      _error = null;
    });
    final watch = Stopwatch()..start();
    final facts = await _currentFacts();
    final result = await widget.pingFn(widget.pairing,
        replyPort: _phonePort,
        replyFingerprint: _phoneFingerprint,
        facts: facts);
    watch.stop();
    if (!mounted) return;
    setState(() {
      _sending = false;
      switch (result) {
        case Ok(value: final pong):
          _log.insert(
            0,
            'pong nonce=${pong.nonce} rtt=${watch.elapsedMilliseconds}ms',
          );
        case Err(failure: UpdateRequired(message: final m)):
          _updateMessage = m;
          _log.insert(0, 'update-required rtt=${watch.elapsedMilliseconds}ms');
        case Err(failure: final f):
          _error = f.message;
          _log.insert(0, 'failed: ${f.message}');
      }
    });
  }

  @override
  Widget build(BuildContext context) => Scaffold(
        appBar: AppBar(
          title: Text('Paired: ${widget.pairing.deviceName}'),
          actions: [
            IconButton(
              icon: const Icon(Icons.link_off),
              tooltip: 'Unpair',
              onPressed: widget.onUnpair,
            ),
          ],
        ),
        body: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 960),
            child: LayoutBuilder(
              builder: (context, constraints) {
                if (constraints.maxWidth > 700) {
                  return Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      SizedBox(width: 360, child: _actionColumn(context)),
                      const SizedBox(width: 16),
                      Expanded(child: _logCard(context)),
                    ],
                  );
                }
                return ListView(
                  padding: const EdgeInsets.all(16),
                  children: [
                    _actionColumn(context),
                    const SizedBox(height: 16),
                    _logCard(context),
                  ],
                );
              },
            ),
          ),
        ),
      );

  Widget _actionColumn(BuildContext context) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        mainAxisSize: MainAxisSize.min,
        children: [
          if (_updateMessage != null) ...[
            _updateBanner(_updateMessage!),
            const SizedBox(height: 12),
          ],
          _statusCard(context),
          const SizedBox(height: 12),
          FilledButton.icon(
            onPressed: _sending ? null : _sendPing,
            icon: _sending
                ? const SizedBox(
                    width: 16,
                    height: 16,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.bolt),
            label: Text(_sending ? 'Pinging…' : 'Send ping to Mac'),
          ),
          if (_error != null) ...[
            const SizedBox(height: 12),
            _errorCard(context),
          ],
        ],
      );

  Widget _statusCard(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    final ready = _phonePort != null;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Row(
          children: [
            CircleAvatar(
              backgroundColor: ready
                  ? colorScheme.primaryContainer
                  : colorScheme.surfaceContainerHighest,
              foregroundColor: ready
                  ? colorScheme.onPrimaryContainer
                  : colorScheme.onSurfaceVariant,
              child: Icon(ready ? Icons.dns : Icons.hourglass_empty),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    widget.pairing.deviceName,
                    style: const TextStyle(fontWeight: FontWeight.bold),
                  ),
                  const SizedBox(height: 2),
                  SelectableText(
                    key: const Key('phoneServerStatus'),
                    _phonePort != null
                        ? 'Phone server listening on port $_phonePort'
                        : _serverError != null
                            ? 'Phone server failed to start: $_serverError'
                            : 'Starting phone server…',
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ],
              ),
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

  Widget _logCard(BuildContext context) => Card(
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            mainAxisSize: MainAxisSize.min,
            children: [
              Row(
                children: [
                  const Text('Latency log',
                      style: TextStyle(fontWeight: FontWeight.bold)),
                  const Spacer(),
                  Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 8,
                      vertical: 2,
                    ),
                    decoration: BoxDecoration(
                      color: Theme.of(context)
                          .colorScheme
                          .surfaceContainerHighest,
                      borderRadius: BorderRadius.circular(999),
                    ),
                    child: Text(
                      '${_log.length}',
                      style: Theme.of(context).textTheme.labelSmall,
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 8),
              if (_log.isEmpty)
                const Text(
                    'No pings yet. Send one to measure round-trip time.')
              else
                for (final entry in _log)
                  Container(
                    margin: const EdgeInsets.only(bottom: 6),
                    padding: const EdgeInsets.symmetric(
                      horizontal: 10,
                      vertical: 8,
                    ),
                    decoration: BoxDecoration(
                      color: Theme.of(context)
                          .colorScheme
                          .surfaceContainerHighest
                          .withValues(alpha: 0.5),
                      borderRadius: BorderRadius.circular(10),
                    ),
                    child: SelectableText(entry,
                        style:
                            const TextStyle(fontFamily: 'monospace')),
                  ),
            ],
          ),
        ),
      );

  Widget _updateBanner(String message) {
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
            TextSpan(text: message),
          ],
        ),
      ),
    );
  }
}
