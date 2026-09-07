// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'pair_qr.dart';

// Screen 2/3: 6-digit code confirm. The user reads the code off the Mac
// screen and types it here; the pairing token stays the secret, the code
// is anti-mistake UX + consent only. Errors render via SelectableText.rich,
// never a SnackBar. The full fingerprint stays as small read-only
// "Advanced" text (no typing). QRs without a code mean an old Mac build.
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

  /// Storage failure from the last confirm attempt (set by the parent, which
  /// owns the store). Shown verbatim; confirmation stays blocked while set.
  final String? saveError;

  @override
  State<ConfirmFingerprintPage> createState() =>
      _ConfirmFingerprintPageState();
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
      setState(
        () => _hint = 'Enter the 6-digit code shown on your Mac',
      );
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
      appBar: AppBar(title: const Text('Enter pairing code')),
      body: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 640),
          child: ListView(
            padding: const EdgeInsets.all(16),
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
                            backgroundColor:
                                colorScheme.primaryContainer,
                            foregroundColor:
                                colorScheme.onPrimaryContainer,
                            child: const Icon(Icons.link),
                          ),
                          const SizedBox(width: 12),
                          Expanded(
                            child: Column(
                              crossAxisAlignment:
                                  CrossAxisAlignment.start,
                              children: [
                                Text(
                                  'Pairing with ${widget.pairing.deviceName}.',
                                  style: const TextStyle(
                                    fontWeight: FontWeight.bold,
                                    fontSize: 16,
                                  ),
                                ),
                                const SizedBox(height: 2),
                                const Text(
                                  'Enter the 6-digit code shown on your Mac to confirm pairing.',
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
                        style: const TextStyle(
                          fontSize: 28,
                          letterSpacing: 8,
                          fontWeight: FontWeight.bold,
                        ),
                        inputFormatters: [
                          FilteringTextInputFormatter.digitsOnly
                        ],
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
                          TextSpan(
                            children: [
                              TextSpan(text: _hint),
                            ],
                          ),
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
                          TextSpan(
                            children: [
                              TextSpan(text: widget.saveError),
                            ],
                          ),
                          style: TextStyle(color: colorScheme.error),
                        ),
                      ],
                      TextButton(
                        onPressed: widget.onBack,
                        child: const Text('Back to scan'),
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
                      const Text(
                        'Advanced: verify this matches your Mac',
                        style: TextStyle(fontWeight: FontWeight.bold),
                      ),
                      const SizedBox(height: 4),
                      SelectableText(
                        widget.pairing.fingerprint,
                        style: const TextStyle(
                            fontSize: 12, fontFamily: 'monospace'),
                      ),
                    ],
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _oldMac(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    return Scaffold(
      appBar: AppBar(title: const Text('Enter pairing code')),
      body: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 640),
          child: ListView(
            padding: const EdgeInsets.all(16),
            children: [
              Card(
                color: colorScheme.tertiaryContainer,
                child: Padding(
                  padding: const EdgeInsets.all(16),
                  child: SelectableText.rich(
                    TextSpan(
                      style: TextStyle(
                        color: colorScheme.onTertiaryContainer,
                      ),
                      children: const [
                        TextSpan(
                          text: 'Update required\n',
                          style: TextStyle(fontWeight: FontWeight.bold),
                        ),
                        TextSpan(
                          text: 'This Mac is on an old build — update it',
                        ),
                      ],
                    ),
                  ),
                ),
              ),
              const SizedBox(height: 12),
              Text('Pairing with ${widget.pairing.deviceName}.'),
              const SizedBox(height: 12),
              TextButton(
                onPressed: widget.onBack,
                child: const Text('Back to scan'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
