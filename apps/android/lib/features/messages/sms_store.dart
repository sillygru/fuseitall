// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

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
}
