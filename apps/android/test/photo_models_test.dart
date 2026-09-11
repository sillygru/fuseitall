// SPDX-License-Identifier: AGPL-3.0-only

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

  test('video entry round-trips with duration', () {
    const e = PhotoEntry(
      photoId: 'vid:42',
      takenAt: 1700000000000,
      mime: 'video/mp4',
      size: 1 << 20,
      mediaType: kMediaTypeVideo,
      durationMs: 61000,
    );
    expect(e.isVideo, isTrue);
    final back = PhotoEntry.fromJson(Map<String, dynamic>.from(e.toJson()));
    expect(back?.photoId, 'vid:42');
    expect(back?.mediaType, kMediaTypeVideo);
    expect(back?.durationMs, 61000);
    expect(back?.isVideo, isTrue);
  });

  test('legacy entry without media_type defaults to photo', () {
    final back = PhotoEntry.fromJson({'photo_id': '1', 'taken_at': 1});
    expect(back?.mediaType, kMediaTypePhoto);
    expect(back?.isVideo, isFalse);
  });

  test('parsePhotoID splits namespaced IDs', () {
    expect(parsePhotoID('12345')?.kind, kMediaTypePhoto);
    expect(parsePhotoID('12345')?.row, '12345');
    expect(parsePhotoID('img:7')?.kind, kMediaTypePhoto);
    expect(parsePhotoID('vid:9')?.kind, kMediaTypeVideo);
    expect(parsePhotoID('vid:9')?.row, '9');
    expect(parsePhotoID('vid:'), isNull);
    expect(parsePhotoID('vid:a/b'), isNull);
    expect(parsePhotoID(''), isNull);
  });

  test('photo ranges validate bounds', () {
    expect(isValidPhotoRange(offset: 0, length: 0), isTrue);
    expect(isValidPhotoRange(offset: 1 << 20, length: 1 << 20), isTrue);
    expect(isValidPhotoRange(offset: -1, length: 0), isFalse);
    expect(isValidPhotoRange(offset: 0, length: -1), isFalse);
    expect(isValidPhotoRange(offset: 0, length: kMaxPhotoRangeLen + 1), isFalse);
  });
}
