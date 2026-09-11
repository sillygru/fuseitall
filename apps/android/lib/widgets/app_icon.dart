// SPDX-License-Identifier: AGPL-3.0-only

// Reading this as: brand mark widget for in-app header, following HIG 13/7.

import 'package:flutter/material.dart';
import 'package:flutter_svg/flutter_svg.dart';

/// Stacked-squares brand mark (v14 Soft Standard).
/// Glyph-only vector, tinted via [color] — never hard-coded hex.
class AppIcon extends StatelessWidget {
  const AppIcon({this.size = 32, this.color, this.semanticLabel, super.key});

  final double size;
  final Color? color;
  final String? semanticLabel;

  @override
  Widget build(BuildContext context) {
    final effective = color ?? Theme.of(context).colorScheme.onSurface;
    // Decorative when used beside a text title — hide from semantics.
    final label = semanticLabel;
    return Semantics(
      label: label,
      image: label != null,
      excludeSemantics: label == null,
      child: SvgPicture.asset(
        'assets/icon/app_icon.svg',
        width: size,
        height: size,
        colorFilter: ColorFilter.mode(effective, BlendMode.srcIn),
        semanticsLabel: label,
      ),
    );
  }
}

/// Circular well for the brand mark — M3 surfaceContainer + accent wash.
/// Use only in the home hero header, not in navigation/tab bars.
class AppIconWell extends StatelessWidget {
  const AppIconWell({this.size = 56, super.key});

  final double size;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
      width: size,
      height: size,
      decoration: BoxDecoration(
        color: scheme.primaryContainer,
        shape: BoxShape.circle,
      ),
      child: Center(
        child: AppIcon(
          size: size * 0.52,
          color: scheme.onPrimaryContainer,
        ),
      ),
    );
  }
}
