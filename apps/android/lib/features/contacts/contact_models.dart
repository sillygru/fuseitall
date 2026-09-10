// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

class ContactPhone {
  const ContactPhone({
    required this.number,
    this.type = 'mobile',
    this.label,
    this.isPrimary = false,
  });

  final String number;
  final String type;
  final String? label;
  final bool isPrimary;

  Map<String, Object?> toJson() => {
    'number': number,
    'type': type,
    if (label != null && label!.isNotEmpty) 'label': label,
    if (isPrimary) 'is_primary': true,
  };

  factory ContactPhone.fromJson(Map<String, dynamic> json) {
    return ContactPhone(
      number: json['number'] as String? ?? '',
      type: json['type'] as String? ?? 'mobile',
      label: json['label'] as String?,
      isPrimary: json['is_primary'] as bool? ?? false,
    );
  }
}

class ContactEmail {
  const ContactEmail({
    required this.address,
    this.type = 'other',
    this.label,
  });

  final String address;
  final String type;
  final String? label;

  Map<String, Object?> toJson() => {
    'address': address,
    'type': type,
    if (label != null && label!.isNotEmpty) 'label': label,
  };

  factory ContactEmail.fromJson(Map<String, dynamic> json) {
    return ContactEmail(
      address: json['address'] as String? ?? '',
      type: json['type'] as String? ?? 'other',
      label: json['label'] as String?,
    );
  }
}

class ContactEntry {
  const ContactEntry({
    required this.contactId,
    required this.displayName,
    this.phones = const [],
    this.emails = const [],
    this.avatarB64,
    this.starred = false,
  });

  final String contactId;
  final String displayName;
  final List<ContactPhone> phones;
  final List<ContactEmail> emails;
  final String? avatarB64;
  final bool starred;

  Map<String, Object?> toJson() => {
    'contact_id': contactId,
    'display_name': displayName,
    if (phones.isNotEmpty) 'phones': phones.map((p) => p.toJson()).toList(),
    if (emails.isNotEmpty) 'emails': emails.map((e) => e.toJson()).toList(),
    if (avatarB64 != null && avatarB64!.isNotEmpty) 'avatar_b64': avatarB64,
    if (starred) 'starred': true,
  };

  factory ContactEntry.fromJson(Map<String, dynamic> json) {
    final rawPhones = json['phones'];
    final phoneList = <ContactPhone>[];
    if (rawPhones is List) {
      for (final p in rawPhones) {
        if (p is Map<String, dynamic>) {
          phoneList.add(ContactPhone.fromJson(p));
        }
      }
    }

    final rawEmails = json['emails'];
    final emailList = <ContactEmail>[];
    if (rawEmails is List) {
      for (final e in rawEmails) {
        if (e is Map<String, dynamic>) {
          emailList.add(ContactEmail.fromJson(e));
        }
      }
    }

    return ContactEntry(
      contactId: json['contact_id'] as String? ?? '',
      displayName: json['display_name'] as String? ?? '',
      phones: phoneList,
      emails: emailList,
      avatarB64: json['avatar_b64'] as String?,
      starred: json['starred'] as bool? ?? false,
    );
  }
}

class ContactsListResult {
  const ContactsListResult({
    required this.entries,
    required this.nextCursor,
    this.totalCount = 0,
  });

  final List<ContactEntry> entries;
  final String nextCursor;
  final int totalCount;
}
