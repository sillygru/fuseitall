// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Reading this as: app theming layer for all screens, following HIG 5/6/7/8
// — palette is a 1:1 port of apps/mac/frontend/src/app.css so the phone
// and the Mac share the same NSColor-mapped tokens.

import 'package:flutter/material.dart';

// ── macOS tokens (app.css :root) ──────────────────────────────────────
abstract final class MacTokens {
  // Light
  static const windowLight = Color(0xFFECECEC);
  static const sidebarLight = Color(0xFFF2F2F4);
  static const controlLight = Color(0xFFFFFFFF);
  static const altRowLight = Color(0xFFF5F5F7);
  static const labelLight = Color(0xFF1D1D1F);
  static const secondaryLight = Color(0xFF6E6E73);
  static const tertiaryLight = Color(0xFFAEAEB2);
  static const separatorLight = Color(0xFFD1D1D6);
  static const accentLight = Color(0xFF007AFF);
  static const okLight = Color(0xFF248A3D);
  static const warnLight = Color(0xFF9A6A00);
  static const badLight = Color(0xFFD70015);
  static const destructiveLight = Color(0xFFFF3B30);

  // Dark
  static const windowDark = Color(0xFF323232);
  static const sidebarDark = Color(0xFF2B2B2D);
  static const controlDark = Color(0xFF1E1E1E);
  static const altRowDark = Color(0xFF262628);
  static const labelDark = Color(0xFFFFFFFF);
  static const secondaryDark = Color(0xFF98989F);
  static const tertiaryDark = Color(0xFF636366);
  static const separatorDark = Color(0xFF424245);
  static const accentDark = Color(0xFF0A84FF);
  static const okDark = Color(0xFF30D158);
  static const warnDark = Color(0xFFFFD60A);
  static const badDark = Color(0xFFFF453A);
  static const destructiveDark = Color(0xFFFF453A);
}

class AppTheme {
  // Accent wash 15% (matches .icon-well / .pill-ok color-mix in app.css)
  static Color _wash(Color c, Color bg, double t) => Color.lerp(bg, c, t)!;

  static ColorScheme get lightScheme => ColorScheme(
        brightness: Brightness.light,
        primary: MacTokens.accentLight,
        onPrimary: Colors.white,
        primaryContainer: _wash(MacTokens.accentLight, MacTokens.controlLight, 0.12),
        onPrimaryContainer: const Color(0xFF001E4D),
        secondary: MacTokens.secondaryLight,
        onSecondary: Colors.white,
        // was ok-green wash (mixing blue + green) — use the same blue wash
        // as primary so the app stays monochrome; ok-green remains available
        // as a tiny dot/accent only, not a large container.
        secondaryContainer: _wash(MacTokens.accentLight, MacTokens.controlLight, 0.12),
        onSecondaryContainer: const Color(0xFF001E4D),
        tertiary: MacTokens.warnLight,
        onTertiary: Colors.white,
        tertiaryContainer: _wash(MacTokens.warnLight, MacTokens.controlLight, 0.14),
        onTertiaryContainer: const Color(0xFF3D2E00),
        error: MacTokens.destructiveLight,
        onError: Colors.white,
        errorContainer: _wash(MacTokens.badLight, MacTokens.controlLight, 0.10),
        onErrorContainer: const Color(0xFF4D0008),
        surface: MacTokens.controlLight,
        onSurface: MacTokens.labelLight,
        surfaceContainerLowest: MacTokens.controlLight,
        surfaceContainerLow: MacTokens.altRowLight,
        surfaceContainer: MacTokens.sidebarLight,
        surfaceContainerHigh: MacTokens.windowLight,
        surfaceContainerHighest: const Color(0xFFE8E8EA),
        onSurfaceVariant: MacTokens.secondaryLight,
        outline: MacTokens.separatorLight,
        outlineVariant: const Color(0xFFE5E5EA),
        scrim: Colors.black54,
        inverseSurface: MacTokens.labelLight,
        onInverseSurface: MacTokens.controlLight,
        inversePrimary: MacTokens.accentDark,
        surfaceDim: MacTokens.windowLight,
        surfaceBright: MacTokens.controlLight,
      );

  static ColorScheme get darkScheme => ColorScheme(
        brightness: Brightness.dark,
        primary: MacTokens.accentDark,
        onPrimary: Colors.white,
        primaryContainer: _wash(MacTokens.accentDark, MacTokens.controlDark, 0.22),
        onPrimaryContainer: const Color(0xFFD6E4FF),
        secondary: MacTokens.secondaryDark,
        onSecondary: MacTokens.controlDark,
        secondaryContainer: _wash(MacTokens.accentDark, MacTokens.controlDark, 0.22),
        onSecondaryContainer: const Color(0xFFD6E4FF),
        tertiary: MacTokens.warnDark,
        onTertiary: const Color(0xFF3D2E00),
        tertiaryContainer: _wash(MacTokens.warnDark, MacTokens.controlDark, 0.18),
        onTertiaryContainer: MacTokens.warnDark,
        error: MacTokens.destructiveDark,
        onError: Colors.white,
        errorContainer: _wash(MacTokens.badDark, MacTokens.controlDark, 0.18),
        onErrorContainer: const Color(0xFFFFDAD6),
        surface: MacTokens.controlDark,
        onSurface: MacTokens.labelDark,
        surfaceContainerLowest: const Color(0xFF121212),
        surfaceContainerLow: MacTokens.altRowDark,
        surfaceContainer: MacTokens.sidebarDark,
        surfaceContainerHigh: MacTokens.windowDark,
        surfaceContainerHighest: const Color(0xFF3A3A3C),
        onSurfaceVariant: MacTokens.secondaryDark,
        outline: MacTokens.separatorDark,
        outlineVariant: const Color(0xFF3A3A3C),
        scrim: Colors.black54,
        inverseSurface: MacTokens.controlLight,
        onInverseSurface: MacTokens.labelLight,
        inversePrimary: MacTokens.accentLight,
        surfaceDim: const Color(0xFF1A1A1C),
        surfaceBright: const Color(0xFF3A3A3C),
      );

  static ThemeData light() => _build(lightScheme, MacTokens.windowLight);
  static ThemeData dark() => _build(darkScheme, MacTokens.windowDark);

  static ThemeData _build(ColorScheme scheme, Color window) => ThemeData(
        colorScheme: scheme,
        useMaterial3: true,
        scaffoldBackgroundColor: window,
        appBarTheme: AppBarTheme(
          centerTitle: false,
          scrolledUnderElevation: 2,
          backgroundColor: scheme.surfaceContainer,
          foregroundColor: scheme.onSurface,
          elevation: 0,
          surfaceTintColor: Colors.transparent,
        ),
        cardTheme: CardThemeData(
          elevation: 0,
          margin: EdgeInsets.zero,
          color: scheme.surface,
          shadowColor: scheme.shadow.withValues(alpha: 0.08),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(12),
            side: BorderSide(color: scheme.outlineVariant, width: 1),
          ),
        ),
        dividerTheme: DividerThemeData(
          color: scheme.outlineVariant,
          thickness: 1,
          space: 1,
        ),
        navigationBarTheme: NavigationBarThemeData(
          backgroundColor: scheme.surfaceContainer,
          indicatorColor: scheme.primaryContainer,
          elevation: 0,
        ),
        navigationRailTheme: NavigationRailThemeData(
          backgroundColor: scheme.surfaceContainer,
          indicatorColor: scheme.primaryContainer,
          useIndicator: true,
        ),
        inputDecorationTheme: const InputDecorationTheme(
          border: InputBorder.none,
          enabledBorder: InputBorder.none,
          focusedBorder: InputBorder.none,
          errorBorder: InputBorder.none,
          focusedErrorBorder: InputBorder.none,
        ),
        filledButtonTheme: FilledButtonThemeData(
          style: FilledButton.styleFrom(
            backgroundColor: scheme.primary,
            foregroundColor: scheme.onPrimary,
            padding: const EdgeInsets.symmetric(vertical: 14, horizontal: 20),
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(6),
            ),
          ),
        ),
        textTheme: Typography.material2021().black.apply(
              bodyColor: scheme.onSurface,
              displayColor: scheme.onSurface,
            ),
      );
}
