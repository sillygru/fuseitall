// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter/services.dart';

import 'contact_models.dart';

class ContactStore {
  ContactStore({MethodChannel? channel})
      : _channel = channel ?? const MethodChannel('fuseitall/contacts');

  final MethodChannel _channel;

  Future<ContactsListResult> list({
    String cursor = '',
    int limit = 50,
    String query = '',
  }) async {
    final res = await _channel.invokeMethod<Map<Object?, Object?>>('queryContacts', {
      'cursor': cursor,
      'limit': limit,
      'query': query,
    });
    if (res == null) return const ContactsListResult(entries: [], nextCursor: '');

    final rawEntries = res['entries'];
    final entries = <ContactEntry>[];
    if (rawEntries is List) {
      for (final e in rawEntries) {
        if (e is Map) {
          entries.add(ContactEntry.fromJson(Map<String, dynamic>.from(e)));
        }
      }
    }
    final nextCursor = (res['next_cursor'] as String?) ?? '';
    final totalCount = (res['total_count'] as int?) ?? entries.length;

    return ContactsListResult(
      entries: entries,
      nextCursor: nextCursor,
      totalCount: totalCount,
    );
  }

  Future<Map<String, String>?> getAvatar(String contactId, {bool highRes = false}) async {
    final res = await _channel.invokeMethod<Map<Object?, Object?>>('getContactAvatar', {
      'contact_id': contactId,
      'high_res': highRes,
    });
    if (res == null) return null;
    final dataB64 = (res['data_b64'] as String?) ?? '';
    if (dataB64.isEmpty) return null;
    return {
      'mime': (res['mime'] as String?) ?? 'image/jpeg',
      'data_b64': dataB64,
      'photo_version': (res['photo_version'] as String?) ?? '',
    };
  }
}
