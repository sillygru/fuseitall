// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Photo models mirroring packages/proto/photos.json and packages/core/photos.go.
// Pure helpers: sanitizers, caps, explicit maps for protocol stability.

const kMaxPhotoIDLen = 128;
const kMaxPhotosPerList = 200;
const kMaxPhotoThumbB64Len = 2000000;
const kMaxPhotoDeleteBatch = 200;
const kMaxPhotoChunkRaw = 1 << 20;
const kMinPhotoThumbSize = 64;
const kMaxPhotoThumbSize = 1024;
const kDefaultPhotoLimit = 100;
const kMaxVideoDurationMs = 24 * 3600 * 1000;
const kMaxPhotoRangeLen = 32 << 20;
const kMaxPhotoTotalSize = 8 << 30; // 8 GiB, mirrors core MaxFileTotalSize

const kMediaTypePhoto = 'photo';
const kMediaTypeVideo = 'video';

/// One photo or video in a library listing.
class PhotoEntry {
  const PhotoEntry({
    required this.photoId,
    required this.takenAt,
    this.width = 0,
    this.height = 0,
    this.mime = '',
    this.size = 0,
    this.orientation = 0,
    this.mediaType = kMediaTypePhoto,
    this.durationMs = 0,
  });

  final String photoId;
  final int takenAt; // unix millis
  final int width;
  final int height;
  final String mime;
  final int size;
  final int orientation;
  final String mediaType; // photo | video (absent on wire = photo)
  final int durationMs; // video millis, 0 = unknown

  bool get isVideo => mediaType == kMediaTypeVideo;

  Map<String, Object?> toJson() => {
        'photo_id': photoId,
        'taken_at': takenAt,
        if (width != 0) 'width': width,
        if (height != 0) 'height': height,
        if (mime.isNotEmpty) 'mime': mime,
        if (size != 0) 'size': size,
        if (orientation != 0) 'orientation': orientation,
        if (mediaType == kMediaTypeVideo) 'media_type': mediaType,
        if (durationMs != 0) 'duration_ms': durationMs,
      };

  static PhotoEntry? fromJson(Map<String, dynamic> j) {
    final id = j['photo_id'];
    final taken = j['taken_at'];
    if (id is! String || id.isEmpty) return null;
    if (taken is! int) return null;
    final mt = j['media_type'];
    final mediaType = mt is String && mt == kMediaTypeVideo ? kMediaTypeVideo : kMediaTypePhoto;
    final dur = j['duration_ms'];
    return PhotoEntry(
      photoId: id,
      takenAt: taken,
      width: j['width'] is int ? j['width'] as int : 0,
      height: j['height'] is int ? j['height'] as int : 0,
      mime: j['mime'] is String ? j['mime'] as String : '',
      size: j['size'] is int ? j['size'] as int : 0,
      orientation: j['orientation'] is int ? j['orientation'] as int : 0,
      mediaType: mediaType,
      durationMs: dur is int && dur >= 0 && dur <= kMaxVideoDurationMs ? dur : 0,
    );
  }
}

/// Split a photo ID into (kind, row). Legacy digits and unknown prefixes
/// are opaque image IDs; `img:` / `vid:` prefixed IDs select the collection.
({String kind, String row})? parsePhotoID(String s) {
  final t = s.trim();
  if (!isValidPhotoID(t)) return null;
  if (t.startsWith('img:')) {
    final rest = t.substring(4);
    if (rest.isEmpty || rest.contains(':') || !isValidPhotoID(rest)) return null;
    return (kind: kMediaTypePhoto, row: rest);
  }
  if (t.startsWith('vid:')) {
    final rest = t.substring(4);
    if (rest.isEmpty || rest.contains(':') || !isValidPhotoID(rest)) return null;
    return (kind: kMediaTypeVideo, row: rest);
  }
  return (kind: kMediaTypePhoto, row: t);
}

bool isValidMediaType(String s) => s.isEmpty || s == kMediaTypePhoto || s == kMediaTypeVideo;

bool isValidPhotoRange({required int offset, required int length}) {
  if (offset < 0 || offset > kMaxPhotoTotalSize) return false;
  if (length < 0 || length > kMaxPhotoTotalSize) return false;
  if (length > 0) {
    if (length > kMaxPhotoRangeLen) return false;
    if (offset > kMaxPhotoTotalSize - length) return false;
  }
  return true;
}

bool isValidPhotoID(String s) {
  final t = s.trim();
  if (t.isEmpty || t.length > kMaxPhotoIDLen) return false;
  if (t.contains('/') || t.contains('\\') || t.contains('\x00')) return false;
  for (final r in t.runes) {
    if (r == 0 || r < 0x20 || r == 0x7f) return false;
  }
  if (t == '.' || t == '..') return false;
  return true;
}

bool isValidPhotoReqID(String s) {
  final t = s.trim();
  if (t.isEmpty || t.length > 64) return false;
  return true;
}

bool isValidPhotoCursor(String s) {
  if (s.length > 256) return false;
  return true;
}

bool isValidThumbSize(int? v) {
  if (v == null || v == 0) return true;
  return v >= kMinPhotoThumbSize && v <= kMaxPhotoThumbSize;
}

bool isValidPhotoTransferID(String s) {
  final t = s.trim().toLowerCase();
  if (t.length < 16 || t.length > 64) return false;
  if (t.length % 2 != 0) return false;
  return RegExp(r'^[0-9a-f]+$').hasMatch(t);
}
