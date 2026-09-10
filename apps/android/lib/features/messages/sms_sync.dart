// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'dart:async';
import 'dart:math';

import 'package:flutter/services.dart';

import 'sms_models.dart';
import 'sms_store.dart';

/// Fresh 128-bit hex nonce (crypto randomness) for push dedup correlation.
String _freshSyncNonce() {
  final rnd = Random.secure();
  final bytes = List<int>.generate(16, (_) => rnd.nextInt(256));
  return bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
}

class SmsSync {
  SmsSync({
    required this.store,
    required this.sendFeature,
    EventChannel? eventChannel,
  }) : _eventChannel = eventChannel ?? const EventChannel('fuseitall/smsEvents');

  final SmsStore store;
  final Future<void> Function(String type, Map<String, Object?> payload) sendFeature;
  final EventChannel _eventChannel;
  StreamSubscription<dynamic>? _sub;

  void startEvents() {
    _sub?.cancel();
    _sub = _eventChannel.receiveBroadcastStream().listen((dynamic event) {
      if (event is Map) {
        final eventType = event['type'] as String?;
        if (eventType == 'push') {
          final rawMsg = event['message'];
          if (rawMsg is Map) {
            final msg = SMSMessage.fromJson(Map<String, dynamic>.from(rawMsg));
            final name = event['contact_name'] as String?;
            final contactId = event['contact_id'] as String?;
            final photoVersion = event['photo_version'] as String?;
            final clientId = event['client_id'] as String?;
            final seq = (event['seq'] as num?)?.toInt() ?? 0;
            _sendSmsPush(msg,
                contactName: name,
                contactId: contactId,
                photoVersion: photoVersion,
                clientId: clientId,
                seq: seq);
          }
        } else if (eventType == 'changed') {
          final changedAt = (event['changed_at'] as num?)?.toInt() ?? DateTime.now().millisecondsSinceEpoch;
          _sendSmsChanged(changedAt);
        }
      }
    }, onError: (_) {});
  }

  void stopEvents() {
    _sub?.cancel();
    _sub = null;
  }

  Future<void> _sendSmsPush(SMSMessage msg,
      {String? contactName, String? contactId, String? photoVersion, String? clientId, int seq = 0}) async {
    try {
      await sendFeature('sms-push', {
        'nonce': _freshSyncNonce(),
        'message': msg.toJson(),
        if (contactName != null && contactName.isNotEmpty) 'contact_name': contactName,
        if (contactId != null && contactId.isNotEmpty) 'contact_id': contactId,
        if (photoVersion != null && photoVersion.isNotEmpty) 'photo_version': photoVersion,
        if (clientId != null && clientId.isNotEmpty) 'client_id': clientId,
        if (seq != 0) 'seq': seq,
      });
    } catch (_) {}
  }

  Future<void> _sendSmsChanged(int changedAt) async {
    try {
      await sendFeature('sms-changed', {
        'nonce': _freshSyncNonce(),
        'changed_at': changedAt,
      });
    } catch (_) {}
  }

  Future<bool> handleEvent(Map<String, dynamic> envelope) async {
    final type = envelope['type'] as String?;
    final payload = envelope['payload'];
    if (type == null || payload is! Map<String, dynamic>) return false;

    switch (type) {
      case 'sms-threads-req':
        await _handleThreads(payload);
        return true;
      case 'sms-messages-req':
        await _handleMessages(payload);
        return true;
      case 'sms-send-req':
        await _handleSend(payload);
        return true;
      default:
        return false;
    }
  }

  Future<void> _handleThreads(Map<String, dynamic> p) async {
    final reqId = (p['req_id'] as String?)?.trim() ?? '';
    final reqNonce = (p['nonce'] as String?)?.trim() ?? '';
    final cursor = (p['cursor'] as String?)?.trim() ?? '';
    var limit = p['limit'] is int ? p['limit'] as int : 50;
    if (limit < 1 || limit > 100) limit = 50;

    try {
      final res = await store.queryThreads(cursor: cursor, limit: limit);
      final jsonThreads = res.threads.map((t) => t.toJson()).toList();
      await _sendThreadsResp(reqId, jsonThreads, res.nextCursor, nonce: reqNonce);
    } catch (e) {
      final msg = e.toString().toLowerCase();
      final isPerm = msg.contains('permission') || msg.contains('denied') || msg.contains('securityexception');
      await _sendThreadsResp(
        reqId,
        [],
        '',
        nonce: reqNonce,
        error: isPerm ? 'SMS permission needed — allow SMS in Settings.' : e.toString(),
        errorCode: isPerm ? 'permission_denied' : 'internal',
        permission: isPerm ? 'sms' : '',
      );
    }
  }

  Future<void> _sendThreadsResp(
    String reqId,
    List<Map<String, Object?>> threads,
    String nextCursor, {
    String? nonce,
    String? error,
    String? errorCode,
    String? permission,
  }) async {
    final payload = <String, Object?>{
      'nonce': (nonce != null && nonce.isNotEmpty) ? nonce : _freshSyncNonce(),
      'req_id': reqId,
      'threads': threads,
      'next_cursor': nextCursor,
      if (error != null && error.isNotEmpty) 'error': error,
      if (errorCode != null && errorCode.isNotEmpty) 'error_code': errorCode,
      if (permission != null && permission.isNotEmpty) 'permission': permission,
    };
    try {
      await sendFeature('sms-threads-resp', payload);
    } catch (_) {}
  }

  Future<void> _handleMessages(Map<String, dynamic> p) async {
    final reqId = (p['req_id'] as String?)?.trim() ?? '';
    final reqNonce = (p['nonce'] as String?)?.trim() ?? '';
    final threadId = (p['thread_id'] as num?)?.toInt() ?? 0;
    final cursor = (p['cursor'] as String?)?.trim() ?? '';
    var limit = p['limit'] is int ? p['limit'] as int : 50;
    if (limit < 1 || limit > 100) limit = 50;

    try {
      final res = await store.queryMessages(threadId: threadId, cursor: cursor, limit: limit);
      final jsonMessages = res.messages.map((m) => m.toJson()).toList();
      await _sendMessagesResp(reqId, threadId, jsonMessages, res.nextCursor, nonce: reqNonce);
    } catch (e) {
      final msg = e.toString().toLowerCase();
      final isPerm = msg.contains('permission') || msg.contains('denied') || msg.contains('securityexception');
      await _sendMessagesResp(
        reqId,
        threadId,
        [],
        '',
        nonce: reqNonce,
        error: isPerm ? 'SMS permission needed — allow SMS in Settings.' : e.toString(),
        errorCode: isPerm ? 'permission_denied' : 'internal',
        permission: isPerm ? 'sms' : '',
      );
    }
  }

  Future<void> _sendMessagesResp(
    String reqId,
    int threadId,
    List<Map<String, Object?>> messages,
    String nextCursor, {
    String? nonce,
    String? error,
    String? errorCode,
    String? permission,
  }) async {
    final payload = <String, Object?>{
      'nonce': (nonce != null && nonce.isNotEmpty) ? nonce : _freshSyncNonce(),
      'req_id': reqId,
      'thread_id': threadId,
      'messages': messages,
      'next_cursor': nextCursor,
      if (error != null && error.isNotEmpty) 'error': error,
      if (errorCode != null && errorCode.isNotEmpty) 'error_code': errorCode,
      if (permission != null && permission.isNotEmpty) 'permission': permission,
    };
    try {
      await sendFeature('sms-messages-resp', payload);
    } catch (_) {}
  }

  Future<void> _handleSend(Map<String, dynamic> p) async {
    final reqId = (p['req_id'] as String?)?.trim() ?? '';
    final reqNonce = (p['nonce'] as String?)?.trim() ?? '';
    final recipient = (p['recipient'] as String?)?.trim() ?? '';
    final body = (p['body'] as String?)?.trim() ?? '';
    final clientId = (p['client_id'] as String?)?.trim() ?? '';
    final subId = (p['sub_id'] as String?)?.trim() ?? '';

    if (recipient.isEmpty || body.isEmpty) {
      await _sendSendResp(reqId, clientId, ok: false, nonce: reqNonce, error: 'recipient and body required', errorCode: 'invalid_arg');
      return;
    }

    try {
      final res = await store.sendSms(recipient: recipient, body: body, clientId: clientId, subId: subId);
      await _sendSendResp(
        reqId,
        clientId,
        ok: res.ok,
        nonce: reqNonce,
        messageId: res.messageId,
        threadId: res.threadId,
        error: res.error,
        errorCode: res.errorCode,
      );
    } catch (e) {
      final msg = e.toString().toLowerCase();
      final isPerm = msg.contains('permission') || msg.contains('denied') || msg.contains('securityexception');
      await _sendSendResp(
        reqId,
        clientId,
        ok: false,
        nonce: reqNonce,
        error: isPerm ? 'SMS send permission needed — grant SEND_SMS.' : e.toString(),
        errorCode: isPerm ? 'permission_denied' : 'send_failed',
        permission: isPerm ? 'sms' : '',
      );
    }
  }

  Future<void> _sendSendResp(
    String reqId,
    String clientId, {
    required bool ok,
    String? nonce,
    int? messageId,
    int? threadId,
    String? error,
    String? errorCode,
    String? permission,
  }) async {
    final payload = <String, Object?>{
      'nonce': (nonce != null && nonce.isNotEmpty) ? nonce : _freshSyncNonce(),
      'req_id': reqId,
      'client_id': clientId,
      'ok': ok,
      'message_id': ?messageId,
      'thread_id': ?threadId,
      if (error != null && error.isNotEmpty) 'error': error,
      if (errorCode != null && errorCode.isNotEmpty) 'error_code': errorCode,
      if (permission != null && permission.isNotEmpty) 'permission': permission,
    };
    try {
      await sendFeature('sms-send-resp', payload);
    } catch (_) {}
  }
}
