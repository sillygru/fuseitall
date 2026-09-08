// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/photos/photo_models.dart';

void main() {
  test('photo IDs accept digits, reject traversal', () {
    expect(isValidPhotoID('12345'), isTrue);
    expect(isValidPhotoID(''), isFalse);
    expect(isValidPhotoID('../x'), isFalse);
    expect(isValidPhotoID('a/b'), isFalse);
    expect(isValidPhotoID('.'), isFalse);
  });

  test('thumb sizes clamp range', () {
    expect(isValidThumbSize(0), isTrue);
    expect(isValidThumbSize(256), isTrue);
    expect(isValidThumbSize(63), isFalse);
    expect(isValidThumbSize(1025), isFalse);
  });

  test('photo entry round-trips', () {
    const e = PhotoEntry(photoId: '42', takenAt: 1700000000000, mime: 'image/jpeg');
    final back = PhotoEntry.fromJson(Map<String, dynamic>.from(e.toJson()));
    expect(back?.photoId, '42');
    expect(back?.takenAt, 1700000000000);
  });
}
