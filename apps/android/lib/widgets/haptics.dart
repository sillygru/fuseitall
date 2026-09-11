// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

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
