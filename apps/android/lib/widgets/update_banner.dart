// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter/material.dart';

// HIG 10: version-gate update uses an inline row (not a modal)
// with the canonical verbatim server message.
class UpdateBanner extends StatelessWidget {
  const UpdateBanner({
    required this.message,
    this.detail,
    super.key,
  });

  final String message;
  final String? detail;

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
        key: const Key('updateBannerText'),
        TextSpan(
          style: TextStyle(color: scheme.onTertiaryContainer),
          children: [
            TextSpan(
              text: 'Update required\n',
              style: Theme.of(context).textTheme.titleSmall?.copyWith(
                    color: scheme.onTertiaryContainer,
                    fontWeight: FontWeight.bold,
                  ),
            ),
            if (detail != null) TextSpan(text: '$detail\n'),
            TextSpan(text: message),
          ],
        ),
      ),
    );
  }
}
