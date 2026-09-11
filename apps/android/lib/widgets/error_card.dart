// SPDX-License-Identifier: AGPL-3.0-only

import 'package:flutter/material.dart';

class ErrorCard extends StatelessWidget {
  const ErrorCard({required this.title, required this.detail, super.key});

  final String title;
  final String detail;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Card(
      color: scheme.errorContainer,
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: SelectableText.rich(
          TextSpan(
            style: TextStyle(color: scheme.onErrorContainer),
            children: [
              TextSpan(
                text: '$title\n',
                style: const TextStyle(fontWeight: FontWeight.bold),
              ),
              TextSpan(text: detail),
            ],
          ),
        ),
      ),
    );
  }
}

class NoticeBanner extends StatelessWidget {
  const NoticeBanner({required this.title, required this.body, super.key});

  final String title;
  final String body;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: scheme.tertiaryContainer,
        borderRadius: BorderRadius.circular(16),
      ),
      child: SelectableText.rich(
        key: const Key('pairingNotice'),
        TextSpan(
          style: TextStyle(color: scheme.onTertiaryContainer),
          children: [
            TextSpan(
              text: '$title\n',
              style: const TextStyle(fontWeight: FontWeight.bold),
            ),
            TextSpan(text: body),
          ],
        ),
      ),
    );
  }
}
