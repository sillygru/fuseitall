// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Reading this as: sheet-like confirmation for pairing code, following HIG 4/6/9/10/14.

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'pair_qr.dart';

class ConfirmFingerprintPage extends StatefulWidget {
  const ConfirmFingerprintPage({
    required this.pairing,
    required this.onConfirmed,
    required this.onBack,
    this.saveError,
    super.key,
  });

  final PairQR pairing;
  final VoidCallback onConfirmed;
  final VoidCallback onBack;
  final String? saveError;

  @override
  State<ConfirmFingerprintPage> createState() => _ConfirmFingerprintPageState();
}

class _ConfirmFingerprintPageState extends State<ConfirmFingerprintPage> {
  final _typed = TextEditingController();
  String? _hint;

  @override
  void dispose() {
    _typed.dispose();
    super.dispose();
  }

  void _compare() {
    final typed = _typed.text.trim();
    if (typed.isEmpty) {
      setState(() => _hint = 'Enter the 6-digit code shown on your Mac');
      return;
    }
    if (typed == widget.pairing.code) {
      widget.onConfirmed();
    } else {
      setState(
        () => _hint =
            "That code doesn't match — check you're looking at "
            '${widget.pairing.deviceName}',
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    if (widget.pairing.code.isEmpty) return _oldMac(context);
    final colorScheme = Theme.of(context).colorScheme;
    return Scaffold(
      appBar: AppBar(title: const Text('Enter Code')),
      body: SafeArea(
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 640),
            child: CustomScrollView(
              slivers: [
                SliverPadding(
                  padding: const EdgeInsets.all(16),
                  sliver: SliverList.list(
                    children: [
                      Card(
                        child: Padding(
                          padding: const EdgeInsets.all(20),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.stretch,
                            children: [
                              Row(
                                children: [
                                  CircleAvatar(
                                    backgroundColor: colorScheme.primaryContainer,
                                    foregroundColor: colorScheme.onPrimaryContainer,
                                    child: const Icon(Icons.link),
                                  ),
                                  const SizedBox(width: 12),
                                  Expanded(
                                    child: Column(
                                      crossAxisAlignment: CrossAxisAlignment.start,
                                      children: [
                                        Text(
                                          'Pairing with ${widget.pairing.deviceName}.',
                                          style: Theme.of(context)
                                              .textTheme
                                              .titleMedium
                                              ?.copyWith(fontWeight: FontWeight.bold),
                                        ),
                                        const SizedBox(height: 2),
                                        Text(
                                          'Enter the 6-digit code shown on your Mac to confirm pairing.',
                                          style: Theme.of(context).textTheme.bodyMedium,
                                        ),
                                      ],
                                    ),
                                  ),
                                ],
                              ),
                              const SizedBox(height: 16),
                              TextField(
                                controller: _typed,
                                keyboardType: TextInputType.number,
                                maxLength: 6,
                                textAlign: TextAlign.center,
                                style: Theme.of(context).textTheme.headlineMedium?.copyWith(
                                      fontWeight: FontWeight.bold,
                                      letterSpacing: 8,
                                    ),
                                inputFormatters: [FilteringTextInputFormatter.digitsOnly],
                                decoration: const InputDecoration(
                                  border: InputBorder.none,
                                  labelText: '6-digit code on your Mac',
                                  counterText: '',
                                ),
                                onChanged: (_) {
                                  if (_hint != null) {
                                    setState(() => _hint = null);
                                  }
                                },
                              ),
                              if (_hint != null) ...[
                                const SizedBox(height: 8),
                                SelectableText.rich(
                                  TextSpan(children: [TextSpan(text: _hint)]),
                                  style: TextStyle(color: colorScheme.error),
                                ),
                              ],
                              const SizedBox(height: 12),
                              FilledButton(
                                onPressed: _compare,
                                child: const Text('Confirm and pair'),
                              ),
                              if (widget.saveError != null) ...[
                                const SizedBox(height: 8),
                                SelectableText.rich(
                                  TextSpan(children: [TextSpan(text: widget.saveError)]),
                                  style: TextStyle(color: colorScheme.error),
                                ),
                              ],
                              const SizedBox(height: 4),
                              TextButton(
                                onPressed: widget.onBack,
                                child: const Text('Back'),
                              ),
                            ],
                          ),
                        ),
                      ),
                      const SizedBox(height: 16),
                      Card(
                        child: Padding(
                          padding: const EdgeInsets.all(16),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.stretch,
                            children: [
                              Text(
                                'Advanced: verify this matches your Mac',
                                style: Theme.of(context)
                                    .textTheme
                                    .titleSmall
                                    ?.copyWith(fontWeight: FontWeight.bold),
                              ),
                              const SizedBox(height: 4),
                              SelectableText(
                                widget.pairing.fingerprint,
                                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                                      fontFamily: 'RobotoMono',
                                      fontFeatures: const [FontFeature.tabularFigures()],
                                    ),
                              ),
                            ],
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _oldMac(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Scaffold(
      appBar: AppBar(title: const Text('Enter Code')),
      body: SafeArea(
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 640),
            child: CustomScrollView(
              slivers: [
                SliverPadding(
                  padding: const EdgeInsets.all(16),
                  sliver: SliverList.list(
                    children: [
                      Container(
                        padding: const EdgeInsets.all(16),
                        decoration: BoxDecoration(
                          color: scheme.tertiaryContainer,
                          borderRadius: BorderRadius.circular(16),
                        ),
                        child: SelectableText.rich(
                          TextSpan(
                            style: TextStyle(color: scheme.onTertiaryContainer),
                            children: const [
                              TextSpan(
                                text: 'Update required\n',
                                style: TextStyle(fontWeight: FontWeight.bold),
                              ),
                              TextSpan(text: 'This Mac is on an old build — update it'),
                            ],
                          ),
                        ),
                      ),
                      const SizedBox(height: 12),
                      Text(
                        'Pairing with ${widget.pairing.deviceName}.',
                        style: Theme.of(context).textTheme.bodyMedium,
                      ),
                      const SizedBox(height: 12),
                      TextButton(
                        onPressed: widget.onBack,
                        child: const Text('Back'),
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
