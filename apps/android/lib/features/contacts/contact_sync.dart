// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'dart:async';
import 'dart:math';

import 'package:flutter/services.dart';

import 'contact_store.dart';

/// Fresh 128-bit hex nonce (crypto randomness) for push dedup correlation.
String _freshContactNonce() {
  final rnd = Random.secure();
  final bytes = List<int>.generate(16, (_) => rnd.nextInt(256));
  return bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
}

class ContactSync {
  ContactSync({
    required this.store,
    required this.sendFeature,
    EventChannel? eventChannel,
  }) : _eventChannel = eventChannel ?? const EventChannel('fuseitall/contactsEvents');

  final ContactStore store;
  final Future<void> Function(String type, Map<String, Object?> payload) sendFeature;
  final EventChannel _eventChannel;
  StreamSubscription<dynamic>? _sub;

  void startEvents() {
    _sub?.cancel();
    _sub = _eventChannel.receiveBroadcastStream().listen((dynamic event) {
      if (event is Map) {
        final changedAt = (event['changed_at'] as num?)?.toInt() ?? DateTime.now().millisecondsSinceEpoch;
        _sendContactsChanged(changedAt);
      }
    }, onError: (_) {});
  }

  void stopEvents() {
    _sub?.cancel();
    _sub = null;
  }

  Future<void> _sendContactsChanged(int changedAt) async {
    try {
      await sendFeature('contacts-changed', {
        'nonce': _freshContactNonce(),
        'changed_at': changedAt,
      });
    } catch (_) {}
  }

  Future<bool> handleEvent(Map<String, dynamic> envelope) async {
    final type = envelope['type'] as String?;
    final payload = envelope['payload'];
    if (type == null || payload is! Map<String, dynamic>) return false;

    switch (type) {
      case 'contacts-list-req':
        await _handleList(payload);
        return true;
      case 'contact-avatar-req':
        await _handleAvatar(payload);
        return true;
      default:
        return false;
    }
  }

  Future<void> _handleList(Map<String, dynamic> p) async {
    final reqId = (p['req_id'] as String?)?.trim() ?? '';
    final reqNonce = (p['nonce'] as String?)?.trim() ?? '';
    final cursor = (p['cursor'] as String?)?.trim() ?? '';
    final query = (p['query'] as String?)?.trim() ?? '';
    var limit = p['limit'] is int ? p['limit'] as int : 50;
    if (limit < 1 || limit > 100) limit = 50;

    try {
      final res = await store.list(cursor: cursor, limit: limit, query: query);
      final jsonEntries = res.entries.map((e) => e.toJson()).toList();
      await _sendListResp(reqId, jsonEntries, res.nextCursor, totalCount: res.totalCount, nonce: reqNonce);
    } catch (e) {
      final msg = e.toString().toLowerCase();
      final isPerm = msg.contains('permission') || msg.contains('denied') || msg.contains('securityexception');
      await _sendListResp(
        reqId,
        [],
        '',
        nonce: reqNonce,
        error: isPerm ? 'Contacts permission needed — allow contacts in Settings.' : e.toString(),
        errorCode: isPerm ? 'permission_denied' : 'internal',
        permission: isPerm ? 'contacts' : '',
      );
    }
  }

  Future<void> _sendListResp(
    String reqId,
    List<Map<String, Object?>> entries,
    String nextCursor, {
    int totalCount = 0,
    String? nonce,
    String? error,
    String? errorCode,
    String? permission,
  }) async {
    final payload = <String, Object?>{
      'nonce': (nonce != null && nonce.isNotEmpty) ? nonce : _freshContactNonce(),
      'req_id': reqId,
      'entries': entries,
      'next_cursor': nextCursor,
      'total_count': totalCount,
      if (error != null && error.isNotEmpty) 'error': error,
      if (errorCode != null && errorCode.isNotEmpty) 'error_code': errorCode,
      if (permission != null && permission.isNotEmpty) 'permission': permission,
    };
    try {
      await sendFeature('contacts-list-resp', payload);
    } catch (_) {}
  }

  Future<void> _handleAvatar(Map<String, dynamic> p) async {
    final reqId = (p['req_id'] as String?)?.trim() ?? '';
    final reqNonce = (p['nonce'] as String?)?.trim() ?? '';
    final contactId = (p['contact_id'] as String?)?.trim() ?? '';
    if (reqId.isEmpty || contactId.isEmpty) return;

    try {
      final res = await store.getAvatar(contactId);
      final payload = <String, Object?>{
        'nonce': reqNonce.isNotEmpty ? reqNonce : _freshContactNonce(),
        'req_id': reqId,
        'contact_id': contactId,
        if (res != null) ...{
          'mime': res['mime'] ?? 'image/jpeg',
          'data_b64': res['data_b64'] ?? '',
          if ((res['photo_version'] ?? '').isNotEmpty) 'photo_version': res['photo_version'] ?? '',
        }
      };
      await sendFeature('contact-avatar-resp', payload);
    } catch (e) {
      await sendFeature('contact-avatar-resp', {
        'nonce': reqNonce.isNotEmpty ? reqNonce : _freshContactNonce(),
        'req_id': reqId,
        'contact_id': contactId,
        'error': e.toString(),
        'error_code': 'internal',
      });
    }
  }
}
