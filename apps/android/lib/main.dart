// SPDX-License-Identifier: AGPL-3.0-only

// Reading this as: app root for Scan → Confirm → Paired shell, following HIG 8/14
// with studio-grade light, dark, and auto theme reactivity.

import 'package:flutter/material.dart';

import 'app_theme.dart';
import 'features/pairing/confirm_fingerprint_page.dart';
import 'features/pairing/pair_qr.dart';
import 'features/pairing/pairing_store.dart';
import 'features/pairing/scan_qr_page.dart';
import 'features/ping/ping_page.dart';
import 'features/settings/settings_store.dart';

void main() => runApp(const FuseItAllApp());

class FuseItAllApp extends StatefulWidget {
  const FuseItAllApp({this.store, this.settingsStore, super.key});

  final PairingStore? store;
  final SettingsStore? settingsStore;

  @override
  State<FuseItAllApp> createState() => _FuseItAllAppState();
}

class _FuseItAllAppState extends State<FuseItAllApp> {
  PairQR? _pairing;
  bool _confirmed = false;
  bool _loading = true;
  String? _saveError;
  String? _notice;
  ThemeMode _themeMode = ThemeMode.system;
  late final PairingStore _store;
  late final SettingsStore _settingsStore;

  @override
  void initState() {
    super.initState();
    _store = widget.store ?? PairingStore();
    _settingsStore = widget.settingsStore ?? SettingsStore();
    _restore();
  }

  Future<void> _restore() async {
    PairQR? saved;
    try {
      saved = await _store.load();
    } catch (e) {
      debugPrint('pairing restore failed: $e');
      saved = null;
    }

    try {
      final settings = await _settingsStore.load();
      _themeMode = _parseThemeMode(settings.themeMode);
    } catch (e) {
      debugPrint('settings theme restore failed: $e');
    }

    if (!mounted) return;
    setState(() {
      if (saved != null) {
        _pairing = saved;
        _confirmed = true;
      }
      _loading = false;
    });
  }

  static ThemeMode _parseThemeMode(String mode) {
    switch (mode.toLowerCase()) {
      case 'light':
        return ThemeMode.light;
      case 'dark':
        return ThemeMode.dark;
      default:
        return ThemeMode.system;
    }
  }

  Future<void> _reset([String? notice]) async {
    try {
      await _store.clear();
    } catch (e) {
      debugPrint('pairing wipe failed: $e');
    }
    if (!mounted) return;
    setState(() {
      _pairing = null;
      _confirmed = false;
      _notice = notice;
    });
  }

  Future<void> _confirm(PairQR pairing) async {
    try {
      await _store.save(pairing);
    } catch (e) {
      if (!mounted) return;
      setState(() => _saveError = 'Could not save pairing: $e');
      return;
    }
    if (!mounted) return;
    setState(() {
      _saveError = null;
      _confirmed = true;
    });
  }

  @override
  Widget build(BuildContext context) {
    final pairing = _pairing;
    return MaterialApp(
      title: 'FuseItAll',
      theme: AppTheme.light(),
      darkTheme: AppTheme.dark(),
      themeMode: _themeMode,
      home: _loading
          ? Scaffold(
              body: Center(
                child: Builder(
                  builder: (context) {
                    final noAnim = MediaQuery.disableAnimationsOf(context);
                    if (noAnim) {
                      return Icon(
                        Icons.hourglass_empty,
                        size: 28,
                        color: Theme.of(context).colorScheme.primary,
                      );
                    }
                    return const SizedBox(
                      width: 22,
                      height: 22,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    );
                  },
                ),
              ),
            )
          : pairing == null
              ? ScanQrPage(
                  notice: _notice,
                  onScanned: (qr) => setState(() {
                    _pairing = qr;
                    _notice = null;
                  }),
                )
              : _confirmed
                  ? PingPage(
                      pairing: pairing,
                      settingsStore: _settingsStore,
                      onUnpair: _reset,
                      onRevoked: (msg) => _reset(msg),
                      onThemeModeChanged: (mode) {
                        setState(() => _themeMode = _parseThemeMode(mode));
                      },
                    )
                  : ConfirmFingerprintPage(
                      pairing: pairing,
                      onConfirmed: () => _confirm(pairing),
                      onBack: () => setState(() {
                        _pairing = null;
                        _saveError = null;
                      }),
                      saveError: _saveError,
                    ),
    );
  }
}
