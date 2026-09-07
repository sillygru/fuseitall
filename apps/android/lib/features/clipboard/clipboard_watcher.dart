// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

/// Event-driven clipboard changes from MainActivity's native
/// OnPrimaryClipChangedListener. No polling: Dart subscribes once and gets
/// a callback per copy. Missing channel degrades to never-firing (callers
/// keep their foreground poll fallback).
class ClipboardWatcher {
  ClipboardWatcher({EventChannel? channel})
    : _channel = channel ?? const EventChannel('fuseitall/clipboardEvents');

  final EventChannel _channel;

  Stream<String> get changes {
    try {
      return _channel
          .receiveBroadcastStream()
          .where((e) => e is String)
          .cast<String>();
    } catch (e) {
      debugPrint('clipboard watcher failed: $e');
      return const Stream.empty();
    }
  }
}
