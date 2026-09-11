// SPDX-License-Identifier: AGPL-3.0-only

import 'package:flutter/services.dart';

import 'sms_models.dart';

class SmsStore {
  SmsStore({MethodChannel? channel})
      : _channel = channel ?? const MethodChannel('fuseitall/sms');

  final MethodChannel _channel;

  Future<SMSThreadsResult> queryThreads({
    String cursor = '',
    int limit = 50,
  }) async {
    final res = await _channel.invokeMethod<Map<Object?, Object?>>('queryThreads', {
      'cursor': cursor,
      'limit': limit,
    });
    if (res == null) return const SMSThreadsResult(threads: [], nextCursor: '');

    final rawThreads = res['threads'];
    final threads = <SMSThread>[];
    if (rawThreads is List) {
      for (final t in rawThreads) {
        if (t is Map) {
          threads.add(SMSThread.fromJson(Map<String, dynamic>.from(t)));
        }
      }
    }
    final nextCursor = (res['next_cursor'] as String?) ?? '';
    return SMSThreadsResult(threads: threads, nextCursor: nextCursor);
  }

  Future<SMSMessagesResult> queryMessages({
    required int threadId,
    String cursor = '',
    int limit = 50,
  }) async {
    final res = await _channel.invokeMethod<Map<Object?, Object?>>('queryMessages', {
      'thread_id': threadId,
      'cursor': cursor,
      'limit': limit,
    });
    if (res == null) {
      return SMSMessagesResult(threadId: threadId, messages: [], nextCursor: '');
    }

    final rawMessages = res['messages'];
    final messages = <SMSMessage>[];
    if (rawMessages is List) {
      for (final m in rawMessages) {
        if (m is Map) {
          messages.add(SMSMessage.fromJson(Map<String, dynamic>.from(m)));
        }
      }
    }
    final nextCursor = (res['next_cursor'] as String?) ?? '';
    return SMSMessagesResult(
      threadId: threadId,
      messages: messages,
      nextCursor: nextCursor,
    );
  }

  Future<SMSSendResult> sendSms({
    required String recipient,
    required String body,
    required String clientId,
    String subId = '',
  }) async {
    final res = await _channel.invokeMethod<Map<Object?, Object?>>('sendSms', {
      'recipient': recipient,
      'body': body,
      'client_id': clientId,
      if (subId.isNotEmpty) 'sub_id': subId,
    });
    if (res == null) {
      return SMSSendResult(ok: false, clientId: clientId, error: 'unknown failure');
    }
    final ok = res['ok'] as bool? ?? false;
    final messageId = (res['message_id'] as num?)?.toInt();
    final threadId = (res['thread_id'] as num?)?.toInt();
    return SMSSendResult(
      ok: ok,
      clientId: clientId,
      messageId: messageId,
      threadId: threadId,
    );
  }

  Future<bool> markRead({
    required int threadId,
    int messageId = 0,
    String address = '',
  }) async {
    final res = await _channel.invokeMethod<Map<Object?, Object?>>('markRead', {
      'thread_id': threadId,
      'message_id': messageId,
      if (address.isNotEmpty) 'address': address,
    });
    if (res == null) return false;
    return (res['ok'] as bool?) ?? false;
  }
}
