// SPDX-License-Identifier: AGPL-3.0-only

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

  test('contact entry handles malformed sub-lists safely', () {    final valid = ContactEntry.fromJson({
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

  test('contact extended fields round-trip and ignore unknown', () {    final back = ContactEntry.fromJson({
      'contact_id': '7',
      'display_name': 'Extended',
      'photo_version': '1:2:3',
      'birthday_ms': 631152000000,
      'nickname': 'Ext',
      'organization': {'company': 'Acme', 'title': 'Eng'},
      'postal': {'formatted': '1 Main St', 'type': 'home'},
      'note': 'hello',
      'website': 'https://example.com',
      'future_field': 'ignored',
    });
    expect(back.photoVersion, '1:2:3');
    expect(back.birthdayMs, 631152000000);
    expect(back.nickname, 'Ext');
    expect(back.organization?.company, 'Acme');
    expect(back.postal?.formatted, '1 Main St');
    expect(back.note, 'hello');
    expect(back.toJson()['photo_version'], '1:2:3');
  });

  test('contact entry parses MethodChannel-typed nested maps', () {
    // Regression: the standard method codec decodes nested maps as
    // Map<Object?, Object?>, not Map<String, dynamic>. A strict guard dropped
    // every phone/email on device data while literals in tests still passed.
    final mcStyle = Map<String, dynamic>.from({
      'contact_id': '1',
      'display_name': 'Device Contact',
      'phones': [
        Map<Object?, Object?>.from({
          'number': '+15551234567',
          'type': 'mobile',
          'is_primary': true,
          'normalized_number': '+15551234567',
        }),
      ],
      'emails': [
        Map<Object?, Object?>.from({'address': 'dev@example.com', 'type': 'home'}),
      ],
      'organization': Map<Object?, Object?>.from({'company': 'Acme'}),
    });
    final e = ContactEntry.fromJson(mcStyle);
    expect(e.phones.length, 1);
    expect(e.phones.first.number, '+15551234567');
    expect(e.phones.first.isPrimary, isTrue);
    expect(e.phones.first.normalizedNumber, '+15551234567');
    expect(e.emails.length, 1);
    expect(e.emails.first.address, 'dev@example.com');
    expect(e.organization?.company, 'Acme');
    // Round-trip back onto the wire keeps both lists.
    final wire = e.toJson();
    expect((wire['phones'] as List).length, 1);
    expect((wire['emails'] as List).length, 1);
  });
}
