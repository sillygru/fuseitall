// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'dart:async';

import 'package:flutter/services.dart';

import 'sms_models.dart';
import 'sms_store.dart';

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
            _sendSmsPush(msg, name);
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

  Future<void> _sendSmsPush(SMSMessage msg, String? contactName) async {
    try {
      await sendFeature('sms-push', {
        'message': msg.toJson(),
        if (contactName != null && contactName.isNotEmpty) 'contact_name': contactName,
      });
    } catch (_) {}
  }

  Future<void> _sendSmsChanged(int changedAt) async {
    try {
      await sendFeature('sms-changed', {
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
    final cursor = (p['cursor'] as String?)?.trim() ?? '';
    var limit = p['limit'] is int ? p['limit'] as int : 50;
    if (limit < 1 || limit > 100) limit = 50;

    try {
      final res = await store.queryThreads(cursor: cursor, limit: limit);
      final jsonThreads = res.threads.map((t) => t.toJson()).toList();
      await _sendThreadsResp(reqId, jsonThreads, res.nextCursor);
    } catch (e) {
      final msg = e.toString().toLowerCase();
      final isPerm = msg.contains('permission') || msg.contains('denied') || msg.contains('securityexception');
      await _sendThreadsResp(
        reqId,
        [],
        '',
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
    String? error,
    String? errorCode,
    String? permission,
  }) async {
    final payload = <String, Object?>{
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
    final threadId = (p['thread_id'] as num?)?.toInt() ?? 0;
    final cursor = (p['cursor'] as String?)?.trim() ?? '';
    var limit = p['limit'] is int ? p['limit'] as int : 50;
    if (limit < 1 || limit > 100) limit = 50;

    try {
      final res = await store.queryMessages(threadId: threadId, cursor: cursor, limit: limit);
      final jsonMessages = res.messages.map((m) => m.toJson()).toList();
      await _sendMessagesResp(reqId, threadId, jsonMessages, res.nextCursor);
    } catch (e) {
      final msg = e.toString().toLowerCase();
      final isPerm = msg.contains('permission') || msg.contains('denied') || msg.contains('securityexception');
      await _sendMessagesResp(
        reqId,
        threadId,
        [],
        '',
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
    String? error,
    String? errorCode,
    String? permission,
  }) async {
    final payload = <String, Object?>{
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
    final recipient = (p['recipient'] as String?)?.trim() ?? '';
    final body = (p['body'] as String?)?.trim() ?? '';
    final clientId = (p['client_id'] as String?)?.trim() ?? '';

    if (recipient.isEmpty || body.isEmpty) {
      await _sendSendResp(reqId, clientId, ok: false, error: 'recipient and body required', errorCode: 'invalid_arg');
      return;
    }

    try {
      final res = await store.sendSms(recipient: recipient, body: body, clientId: clientId);
      await _sendSendResp(
        reqId,
        clientId,
        ok: res.ok,
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
    int? messageId,
    int? threadId,
    String? error,
    String? errorCode,
    String? permission,
  }) async {
    final payload = <String, Object?>{
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
