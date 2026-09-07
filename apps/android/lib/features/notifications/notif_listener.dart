// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import 'notif_models.dart';

// Bridge to the native NotificationListenerService (NotifListener.kt).
// The service queues post/remove events while the app is backgrounded;
// this poller drains them on heartbeat. Missing channel or platform
// errors degrade to empty: presence never breaks for a listener failure.
class NotifListener {
  NotifListener({MethodChannel? channel})
      : _channel = channel ?? const MethodChannel('fuseitall/notif');

  final MethodChannel _channel;

  /// Drain queued native events in one poll (the native side clears its
  /// queue per call, so posts and removals must come from the same batch).
  /// Each map carries {event: post|remove, id, app, title, text,
  /// posted_at}. Never throws.
  Future<({List<NotifItem> posts, List<String> removals})> drain() async {
    final posts = <NotifItem>[];
    final removals = <String>[];
    try {
      final raw = await _channel.invokeMethod<List<dynamic>>('pollNotifs');
      for (final entry in raw ?? const []) {
        if (entry is! Map) continue;
        final m = Map<String, dynamic>.from(entry);
        if (m['event'] == 'remove') {
          final clean = NotifItem.cleanId(m['id'] as String?);
          if (clean != null) removals.add(clean);
          continue;
        }
        final item = NotifItem.fromPosted(
          id: m['id'] as String?,
          app: m['app'] as String?,
          title: m['title'] as String?,
          text: m['text'] as String?,
          postedAt: m['posted_at'] is int ? m['posted_at'] as int : 0,
        );
        if (item != null) posts.add(item);
      }
    } catch (e) {
      debugPrint('notif drain failed: $e');
    }
    return (posts: posts, removals: removals);
  }

  /// Ask the native listener to cancel a system notification the Mac
  /// dismissed. Best-effort: failures only reach the debug console.
  Future<void> dismiss(String id) async {
    final clean = NotifItem.cleanId(id);
    if (clean == null) return;
    try {
      await _channel.invokeMethod<void>('dismissNotif', {'id': clean});
    } catch (e) {
      debugPrint('notif dismiss failed: $e');
    }
  }
}
