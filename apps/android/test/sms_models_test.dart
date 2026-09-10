// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/messages/sms_models.dart';

void main() {
  test('thread entry serialization and round-trip', () {
    const t = SMSThread(
      threadId: 1,
      snippet: 'Hello there',
      date: 1700000000000,
      messageCount: 5,
      read: true,
      address: '+15551234567',
    );
    final json = t.toJson();
    final back = SMSThread.fromJson(json);
    expect(back.threadId, 1);
    expect(back.snippet, 'Hello there');
    expect(back.address, '+15551234567');
  });

  test('message entry serialization and round-trip', () {
    const m = SMSMessage(
      id: 42,
      threadId: 1,
      address: '+15551234567',
      body: 'Testing message',
      date: 1700000000000,
      type: 1, // received
      read: true,
      status: 0,
    );
    final json = m.toJson();
    final back = SMSMessage.fromJson(json);
    expect(back.id, 42);
    expect(back.type, 1);
    expect(back.body, 'Testing message');
  });
}
