// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/contacts/contact_models.dart';

void main() {
  test('contact entry serialization and round-trip', () {
    const entry = ContactEntry(
      contactId: '101',
      displayName: 'Alice Bob',
      phones: [
        ContactPhone(number: '+15551234567', type: 'mobile', isPrimary: true),
      ],
      emails: [
        ContactEmail(address: 'alice@example.com', type: 'home'),
      ],
      starred: true,
      avatarB64: 'abc',
    );

    final json = entry.toJson();
    final back = ContactEntry.fromJson(json);

    expect(back.contactId, '101');
    expect(back.displayName, 'Alice Bob');
    expect(back.phones.length, 1);
    expect(back.phones.first.number, '+15551234567');
    expect(back.emails.first.address, 'alice@example.com');
    expect(back.starred, isTrue);
    expect(back.avatarB64, 'abc');
  });

  test('contact entry handles malformed sub-lists safely', () {
    final valid = ContactEntry.fromJson({
      'contact_id': '999',
      'display_name': 'Jane',
      'phones': [
        {'number': '12345', 'type': 'mobile'},
        'not a map',
      ],
      'emails': [
        {'address': 'jane@example.com'},
        null,
      ],
    });
    expect(valid.contactId, '999');
    expect(valid.displayName, 'Jane');
    expect(valid.phones.length, 1);
    expect(valid.emails.length, 1);
  });
}
