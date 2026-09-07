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
        widget.onScanned(qr);
      case Err(failure: final f):
        setState(() => _error = f.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    final notice = widget.notice;
    return Scaffold(
      appBar: AppBar(title: const Text('Pair QR')),
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
            final body = wide
                ? Center(
                    child: ConstrainedBox(
                      constraints: const BoxConstraints(maxWidth: 960),
                      child: CustomScrollView(
                        slivers: [
                          SliverPadding(
                            padding: const EdgeInsets.all(16),
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
                  )
                : Center(
                    child: ConstrainedBox(
                      constraints: const BoxConstraints(maxWidth: 960),
                      child: CustomScrollView(
                        slivers: [
                          SliverPadding(
                            padding: const EdgeInsets.all(16),
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
            return body;
          },
        ),
      ),
    );
  }

  Widget _scanCard(BuildContext context) => Card(
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Row(
                children: [
                  Icon(
                    Icons.qr_code_scanner,
                    color: Theme.of(context).colorScheme.primary,
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      'Point the camera at the QR on your Mac. '
                      'The camera is used only to read that code; no photos are stored.',
                      style: Theme.of(context).textTheme.bodyMedium,
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 12),
              SizedBox(
                height: 280,
                child: ClipRRect(
                  borderRadius: BorderRadius.circular(16),
                  child: MobileScanner(
                    onDetect: (capture) {
                      for (final code in capture.barcodes) {
                        _accept(code.rawValue);
                        if (_handled) break;
                      }
                    },
                  ),
                ),
              ),
              if (_error != null) ...[
                const SizedBox(height: 12),
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
                  style: TextStyle(color: Theme.of(context).colorScheme.error),
                ),
              ],
            ],
          ),
        ),
      );

  Widget _manualCard(BuildContext context) => Card(
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Row(
                children: [
                  Icon(
                    Icons.keyboard_alt_outlined,
                    color: Theme.of(context).colorScheme.primary,
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      'No camera? Paste the QR JSON:',
                      style: Theme.of(context).textTheme.titleSmall?.copyWith(
                            fontWeight: FontWeight.bold,
                          ),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 12),
              TextField(
                controller: _paste,
                maxLines: 3,
                decoration: const InputDecoration(
                  border: InputBorder.none,
                  hintText: '{"device_name": ...}',
                ),
                style: Theme.of(context).textTheme.bodyMedium,
              ),
              const SizedBox(height: 8),
              FilledButton(
                onPressed: () => _accept(_paste.text),
                child: const Text('Use pasted JSON'),
              ),
            ],
          ),
        ),
      );
}
