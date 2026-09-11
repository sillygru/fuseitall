// SPDX-License-Identifier: AGPL-3.0-only

import 'package:flutter/services.dart';

// Event-driven clipboard watcher backed by MainActivity's
// OnPrimaryClipChangedListener → EventChannel fuseitall/clipboardEvents.
// Push-based: zero CPU while idle, fires only on copy.
// Android 10+ background reads return null/empty — callers drop them.
// Emits either String (text) or Map (image: {kind,image_b64,mime}).
class ClipboardWatcher {
  ClipboardWatcher({EventChannel? channel}) : _channel = channel ?? const EventChannel('fuseitall/clipboardEvents');
  final EventChannel _channel;

  /// Legacy text stream.
  Stream<String> get changes => _channel.receiveBroadcastStream().where((e) => e is String).cast<String>().map((s) => s.trim()).where((s) => s.isNotEmpty);

  /// Full clip events: String text or Map image.
  Stream<dynamic> get clipChanges => _channel.receiveBroadcastStream().where((e) => e != null);
}
