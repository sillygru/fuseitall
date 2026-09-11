// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Reading this as: primary screen for Scan QR, following HIG 1/2/4/5/6/8/9/14.

import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import '../../result.dart';
import '../../widgets/error_card.dart';
import '../../widgets/haptics.dart';
import 'pair_qr.dart';

class ScanQrPage extends StatefulWidget {
  const ScanQrPage({required this.onScanned, this.notice, super.key});

  final void Function(PairQR pairing) onScanned;
  final String? notice;

  @override
  State<ScanQrPage> createState() => _ScanQrPageState();
}

class _ScanQrPageState extends State<ScanQrPage> {
  final _paste = TextEditingController();
  bool _handled = false;
  String? _error;

  @override
  void dispose() {
    _paste.dispose();
    super.dispose();
  }

  void _accept(String? raw) {
    if (raw == null || _handled) return;
    switch (parsePairQrJson(raw)) {
      case Ok(value: final qr):
        _handled = true;
        AppHaptics.medium();
        widget.onScanned(qr);
      case Err(failure: final f):
        AppHaptics.error();
        setState(() => _error = f.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    final notice = widget.notice;
    return Scaffold(
      appBar: AppBar(
        title: const Text('Pair QR'),
        scrolledUnderElevation: 0,
      ),
      body: SafeArea(
        child: LayoutBuilder(
          builder: (context, constraints) {
            final wide = constraints.maxWidth > 700;
            final cards = wide
                ? Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Expanded(child: _scanCard(context)),
                      const SizedBox(width: 16),
                      Expanded(child: _manualCard(context)),
                    ],
                  )
                : Column(
                    children: [
                      _scanCard(context),
                      const SizedBox(height: 16),
                      _manualCard(context),
                    ],
                  );

            return Center(
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 960),
                child: CustomScrollView(
                  slivers: [
                    SliverPadding(
                      padding: const EdgeInsets.all(20),
                      sliver: SliverList.list(
                        children: [
                          if (notice != null) ...[
                            NoticeBanner(
                              title: 'Pairing changed',
                              body: notice,
                            ),
                            const SizedBox(height: 16),
                          ],
                          cards,
                        ],
                      ),
                    ),
                  ],
                ),
              ),
            );
          },
        ),
      ),
    );
  }

  Widget _scanCard(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                Container(
                  padding: const EdgeInsets.all(8),
                  decoration: BoxDecoration(
                    color: scheme.primaryContainer,
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: Icon(
                    Icons.qr_code_scanner_rounded,
                    color: scheme.onPrimaryContainer,
                    size: 22,
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Text(
                    'Point the camera at the QR on your Mac. '
                    'The camera is used only to read that code; no photos are stored.',
                    style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                          fontSize: 13,
                          height: 1.35,
                          color: scheme.onSurfaceVariant,
                        ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 16),
            SizedBox(
              height: 290,
              child: ClipRRect(
                borderRadius: BorderRadius.circular(18),
                child: Stack(
                  fit: StackFit.expand,
                  children: [
                    MobileScanner(
                      onDetect: (capture) {
                        for (final code in capture.barcodes) {
                          _accept(code.rawValue);
                          if (_handled) break;
                        }
                      },
                    ),
                    // Elegant minimalist camera framing
                    Center(
                      child: Container(
                        width: 200,
                        height: 200,
                        decoration: BoxDecoration(
                          borderRadius: BorderRadius.circular(20),
                          color: Colors.white.withValues(alpha: 0.05),
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ),
            if (_error != null) ...[
              const SizedBox(height: 14),
              SelectableText.rich(
                TextSpan(
                  children: [
                    const TextSpan(
                      text: 'Could not use that QR.\n',
                      style: TextStyle(fontWeight: FontWeight.bold),
                    ),
                    TextSpan(text: _error),
                  ],
                ),
                style: TextStyle(color: scheme.error, fontSize: 13),
              ),
            ],
          ],
        ),
      ),
    );
  }

  Widget _manualCard(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                Container(
                  padding: const EdgeInsets.all(8),
                  decoration: BoxDecoration(
                    color: scheme.surfaceContainerHighest,
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: Icon(
                    Icons.keyboard_alt_outlined,
                    color: scheme.onSurfaceVariant,
                    size: 20,
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Text(
                    'No camera? Paste the QR JSON:',
                    style: Theme.of(context).textTheme.titleSmall?.copyWith(
                          fontWeight: FontWeight.w700,
                          letterSpacing: -0.2,
                        ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 14),
            Container(
              decoration: BoxDecoration(
                color: scheme.surfaceContainerHighest,
                borderRadius: BorderRadius.circular(14),
              ),
              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
              child: TextField(
                controller: _paste,
                maxLines: 3,
                decoration: const InputDecoration(
                  border: InputBorder.none,
                  hintText: '{"device_name": ...}',
                ),
                style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                      fontFamily: 'RobotoMono',
                      fontSize: 12,
                    ),
              ),
            ),
            const SizedBox(height: 14),
            FilledButton(
              onPressed: () {
                AppHaptics.light();
                _accept(_paste.text);
              },
              child: const Text('Use pasted JSON'),
            ),
          ],
        ),
      ),
    );
  }
}
