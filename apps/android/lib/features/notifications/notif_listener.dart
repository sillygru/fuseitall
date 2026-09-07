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
  NotifListener({MethodChannel? channel, EventChannel? eventChannel})
      : _channel = channel ?? const MethodChannel('fuseitall/notif'),
        _eventChannel = eventChannel ?? const EventChannel('fuseitall/notifEvents');

  final MethodChannel _channel;
  final EventChannel _eventChannel;

  /// Stream of live native notification events (push-driven, zero polling delay).
  /// Emits `Map<String, dynamic>` {event: 'post'|'remove', id, ...}.
  Stream<Map<String, dynamic>> get notifEvents =>
      _eventChannel.receiveBroadcastStream().where((e) => e is Map).cast<Map>().map((m) => Map<String, dynamic>.from(m));

  /// Parse a raw native notification map into a typed post or removal. Pure.
  static ({NotifItem? post, String? removal}) parseEvent(Map<String, dynamic> m) {
    if (m['event'] == 'remove') {
      final clean = NotifItem.cleanId(m['id'] as String?);
      return (post: null, removal: clean);
    }
    final item = NotifItem.fromPosted(
      id: m['id'] as String?,
      app: m['app'] as String?,
      packageName: m['package_name'] as String? ?? m['package'] as String?,
      iconB64: m['app_icon_b64'] as String? ?? m['icon_b64'] as String?,
      groupKey: m['group_key'] as String?,
      title: m['title'] as String?,
      text: m['text'] as String?,
      postedAt: m['posted_at'] is int ? m['posted_at'] as int : 0,
    );
    return (post: item, removal: null);
  }

  /// Drain queued native events in one poll (the native side clears its
  /// queue per call, so posts and removals must come from the same batch).
  /// Each map carries {event: post|remove, id, app, package_name,
  /// app_icon_b64, group_key, title, text, posted_at}. Never throws.
  Future<({List<NotifItem> posts, List<String> removals})> drain() async {
    final posts = <NotifItem>[];
    final removals = <String>[];
    try {
      final raw = await _channel.invokeMethod<List<dynamic>>('pollNotifs');
      for (final entry in raw ?? const []) {
        if (entry is! Map) continue;
        final (:post, :removal) = parseEvent(Map<String, dynamic>.from(entry));
        if (post != null) posts.add(post);
        if (removal != null) removals.add(removal);
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
