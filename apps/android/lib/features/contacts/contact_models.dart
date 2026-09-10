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
    this.normalizedNumber,
  });

  final String number;
  final String type;
  final String? label;
  final bool isPrimary;
  final String? normalizedNumber;

  Map<String, Object?> toJson() => {
    'number': number,
    'type': type,
    if (label != null && label!.isNotEmpty) 'label': label,
    if (isPrimary) 'is_primary': true,
    if (normalizedNumber != null && normalizedNumber!.isNotEmpty)
      'normalized_number': normalizedNumber,
  };

  factory ContactPhone.fromJson(Map<String, dynamic> json) {
    return ContactPhone(
      number: json['number'] as String? ?? '',
      type: json['type'] as String? ?? 'mobile',
      label: json['label'] as String?,
      isPrimary: json['is_primary'] as bool? ?? false,
      normalizedNumber: json['normalized_number'] as String?,
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

class ContactOrganization {
  const ContactOrganization({this.company, this.title, this.department});

  final String? company;
  final String? title;
  final String? department;

  Map<String, Object?> toJson() => {
    if (company != null && company!.isNotEmpty) 'company': company,
    if (title != null && title!.isNotEmpty) 'title': title,
    if (department != null && department!.isNotEmpty) 'department': department,
  };

  factory ContactOrganization.fromJson(Map<String, dynamic> json) {
    return ContactOrganization(
      company: json['company'] as String?,
      title: json['title'] as String?,
      department: json['department'] as String?,
    );
  }
}

class ContactPostal {
  const ContactPostal({this.formatted, this.type});

  final String? formatted;
  final String? type;

  Map<String, Object?> toJson() => {
    if (formatted != null && formatted!.isNotEmpty) 'formatted': formatted,
    if (type != null && type!.isNotEmpty) 'type': type,
  };

  factory ContactPostal.fromJson(Map<String, dynamic> json) {
    return ContactPostal(
      formatted: json['formatted'] as String?,
      type: json['type'] as String?,
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
    this.lookupKey,
    this.lastUpdatedMs = 0,
    this.photoVersion,
    this.photoUri,
    this.birthdayMs = 0,
    this.anniversaryMs = 0,
    this.nickname,
    this.note,
    this.website,
    this.organization,
    this.postal,
  });

  final String contactId;
  final String displayName;
  final List<ContactPhone> phones;
  final List<ContactEmail> emails;
  final String? avatarB64;
  final bool starred;
  final String? lookupKey;
  final int lastUpdatedMs;
  final String? photoVersion;
  final String? photoUri;
  final int birthdayMs;
  final int anniversaryMs;
  final String? nickname;
  final String? note;
  final String? website;
  final ContactOrganization? organization;
  final ContactPostal? postal;

  Map<String, Object?> toJson() => {
    'contact_id': contactId,
    'display_name': displayName,
    if (phones.isNotEmpty) 'phones': phones.map((p) => p.toJson()).toList(),
    if (emails.isNotEmpty) 'emails': emails.map((e) => e.toJson()).toList(),
    if (avatarB64 != null && avatarB64!.isNotEmpty) 'avatar_b64': avatarB64,
    if (starred) 'starred': true,
    if (lookupKey != null && lookupKey!.isNotEmpty) 'lookup_key': lookupKey,
    if (lastUpdatedMs > 0) 'last_updated_ms': lastUpdatedMs,
    if (photoVersion != null && photoVersion!.isNotEmpty) 'photo_version': photoVersion,
    if (photoUri != null && photoUri!.isNotEmpty) 'photo_uri': photoUri,
    if (birthdayMs > 0) 'birthday_ms': birthdayMs,
    if (anniversaryMs > 0) 'anniversary_ms': anniversaryMs,
    if (nickname != null && nickname!.isNotEmpty) 'nickname': nickname,
    if (note != null && note!.isNotEmpty) 'note': note,
    if (website != null && website!.isNotEmpty) 'website': website,
    if (organization != null) 'organization': organization!.toJson(),
    if (postal != null) 'postal': postal!.toJson(),
  };

  factory ContactEntry.fromJson(Map<String, dynamic> json) {
    // MethodChannel decodes nested maps as Map<Object?, Object?>, not
    // Map<String, dynamic> — a strict `is Map<String, dynamic>` guard silently
    // drops every phone/email on real device data (names/photos survived,
    // lists arrived empty). Accept any Map like organization/postal below.
    final rawPhones = json['phones'];
    final phoneList = <ContactPhone>[];
    if (rawPhones is List) {
      for (final p in rawPhones) {
        if (p is Map) {
          phoneList.add(ContactPhone.fromJson(Map<String, dynamic>.from(p)));
        }
      }
    }

    final rawEmails = json['emails'];
    final emailList = <ContactEmail>[];
    if (rawEmails is List) {
      for (final e in rawEmails) {
        if (e is Map) {
          emailList.add(ContactEmail.fromJson(Map<String, dynamic>.from(e)));
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
      lookupKey: json['lookup_key'] as String?,
      lastUpdatedMs: (json['last_updated_ms'] as num?)?.toInt() ?? 0,
      photoVersion: json['photo_version'] as String?,
      photoUri: json['photo_uri'] as String?,
      birthdayMs: (json['birthday_ms'] as num?)?.toInt() ?? 0,
      anniversaryMs: (json['anniversary_ms'] as num?)?.toInt() ?? 0,
      nickname: json['nickname'] as String?,
      note: json['note'] as String?,
      website: json['website'] as String?,
      organization: json['organization'] is Map
          ? ContactOrganization.fromJson(Map<String, dynamic>.from(json['organization'] as Map))
          : null,
      postal: json['postal'] is Map
          ? ContactPostal.fromJson(Map<String, dynamic>.from(json['postal'] as Map))
          : null,
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
