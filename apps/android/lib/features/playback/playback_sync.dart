// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import 'playback_models.dart';

// Thin adapter to the native MediaSession reader (MediaSessionManager via
// the notification-listener component). No business logic: shaping and
// throttling live in PlaybackState. Never throws: platform errors degrade
// to null so the UI shows Not playing with a fix CTA.
class PlaybackSync {
  PlaybackSync({MethodChannel? channel})
      : _channel = channel ?? const MethodChannel('fuseitall/playback');

  final MethodChannel _channel;

  /// Current now-playing snapshot, or null when nothing plays / permission
  /// missing / platform unavailable.
  Future<PlaybackState?> current() async {
    try {
      final raw = await _channel.invokeMethod<Map>('current');
      if (raw == null) return null;
      return PlaybackState.fromJson(Map<String, dynamic>.from(raw));
    } catch (e) {
      debugPrint('playback current failed: $e');
      return null;
    }
  }

  /// Send one transport command to the active session. Returns true when
  /// the platform accepted it.
  Future<bool> command(String cmd) async {
    final c = cmd.trim().toLowerCase();
    if (!PlaybackCmd.isValid(c)) return false;
    try {
      final ok = await _channel.invokeMethod<bool>('command', {'cmd': c});
      return ok ?? false;
    } catch (e) {
      debugPrint('playback command failed: $e');
      return false;
    }
  }

  /// Whether the listener component grants active-session access.
  Future<bool> hasAccess() async {
    try {
      return await _channel.invokeMethod<bool>('hasAccess') ?? false;
    } catch (e) {
      debugPrint('playback access check failed: $e');
      return false;
    }
  }

  Future<void> openSettings() async {
    try {
      await _channel.invokeMethod<void>('openSettings');
    } catch (e) {
      debugPrint('open playback settings failed: $e');
    }
  }
}
