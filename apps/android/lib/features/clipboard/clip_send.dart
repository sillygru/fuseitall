// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

// Bridge to the invisible ClipSendActivity focus trigger.
//
// Android 10+ only lets the focused UID read the clipboard. Tapping the link
// notification action or the Quick Settings tile opens ClipSendActivity,
// which takes focus and invokes onClipFocus on this channel; the caller then
// runs its normal foreground read + send and acks so the activity closes.
// Cold path (engine was dead): the activity relaunches MainActivity and Dart
// drains it once via popRequested. Push-only: no timers, fires on tap/auto.
class ClipSendChannel {
  ClipSendChannel({MethodChannel? channel})
      : _channel = channel ?? const MethodChannel('fuseitall/clipSend');

  final MethodChannel _channel;

  /// Registers the focus handler. The handler must perform the foreground
  /// read + send, then [ackDone]. Never throws.
  void setFocusHandler(Future<void> Function() onFocus) {
    try {
      _channel.setMethodCallHandler((call) async {
        if (call.method == 'onClipFocus') {
          await onFocus();
        }
      });
    } catch (e) {
      debugPrint('clipSend handler failed: $e');
    }
  }

  void clearHandler() {
    try {
      _channel.setMethodCallHandler(null);
    } catch (e) {
      debugPrint('clipSend clear failed: $e');
    }
  }

  /// Cold-path latch: true once when a send was requested while Dart was
  /// dead. Never throws.
  Future<bool> popRequested() async {
    try {
      return await _channel.invokeMethod<bool>('popRequested') ?? false;
    } catch (e) {
      debugPrint('clipSend pop failed: $e');
      return false;
    }
  }

  /// Tells ClipSendActivity the send finished so it can close. Never throws.
  Future<void> ackDone() async {
    try {
      await _channel.invokeMethod<void>('clipSendDone');
    } catch (e) {
      debugPrint('clipSend ack failed: $e');
    }
  }
}
