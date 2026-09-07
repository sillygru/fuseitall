// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Mirrored-notification models and the bounded outbox. Mirrors
// packages/proto/notifications.json caps (id 128, app 64, title 128, text
// 512): display fields truncate fail-soft, bad IDs drop. Bodies never reach
// logs — only counts and IDs.
class NotifItem {
  const NotifItem({
    required this.id,
    this.app = '',
    this.title = '',
    this.text = '',
    this.postedAt = 0,
  });

  final String id;
  final String app;
  final String title;
  final String text;
  final int postedAt;

  static const maxIdLen = 128;
  static const maxAppLen = 64;
  static const maxTitleLen = 128;
  static const maxTextLen = 512;

  static String? cleanId(String? raw) {
    final t = (raw ?? '').trim();
    if (t.isEmpty || t.runes.length > maxIdLen) return null;
    return t;
  }

  static String trunc(String? raw, int max) {
    final t = (raw ?? '').trim();
    if (t.runes.length <= max) return t;
    return String.fromCharCodes(t.runes.take(max)).trim();
  }

  /// Fail-soft build: null when the ID is unusable. Pure.
  static NotifItem? fromPosted({
    required String? id,
    String? app,
    String? title,
    String? text,
    int postedAt = 0,
  }) {
    final clean = cleanId(id);
    if (clean == null) return null;
    return NotifItem(
      id: clean,
      app: trunc(app, maxAppLen),
      title: trunc(title, maxTitleLen),
      text: trunc(text, maxTextLen),
      postedAt: postedAt,
    );
  }

  Map<String, dynamic> toJson() => {
        'id': id,
        'app': app,
        'title': title,
        'text': text,
        'posted_at': postedAt,
      };
}

/// Bounded FIFO outbox for phone→Mac posts (cap 50, oldest dropped) plus
/// the dismissal queue. In-memory; the listener reposts live notifications
/// after a restart. Pure: unit-tested without platform channels.
class NotifOutbox {
  NotifOutbox({this.maxQueued = 50});

  final int maxQueued;
  final _posts = <NotifItem>[];
  final _dismissals = <String>[];

  List<NotifItem> get posts => List.unmodifiable(_posts);
  List<String> get dismissals => List.unmodifiable(_dismissals);

  void queuePost(NotifItem item) {
    _posts.add(item);
    while (_posts.length > maxQueued) {
      _posts.removeAt(0);
    }
  }

  void queueDismiss(String id) {
    final clean = NotifItem.cleanId(id);
    if (clean == null) return;
    _dismissals.add(clean);
  }

  /// Remove and return up to [limit] queued posts for one flush round.
  List<NotifItem> takePosts([int limit = 10]) {
    final n = limit < _posts.length ? limit : _posts.length;
    final out = _posts.sublist(0, n);
    _posts.removeRange(0, n);
    return out;
  }

  List<String> takeDismissals() {
    final out = List<String>.from(_dismissals);
    _dismissals.clear();
    return out;
  }

  void requeuePosts(List<NotifItem> items) {
    _posts.insertAll(0, items);
    while (_posts.length > maxQueued) {
      _posts.removeLast();
    }
  }

  void requeueDismissals(List<String> ids) {
    _dismissals.insertAll(0, ids);
  }
}
