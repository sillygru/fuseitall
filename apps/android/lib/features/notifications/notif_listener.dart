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
// Live-only (0.9.0): the native side holds an in-memory queue (cap 50,
// 5-min TTL) with no file persistence and no active-sweep replay, so a
// reconnecting Mac never receives stale history. Progress posts and muted
// packages are filtered natively; Dart re-checks (defense in depth).
// Missing channel or platform errors degrade to empty: presence never
// breaks for a listener failure.
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
  /// Progress posts return null/null (dropped before queueing).
  static ({NotifItem? post, String? removal}) parseEvent(Map<String, dynamic> m) {
    if (m['event'] == 'remove') {
      final clean = NotifItem.cleanId(m['id'] as String?);
      return (post: null, removal: clean);
    }
    final hasProgress = m['has_progress'] == true;
    if (hasProgress) return (post: null, removal: null);
    final item = NotifItem.fromPosted(
      id: m['id'] as String?,
      app: m['app'] as String?,
      packageName: m['package_name'] as String? ?? m['package'] as String?,
      iconB64: m['app_icon_b64'] as String? ?? m['icon_b64'] as String?,
      groupKey: m['group_key'] as String?,
      title: m['title'] as String?,
      text: m['text'] as String?,
      postedAt: m['posted_at'] is int ? m['posted_at'] as int : 0,
      ongoing: m['ongoing'] == true,
      hasProgress: false,
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

  /// Push the per-app filter snapshot to the native listener so progress
  /// and muted packages are dropped before queueing (bandwidth/battery win).
  /// Best-effort: failures only reach the debug console.
  Future<void> updateFilter({
    required String mode,
    required Set<String> muted,
    required Set<String> allowed,
  }) async {
    try {
      await _channel.invokeMethod<void>('updateNotifFilter', {
        'mode': mode,
        'muted_packages': muted.toList(),
        'allowed_packages': allowed.toList(),
      });
    } catch (e) {
      debugPrint('notif filter push failed: $e');
    }
  }

  /// Launchable apps for the per-app filter UI: [{package_name, app}].
  /// Best-effort: failures yield an empty list, never a throw.
  Future<List<Map<String, String>>> listApps() async {
    try {
      final raw = await _channel.invokeMethod<List<dynamic>>('listNotifApps');
      final out = <Map<String, String>>[];
      for (final entry in raw ?? const []) {
        if (entry is! Map) continue;
        final pkg = '${entry['package_name'] ?? ''}'.trim();
        if (pkg.isEmpty) continue;
        final app = '${entry['app'] ?? pkg}'.trim();
        out.add({'package_name': pkg, 'app': app.isEmpty ? pkg : app});
        if (out.length >= 500) break;
      }
      return out;
    } catch (e) {
      debugPrint('notif list apps failed: $e');
      return const [];
    }
  }

  /// One page of the launchable inventory for Mac fetch:
  /// ({entries: [{package_name, app, app_icon_b64?}], nextCursor}).
  /// Sorted by package_name (stable pagination). Best-effort: failures
  /// yield an empty last page, never a throw.
  Future<({List<Map<String, String>> entries, String nextCursor})>
      listAppsPaged({
    String cursor = '',
    int limit = 50,
    bool withIcons = true,
  }) async {
    try {
      final raw = await _channel.invokeMethod<Map<dynamic, dynamic>>(
        'listNotifAppsPaged',
        {'cursor': cursor, 'limit': limit.clamp(1, 50), 'with_icons': withIcons},
      );
      final m = raw == null ? const {} : Map<String, dynamic>.from(raw);
      final entries = <Map<String, String>>[];
      final rawEntries = m['entries'];
      if (rawEntries is List) {
        for (final entry in rawEntries) {
          if (entry is! Map) continue;
          final em = Map<String, dynamic>.from(entry);
          final pkg = '${em['package_name'] ?? ''}'.trim();
          if (pkg.isEmpty || pkg.length > 128) continue;
          final app = '${em['app'] ?? pkg}'.trim();
          final row = <String, String>{
            'package_name': pkg,
            'app': app.isEmpty ? pkg : app,
          };
          final icon = '${em['app_icon_b64'] ?? ''}'.trim();
          if (icon.isNotEmpty && icon.length <= 32768) {
            row['app_icon_b64'] = icon;
          }
          entries.add(row);
          if (entries.length >= 50) break;
        }
      }
      final next = '${m['next_cursor'] ?? ''}'.trim();
      return (entries: entries, nextCursor: next);
    } catch (e) {
      debugPrint('notif list apps paged failed: $e');
      return (entries: const <Map<String, String>>[], nextCursor: '');
    }
  }
}
