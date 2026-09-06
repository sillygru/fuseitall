// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter/material.dart';

import 'features/pairing/confirm_fingerprint_page.dart';
import 'features/pairing/pair_qr.dart';
import 'features/pairing/pairing_store.dart';
import 'features/pairing/scan_qr_page.dart';
import 'features/ping/ping_page.dart';

// Three screens, no router package: Scan -> Confirm code -> Paired.
// A stored pairing skips straight to Paired on launch.
void main() => runApp(const FuseItAllApp());

class FuseItAllApp extends StatefulWidget {
  const FuseItAllApp({this.store, super.key});

  /// Injectable seam for tests: fake secure storage.
  final PairingStore? store;

  @override
  State<FuseItAllApp> createState() => _FuseItAllAppState();
}

class _FuseItAllAppState extends State<FuseItAllApp> {
  PairQR? _pairing;
  bool _confirmed = false;
  bool _loading = true;
  String? _saveError;
  late final PairingStore _store;

  @override
  void initState() {
    super.initState();
    _store = widget.store ?? PairingStore();
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
    if (!mounted) return;
    setState(() {
      if (saved != null) {
        _pairing = saved;
        _confirmed = true;
      }
      _loading = false;
    });
  }

  Future<void> _reset() async {
    try {
      await _store.clear();
    } catch (e) {
      debugPrint('pairing wipe failed: $e');
    }
    if (!mounted) return;
    setState(() {
      _pairing = null;
      _confirmed = false;
    });
  }

  Future<void> _confirm(PairQR pairing) async {
    try {
      await _store.save(pairing);
    } catch (e) {
      // Stay unconfirmed: advancing would silently lose the pairing on
      // next launch. The confirm screen surfaces this verbatim.
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
    final seed = ColorScheme.fromSeed(seedColor: Colors.teal);
    final darkSeed = ColorScheme.fromSeed(
      seedColor: Colors.teal,
      brightness: Brightness.dark,
    );
    return MaterialApp(
      title: 'FuseItAll',
      theme: ThemeData(
        colorScheme: seed,
        useMaterial3: true,
        appBarTheme: AppBarTheme(
          centerTitle: false,
          backgroundColor: seed.surface,
          foregroundColor: seed.onSurface,
        ),
        cardTheme: CardThemeData(
          elevation: 1,
          margin: EdgeInsets.zero,
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(20),
          ),
        ),
        inputDecorationTheme: const InputDecorationTheme(
          border: OutlineInputBorder(
            borderRadius: BorderRadius.all(Radius.circular(14)),
          ),
        ),
        filledButtonTheme: FilledButtonThemeData(
          style: FilledButton.styleFrom(
            padding: const EdgeInsets.symmetric(vertical: 14),
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(14),
            ),
          ),
        ),
      ),
      darkTheme: ThemeData(
        colorScheme: darkSeed,
        useMaterial3: true,
        appBarTheme: AppBarTheme(
          centerTitle: false,
          backgroundColor: darkSeed.surface,
          foregroundColor: darkSeed.onSurface,
        ),
        cardTheme: CardThemeData(
          elevation: 1,
          margin: EdgeInsets.zero,
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(20),
          ),
        ),
        inputDecorationTheme: const InputDecorationTheme(
          border: OutlineInputBorder(
            borderRadius: BorderRadius.all(Radius.circular(14)),
          ),
        ),
        filledButtonTheme: FilledButtonThemeData(
          style: FilledButton.styleFrom(
            padding: const EdgeInsets.symmetric(vertical: 14),
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(14),
            ),
          ),
        ),
      ),
      home: _loading
          ? const Scaffold(
              body: Center(child: CircularProgressIndicator()),
            )
          : pairing == null
              ? ScanQrPage(onScanned: (qr) => setState(() => _pairing = qr))
              : _confirmed
                  ? PingPage(pairing: pairing, onUnpair: _reset)
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
