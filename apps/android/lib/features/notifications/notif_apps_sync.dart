// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'notif_listener.dart';

// Handles inbound notif-apps-req pages and drives notif-apps-resp.
// Thin: no UI, no transport fallback — caller supplies [sendFeature] which
// already handles TOFU + fallback via PhoneTransport, and [listPage] which
// defaults to the platform inventory.
class NotifAppsSync {
  NotifAppsSync({
    required this.sendFeature,
    NotifListener? listener,
    this._listPage,
  }) : _listener = listener ?? NotifListener();

  final Future<void> Function(String type, Map<String, Object?> payload)
      sendFeature;
  final NotifListener _listener;
  final Future<({List<Map<String, String>> entries, String nextCursor})>
      Function({
    required String cursor,
    required int limit,
    required bool withIcons,
  })? _listPage;

  /// Entry point from PingPage's feat channel. Returns true if handled.
  Future<bool> handleEvent(Map<String, dynamic> envelope) async {
    final type = envelope['type'] as String?;
    final payload = envelope['payload'];
    if (type != 'notif-apps-req' || payload is! Map<String, dynamic>) {
      return false;
    }
    await _handleReq(payload);
    return true;
  }

  Future<void> _handleReq(Map<String, dynamic> p) async {
    final reqId = '${p['req_id'] ?? ''}'.trim();
    if (reqId.isEmpty || reqId.length > 64) return;
    final cursor = '${p['cursor'] ?? ''}'.trim();
    if (cursor.length > 128) {
      return _sendResp(reqId, const [], '',
          error: 'invalid cursor', errorCode: 'invalid_arg');
    }
    final rawLimit = p['limit'];
    final limit = rawLimit is num ? rawLimit.toInt() : 0;
    final pageLimit = limit == 0 ? 50 : limit;
    if (pageLimit < 1 || pageLimit > 50) {
      return _sendResp(reqId, const [], '',
          error: 'invalid limit', errorCode: 'invalid_arg');
    }
    // Absent with_icons means true (labels+icons default).
    final withIcons = p['with_icons'] is bool ? p['with_icons'] as bool : true;
    final listPage = _listPage;
    try {
      final page = listPage != null
          ? await listPage(
              cursor: cursor, limit: pageLimit, withIcons: withIcons)
          : await _listener.listAppsPaged(
              cursor: cursor, limit: pageLimit, withIcons: withIcons);
      await _sendResp(reqId, page.entries, page.nextCursor);
    } catch (_) {
      await _sendResp(reqId, const [], '',
          error: 'inventory unavailable', errorCode: 'internal');
    }
  }

  Future<void> _sendResp(
    String reqId,
    List<Map<String, String>> entries,
    String nextCursor, {
    String? error,
    String? errorCode,
  }) async {
    final payload = <String, Object?>{
      'req_id': reqId,
      'entries': [
        for (final e in entries.take(50))
          {
            'package_name': e['package_name'] ?? '',
            'app': e['app'] ?? e['package_name'] ?? '',
            if ((e['app_icon_b64'] ?? '').isNotEmpty)
              'app_icon_b64': e['app_icon_b64'] ?? '',
          },
      ],
      'next_cursor': nextCursor,
      if (error != null && error.isNotEmpty) 'error': error,
      if (errorCode != null && errorCode.isNotEmpty) 'error_code': errorCode,
    };
    try {
      await sendFeature('notif-apps-resp', payload);
    } catch (_) {}
  }
}
