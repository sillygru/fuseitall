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
// dedupe live in PlaybackState. Never throws.
//
// Realtime, zero pulls: subscribe to [playbackEvents] for push-driven
// snapshots emitted on every MediaController callback (track, state,
// idle). [resubscribe] re-anchors native callbacks on connect/resume.
// There is intentionally no pull API: nothing here may poll.
class PlaybackSync {
  PlaybackSync({MethodChannel? channel, EventChannel? eventChannel})
      : _channel = channel ?? const MethodChannel('fuseitall/playback'),
        _eventChannel =
            eventChannel ?? const EventChannel('fuseitall/playbackEvents');

  final MethodChannel _channel;
  final EventChannel _eventChannel;

  /// Push-driven now-playing snapshots from the native MediaSession
  /// callbacks. Emits null readings as null so listeners can ignore them.
  /// Never throws: stream errors surface via onError to the subscriber.
  Stream<PlaybackState?> get playbackEvents => _eventChannel
      .receiveBroadcastStream()
      .where((e) => e is Map)
      .map((m) {
        try {
          return PlaybackState.fromJson(Map<String, dynamic>.from(m as Map));
        } catch (_) {
          return null;
        }
      });

  /// Ask native to re-register MediaSession callbacks and emit one anchor
  /// push (covers subscribe-time races with zero pulls). Event-triggered
  /// only: connect, resume, mode-toggle. Never throws.
  Future<void> resubscribe() async {
    try {
      await _channel.invokeMethod<void>('watch');
    } catch (e) {
      debugPrint('playback watch failed: $e');
    }
  }

  /// Send one transport command to the active session. Returns true when
  /// the platform accepted it. [packageHint] routes to the player that
  /// produced the last pushed state (multi-session correctness); empty
  /// falls back to playing-else-first. Never throws.
  Future<bool> command(String cmd, {String packageHint = ''}) async {
    final c = cmd.trim().toLowerCase();
    if (!PlaybackCmd.isValid(c)) return false;
    try {
      final args = <String, dynamic>{'cmd': c};
      if (packageHint.trim().isNotEmpty) {
        args['package_name'] = packageHint.trim();
      }
      final ok = await _channel.invokeMethod<bool>('command', args);
      return ok ?? false;
    } catch (e) {
      debugPrint('playback command failed: $e');
      return false;
    }
  }
}
