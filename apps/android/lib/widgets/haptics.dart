// SPDX-License-Identifier: AGPL-3.0-only

import 'package:flutter/services.dart';

/// Centralized tactile haptics helper for micro-interactions.
/// Fails silently on devices without haptic motors or when disabled.
abstract final class AppHaptics {
  /// Subtle tick for buttons, cards, and tab selections.
  static void light() {
    HapticFeedback.lightImpact().ignore();
  }

  /// Click tick for segmented controls, switches, and PIN digit inputs.
  static void selection() {
    HapticFeedback.selectionClick().ignore();
  }

  /// Solid acknowledgment for successful pairings or completed actions.
  static void medium() {
    HapticFeedback.mediumImpact().ignore();
  }

  /// Notification vibration for invalid codes or critical errors.
  static void error() {
    HapticFeedback.vibrate().ignore();
  }
}
