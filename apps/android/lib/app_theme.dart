// SPDX-License-Identifier: AGPL-3.0-only

// Reading this as: studio-grade theming layer for all screens, following HIG 5/6/7/8
// — luxury monochrome design system harmonized with apps/mac/frontend/src/app.css.

import 'package:flutter/material.dart';

// ── Studio Design Tokens (Monochrome luxury) ─────────────────────────
abstract final class AppTokens {
  // Light: Porcelain / Obsidian
  static const windowLight = Color(0xFFF7F7F5);
  static const surfaceLight = Color(0xFFFFFFFF);
  static const altRowLight = Color(0xFFF0F0ED);
  static const containerLight = Color(0xFFECECE9);
  static const containerHighLight = Color(0xFFE2E2DE);
  static const containerHighestLight = Color(0xFFDCDCD7);
  static const labelLight = Color(0xFF121212);
  static const secondaryLight = Color(0xFF6C6C70);
  static const tertiaryLight = Color(0xFF8E8E93);
  static const separatorLight = Color(0x1F121212);
  static const accentLight = Color(0xFF121212);
  static const onAccentLight = Color(0xFFFFFFFF);
  static const okLight = Color(0xFF248A3D);
  static const warnLight = Color(0xFF9A6A00);
  static const badLight = Color(0xFFD70015);
  static const destructiveLight = Color(0xFFFF3B30);

  // Dark: Deep Obsidian / Luminous Off-White
  static const windowDark = Color(0xFF0E0E10);
  static const surfaceDark = Color(0xFF18181B);
  static const altRowDark = Color(0xFF1E1E22);
  static const containerDark = Color(0xFF222226);
  static const containerHighDark = Color(0xFF2B2B31);
  static const containerHighestDark = Color(0xFF34343B);
  static const labelDark = Color(0xFFF5F5F2);
  static const secondaryDark = Color(0xFFA2A2A8);
  static const tertiaryDark = Color(0xFF6E6E73);
  static const separatorDark = Color(0x24F5F5F2);
  static const accentDark = Color(0xFFF5F5F2);
  static const onAccentDark = Color(0xFF121214);
  static const okDark = Color(0xFF30D158);
  static const warnDark = Color(0xFFFFD60A);
  static const badDark = Color(0xFFFF453A);
  static const destructiveDark = Color(0xFFFF453A);
}

class AppTheme {
  static ColorScheme get lightScheme => const ColorScheme(
        brightness: Brightness.light,
        primary: AppTokens.accentLight,
        onPrimary: AppTokens.onAccentLight,
        primaryContainer: AppTokens.containerLight,
        onPrimaryContainer: AppTokens.labelLight,
        secondary: AppTokens.secondaryLight,
        onSecondary: Colors.white,
        secondaryContainer: AppTokens.altRowLight,
        onSecondaryContainer: AppTokens.labelLight,
        tertiary: AppTokens.warnLight,
        onTertiary: Colors.white,
        tertiaryContainer: Color(0xFFFFF7DB),
        onTertiaryContainer: Color(0xFF423000),
        error: AppTokens.destructiveLight,
        onError: Colors.white,
        errorContainer: Color(0xFFFFEAEA),
        onErrorContainer: Color(0xFF5A000A),
        surface: AppTokens.surfaceLight,
        onSurface: AppTokens.labelLight,
        surfaceContainerLowest: AppTokens.surfaceLight,
        surfaceContainerLow: AppTokens.altRowLight,
        surfaceContainer: AppTokens.containerLight,
        surfaceContainerHigh: AppTokens.containerHighLight,
        surfaceContainerHighest: AppTokens.containerHighestLight,
        onSurfaceVariant: AppTokens.secondaryLight,
        outline: AppTokens.separatorLight,
        outlineVariant: Color(0x14000000),
        scrim: Colors.black54,
        inverseSurface: AppTokens.labelLight,
        onInverseSurface: AppTokens.surfaceLight,
        inversePrimary: AppTokens.accentDark,
        surfaceDim: AppTokens.windowLight,
        surfaceBright: AppTokens.surfaceLight,
      );

  static ColorScheme get darkScheme => const ColorScheme(
        brightness: Brightness.dark,
        primary: AppTokens.accentDark,
        onPrimary: AppTokens.onAccentDark,
        primaryContainer: AppTokens.containerDark,
        onPrimaryContainer: AppTokens.labelDark,
        secondary: AppTokens.secondaryDark,
        onSecondary: AppTokens.windowDark,
        secondaryContainer: AppTokens.altRowDark,
        onSecondaryContainer: AppTokens.labelDark,
        tertiary: AppTokens.warnDark,
        onTertiary: Color(0xFF352700),
        tertiaryContainer: Color(0xFF3A3110),
        onTertiaryContainer: AppTokens.warnDark,
        error: AppTokens.destructiveDark,
        onError: Colors.white,
        errorContainer: Color(0xFF3F1414),
        onErrorContainer: Color(0xFFFFDAD6),
        surface: AppTokens.surfaceDark,
        onSurface: AppTokens.labelDark,
        surfaceContainerLowest: Color(0xFF0A0A0C),
        surfaceContainerLow: AppTokens.altRowDark,
        surfaceContainer: AppTokens.containerDark,
        surfaceContainerHigh: AppTokens.containerHighDark,
        surfaceContainerHighest: AppTokens.containerHighestDark,
        onSurfaceVariant: AppTokens.secondaryDark,
        outline: AppTokens.separatorDark,
        outlineVariant: Color(0x28FFFFFF),
        scrim: Colors.black87,
        inverseSurface: AppTokens.surfaceLight,
        onInverseSurface: AppTokens.labelLight,
        inversePrimary: AppTokens.accentLight,
        surfaceDim: AppTokens.windowDark,
        surfaceBright: AppTokens.containerHighDark,
      );

  static ThemeData light() => _build(lightScheme, AppTokens.windowLight);
  static ThemeData dark() => _build(darkScheme, AppTokens.windowDark);

  static ThemeData _build(ColorScheme scheme, Color window) {
    final isDark = scheme.brightness == Brightness.dark;
    final baseTextTheme = isDark
        ? Typography.material2021().white
        : Typography.material2021().black;

    return ThemeData(
      colorScheme: scheme,
      useMaterial3: true,
      scaffoldBackgroundColor: window,
      appBarTheme: AppBarTheme(
        centerTitle: false,
        scrolledUnderElevation: 0,
        backgroundColor: Colors.transparent,
        foregroundColor: scheme.onSurface,
        elevation: 0,
        surfaceTintColor: Colors.transparent,
        titleTextStyle: TextStyle(
          color: scheme.onSurface,
          fontSize: 20,
          fontWeight: FontWeight.w700,
          letterSpacing: -0.4,
        ),
      ),
      // Law: strictly NO outlines / borders. Tonal surfaces only.
      cardTheme: CardThemeData(
        elevation: isDark ? 0 : 0.5,
        margin: EdgeInsets.zero,
        color: scheme.surface,
        shadowColor: isDark ? Colors.transparent : Colors.black.withValues(alpha: 0.04),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(20),
        ),
      ),
      dividerTheme: DividerThemeData(
        color: scheme.outline,
        thickness: 0.5,
        space: 1,
      ),
      navigationBarTheme: NavigationBarThemeData(
        backgroundColor: scheme.surfaceContainer,
        indicatorColor: scheme.primary.withValues(alpha: isDark ? 0.16 : 0.09),
        elevation: 0,
        height: 68,
        labelTextStyle: WidgetStateProperty.resolveWith((states) {
          final selected = states.contains(WidgetState.selected);
          return TextStyle(
            fontSize: 12,
            fontWeight: selected ? FontWeight.w700 : FontWeight.w500,
            color: selected ? scheme.onSurface : scheme.onSurfaceVariant,
            letterSpacing: -0.1,
          );
        }),
      ),
      navigationRailTheme: NavigationRailThemeData(
        backgroundColor: scheme.surfaceContainer,
        indicatorColor: scheme.primary.withValues(alpha: isDark ? 0.16 : 0.09),
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
          elevation: 0,
          padding: const EdgeInsets.symmetric(vertical: 14, horizontal: 22),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(14),
          ),
          textStyle: const TextStyle(
            fontSize: 15,
            fontWeight: FontWeight.w600,
            letterSpacing: -0.2,
          ),
        ),
      ),
      textTheme: baseTextTheme.copyWith(
        displayLarge: baseTextTheme.displayLarge?.copyWith(letterSpacing: -1.0, fontWeight: FontWeight.bold),
        displayMedium: baseTextTheme.displayMedium?.copyWith(letterSpacing: -0.8, fontWeight: FontWeight.bold),
        titleLarge: baseTextTheme.titleLarge?.copyWith(letterSpacing: -0.5, fontWeight: FontWeight.w700),
        titleMedium: baseTextTheme.titleMedium?.copyWith(letterSpacing: -0.3, fontWeight: FontWeight.w600),
        titleSmall: baseTextTheme.titleSmall?.copyWith(letterSpacing: -0.2, fontWeight: FontWeight.w600),
        bodyLarge: baseTextTheme.bodyLarge?.copyWith(letterSpacing: -0.1),
        bodyMedium: baseTextTheme.bodyMedium?.copyWith(letterSpacing: 0),
        labelLarge: baseTextTheme.labelLarge?.copyWith(fontWeight: FontWeight.w600, letterSpacing: -0.1),
      ),
    );
  }
}
