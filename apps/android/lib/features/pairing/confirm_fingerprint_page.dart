// SPDX-License-Identifier: AGPL-3.0-only

// Reading this as: sheet-like confirmation for pairing code, following HIG 4/6/9/10/14.

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../widgets/haptics.dart';
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
  final _focusNode = FocusNode();
  String? _hint;

  @override
  void initState() {
    super.initState();
    _typed.addListener(() {
      if (mounted) setState(() {});
    });
  }

  @override
  void dispose() {
    _typed.dispose();
    _focusNode.dispose();
    super.dispose();
  }

  void _compare() {
    final typed = _typed.text.trim();
    if (typed.isEmpty) {
      AppHaptics.error();
      setState(() => _hint = 'Enter the 6-digit code shown on your Mac');
      return;
    }
    if (typed == widget.pairing.code) {
      AppHaptics.medium();
      widget.onConfirmed();
    } else {
      AppHaptics.error();
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

    final text = _typed.text;

    return Scaffold(
      appBar: AppBar(
        title: const Text('Enter Code'),
        scrolledUnderElevation: 0,
      ),
      body: SafeArea(
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 640),
            child: CustomScrollView(
              slivers: [
                SliverPadding(
                  padding: const EdgeInsets.all(20),
                  sliver: SliverList.list(
                    children: [
                      Card(
                        child: Padding(
                          padding: const EdgeInsets.all(24),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.stretch,
                            children: [
                              Row(
                                children: [
                                  Container(
                                    width: 48,
                                    height: 48,
                                    decoration: BoxDecoration(
                                      color: colorScheme.primaryContainer,
                                      borderRadius: BorderRadius.circular(16),
                                    ),
                                    child: Icon(
                                      Icons.link_rounded,
                                      color: colorScheme.onPrimaryContainer,
                                      size: 24,
                                    ),
                                  ),
                                  const SizedBox(width: 14),
                                  Expanded(
                                    child: Column(
                                      crossAxisAlignment: CrossAxisAlignment.start,
                                      children: [
                                        Text(
                                          'Pairing with ${widget.pairing.deviceName}.',
                                          style: Theme.of(context)
                                              .textTheme
                                              .titleMedium
                                              ?.copyWith(
                                                fontWeight: FontWeight.w700,
                                                letterSpacing: -0.3,
                                              ),
                                        ),
                                        const SizedBox(height: 3),
                                        Text(
                                          'Enter the 6-digit code shown on your Mac to confirm pairing.',
                                          style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                                                color: colorScheme.onSurfaceVariant,
                                                fontSize: 13,
                                              ),
                                        ),
                                      ],
                                    ),
                                  ),
                                ],
                              ),
                              const SizedBox(height: 24),
                              // Interactive luxury 6-digit pin segmented view
                              GestureDetector(
                                onTap: () => _focusNode.requestFocus(),
                                child: Row(
                                  mainAxisAlignment: MainAxisAlignment.spaceBetween,
                                  children: List.generate(6, (index) {
                                    final char = index < text.length ? text[index] : '';
                                    final isCurrent = index == text.length && _focusNode.hasFocus;
                                    return AnimatedContainer(
                                      duration: const Duration(milliseconds: 180),
                                      width: 46,
                                      height: 58,
                                      decoration: BoxDecoration(
                                        color: isCurrent
                                            ? colorScheme.primary.withValues(alpha: 0.08)
                                            : colorScheme.surfaceContainerHighest,
                                        borderRadius: BorderRadius.circular(14),
                                        boxShadow: isCurrent
                                            ? [
                                                BoxShadow(
                                                  color: colorScheme.primary.withValues(alpha: 0.2),
                                                  blurRadius: 10,
                                                ),
                                              ]
                                            : null,
                                      ),
                                      child: Center(
                                        child: Text(
                                          char.isNotEmpty ? char : (isCurrent ? '|' : '·'),
                                          style: TextStyle(
                                            fontSize: 24,
                                            fontWeight: FontWeight.w800,
                                            color: char.isNotEmpty
                                                ? colorScheme.onSurface
                                                : colorScheme.onSurfaceVariant.withValues(alpha: 0.5),
                                            fontFeatures: const [FontFeature.tabularFigures()],
                                          ),
                                        ),
                                      ),
                                    );
                                  }),
                                ),
                              ),
                              // Accessible TextField retained for screen readers, automated testing, and software keyboard input
                              Opacity(
                                opacity: 0.0,
                                child: SizedBox(
                                  height: 1,
                                  child: TextField(
                                    controller: _typed,
                                    focusNode: _focusNode,
                                    keyboardType: TextInputType.number,
                                    maxLength: 6,
                                    textAlign: TextAlign.center,
                                    inputFormatters: [FilteringTextInputFormatter.digitsOnly],
                                    decoration: const InputDecoration(
                                      border: InputBorder.none,
                                      labelText: '6-digit code on your Mac',
                                      counterText: '',
                                    ),
                                    onChanged: (val) {
                                      AppHaptics.selection();
                                      if (_hint != null) {
                                        setState(() => _hint = null);
                                      }
                                      if (val.length == 6 && val == widget.pairing.code) {
                                        _compare();
                                      }
                                    },
                                  ),
                                ),
                              ),
                              if (_hint != null) ...[
                                const SizedBox(height: 12),
                                SelectableText.rich(
                                  TextSpan(children: [TextSpan(text: _hint)]),
                                  style: TextStyle(
                                    color: colorScheme.error,
                                    fontWeight: FontWeight.w600,
                                    fontSize: 13,
                                  ),
                                ),
                              ],
                              const SizedBox(height: 18),
                              FilledButton(
                                onPressed: _compare,
                                child: const Text('Confirm and pair'),
                              ),
                              if (widget.saveError != null) ...[
                                const SizedBox(height: 10),
                                SelectableText.rich(
                                  TextSpan(children: [TextSpan(text: widget.saveError)]),
                                  style: TextStyle(color: colorScheme.error, fontSize: 13),
                                ),
                              ],
                              const SizedBox(height: 8),
                              TextButton(
                                onPressed: () {
                                  AppHaptics.light();
                                  widget.onBack();
                                },
                                child: const Text('Back'),
                              ),
                            ],
                          ),
                        ),
                      ),
                      const SizedBox(height: 16),
                      Card(
                        child: Padding(
                          padding: const EdgeInsets.all(20),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.stretch,
                            children: [
                              Row(
                                children: [
                                  Icon(Icons.fingerprint_rounded, size: 18, color: colorScheme.onSurfaceVariant),
                                  const SizedBox(width: 8),
                                  Text(
                                    'Advanced: verify this matches your Mac',
                                    style: Theme.of(context)
                                        .textTheme
                                        .titleSmall
                                        ?.copyWith(fontWeight: FontWeight.w700, letterSpacing: -0.2),
                                  ),
                                ],
                              ),
                              const SizedBox(height: 8),
                              SelectableText(
                                widget.pairing.fingerprint,
                                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                                      fontFamily: 'RobotoMono',
                                      letterSpacing: 0.5,
                                      color: colorScheme.onSurfaceVariant,
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
      appBar: AppBar(title: const Text('Enter Code'), scrolledUnderElevation: 0),
      body: SafeArea(
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 640),
            child: CustomScrollView(
              slivers: [
                SliverPadding(
                  padding: const EdgeInsets.all(20),
                  sliver: SliverList.list(
                    children: [
                      Container(
                        padding: const EdgeInsets.all(20),
                        decoration: BoxDecoration(
                          color: scheme.tertiaryContainer,
                          borderRadius: BorderRadius.circular(20),
                        ),
                        child: SelectableText.rich(
                          TextSpan(
                            style: TextStyle(color: scheme.onTertiaryContainer),
                            children: const [
                              TextSpan(
                                text: 'Update required\n',
                                style: TextStyle(fontWeight: FontWeight.w700, fontSize: 16),
                              ),
                              TextSpan(text: 'This Mac is on an old build — update it'),
                            ],
                          ),
                        ),
                      ),
                      const SizedBox(height: 16),
                      Text(
                        'Pairing with ${widget.pairing.deviceName}.',
                        style: Theme.of(context).textTheme.bodyMedium,
                      ),
                      const SizedBox(height: 16),
                      TextButton(
                        onPressed: () {
                          AppHaptics.light();
                          widget.onBack();
                        },
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
