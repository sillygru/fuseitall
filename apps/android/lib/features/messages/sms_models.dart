// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

class SMSThread {
  const SMSThread({
    required this.threadId,
    required this.address,
    this.contactName,
    this.snippet = '',
    required this.date,
    this.messageCount = 0,
    this.unreadCount = 0,
    this.read = true,
  });

  final int threadId;
  final String address;
  final String? contactName;
  final String snippet;
  final int date;
  final int messageCount;
  final int unreadCount;
  final bool read;

  Map<String, Object?> toJson() => {
    'thread_id': threadId,
    'address': address,
    if (contactName != null && contactName!.isNotEmpty) 'contact_name': contactName,
    'snippet': snippet,
    'date': date,
    'message_count': messageCount,
    'unread_count': unreadCount,
    'read': read,
  };

  factory SMSThread.fromJson(Map<String, dynamic> json) {
    return SMSThread(
      threadId: (json['thread_id'] as num?)?.toInt() ?? 0,
      address: json['address'] as String? ?? '',
      contactName: json['contact_name'] as String?,
      snippet: json['snippet'] as String? ?? '',
      date: (json['date'] as num?)?.toInt() ?? 0,
      messageCount: (json['message_count'] as num?)?.toInt() ?? 0,
      unreadCount: (json['unread_count'] as num?)?.toInt() ?? 0,
      read: json['read'] as bool? ?? true,
    );
  }
}

class SMSMessage {
  const SMSMessage({
    required this.id,
    required this.threadId,
    required this.address,
    required this.body,
    required this.date,
    required this.type,
    this.read = true,
    this.status = -1,
  });

  final int id;
  final int threadId;
  final String address;
  final String body;
  final int date;
  final int type;
  final bool read;
  final int status;

  Map<String, Object?> toJson() => {
    'id': id,
    'thread_id': threadId,
    'address': address,
    'body': body,
    'date': date,
    'type': type,
    'read': read,
    if (status >= 0) 'status': status,
  };

  factory SMSMessage.fromJson(Map<String, dynamic> json) {
    return SMSMessage(
      id: (json['id'] as num?)?.toInt() ?? 0,
      threadId: (json['thread_id'] as num?)?.toInt() ?? 0,
      address: json['address'] as String? ?? '',
      body: json['body'] as String? ?? '',
      date: (json['date'] as num?)?.toInt() ?? 0,
      type: (json['type'] as num?)?.toInt() ?? 1,
      read: json['read'] as bool? ?? true,
      status: (json['status'] as num?)?.toInt() ?? -1,
    );
  }
}

class SMSThreadsResult {
  const SMSThreadsResult({
    required this.threads,
    required this.nextCursor,
  });

  final List<SMSThread> threads;
  final String nextCursor;
}

class SMSMessagesResult {
  const SMSMessagesResult({
    required this.threadId,
    required this.messages,
    required this.nextCursor,
  });

  final int threadId;
  final List<SMSMessage> messages;
  final String nextCursor;
}

class SMSSendResult {
  const SMSSendResult({
    required this.ok,
    required this.clientId,
    this.messageId,
    this.threadId,
    this.error,
    this.errorCode,
  });

  final bool ok;
  final String clientId;
  final int? messageId;
  final int? threadId;
  final String? error;
  final String? errorCode;
}
