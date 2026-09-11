// SPDX-License-Identifier: AGPL-3.0-only

// Mirrored-notification models and the bounded outbox. Mirrors
// packages/proto/notifications.json caps (id 256, app 64, package 128,
// title 128, text 512, icon 32768b64): display fields truncate fail-soft, bad
// IDs drop, invalid icons dropped (keep notification). Bodies never reach
// logs — only counts and IDs.
//
// Reliability model (0.9.0, live-only): progress posts never queue (dropped
// at parse/filter), stale posts (>5 min) are dropped on take, identical
// same-ID reposts replace silently without rescheduling banners.
class NotifItem {
  const NotifItem({
    required this.id,
    this.app = '',
    this.packageName = '',
    this.iconB64 = '',
    this.groupKey = '',
    this.title = '',
    this.text = '',
    this.postedAt = 0,
    this.ongoing = false,
    this.hasProgress = false,
  });

  final String id;
  final String app;
  final String packageName;
  final String iconB64;
  final String groupKey;
  final String title;
  final String text;
  final int postedAt;
  final bool ongoing;
  final bool hasProgress;

  static const maxIdLen = 256;
  static const maxAppLen = 64;
  static const maxPackageLen = 128;
  static const maxIconB64Len = 32768;
  static const maxGroupLen = 128;
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

  static String? cleanIcon(String? raw) {
    final t = (raw ?? '').trim();
    if (t.isEmpty) return '';
    if (t.length > maxIconB64Len) return '';
    // Basic base64 shape check — full decode is wasteful per notification.
    for (final r in t.runes) {
      if ((r >= 65 && r <= 90) ||
          (r >= 97 && r <= 122) ||
          (r >= 48 && r <= 57) ||
          r == 43 ||
          r == 47 ||
          r == 61) {
        continue;
      }
      return '';
    }
    return t;
  }

  /// Fail-soft build: null when the ID is unusable or when the post is a
  /// progress notification (dropped before queueing so Play Store percent
  /// ticks never spam the Mac). Pure.
  static NotifItem? fromPosted({
    required String? id,
    String? app,
    String? packageName,
    String? iconB64,
    String? groupKey,
    String? title,
    String? text,
    int postedAt = 0,
    bool ongoing = false,
    bool hasProgress = false,
  }) {
    if (hasProgress) return null;
    final clean = cleanId(id);
    if (clean == null) return null;
    return NotifItem(
      id: clean,
      app: trunc(app, maxAppLen),
      packageName: trunc(packageName, maxPackageLen),
      iconB64: cleanIcon(iconB64) ?? '',
      groupKey: trunc(groupKey, maxGroupLen),
      title: trunc(title, maxTitleLen),
      text: trunc(text, maxTextLen),
      postedAt: postedAt,
      ongoing: ongoing,
      hasProgress: false,
    );
  }

  /// True when this post carries identical display content to [other]
  /// (same title+text): a silent row refresh, never a new banner. Pure.
  bool sameContent(NotifItem other) =>
      title == other.title && text == other.text;

  /// True when too old for live-only mirroring (>5 min). Unknown timestamps
  /// (<=0, old senders) are kept for backward compat. Pure.
  bool get isStale {
    if (postedAt <= 0) return false;
    final now = DateTime.now().toUtc().millisecondsSinceEpoch ~/ 1000;
    return now - postedAt > NotifOutbox.maxAgeSec;
  }

  Map<String, dynamic> toJson() {
    final m = <String, dynamic>{
      'id': id,
      'app': app,
      'title': title,
      'text': text,
      'posted_at': postedAt,
    };
    if (packageName.isNotEmpty) m['package_name'] = packageName;
    if (iconB64.isNotEmpty) m['app_icon_b64'] = iconB64;
    if (groupKey.isNotEmpty) m['group_key'] = groupKey;
    if (ongoing) m['ongoing'] = true;
    // has_progress is never sent: filtered posts never queue. The key is
    // omitted (not false) so old Mac peers see no change.
    return m;
  }
}

/// Bounded FIFO outbox for phone→Mac posts (cap 50, oldest dropped) plus
/// the dismissal queue (cap 50). In-memory, live-only: stale posts (>5 min)
/// are dropped on take so a reconnecting Mac never receives hours-old
/// history as if it were new. Pure: unit-tested without platform channels.
class NotifOutbox {
  NotifOutbox({this.maxQueued = 50});

  /// Live-only TTL in seconds, mirrors native MAX_AGE_SEC.
  static const maxAgeSec = 300;
  static const maxDismissals = 50;

  final int maxQueued;
  final _posts = <NotifItem>[];
  final _dismissals = <String>[];

  List<NotifItem> get posts => List.unmodifiable(_posts);
  List<String> get dismissals => List.unmodifiable(_dismissals);

  static int _nowUnix() =>
      DateTime.now().toUtc().millisecondsSinceEpoch ~/ 1000;

  static bool _isStale(NotifItem item, int now) {
    if (item.postedAt <= 0) return false;
    return now - item.postedAt > maxAgeSec;
  }

  void queuePost(NotifItem item) {
    // Progress never queues (defense in depth: native + parse already drop).
    if (item.hasProgress) return;
    // Deduplicate by id: same notification reposted (e.g. active tick)
    // replaces in place and moves to end so latest wins.
    // Mirrors apps/mac/backend/notifications.go Post behavior (newest first
    // on Mac). Here FIFO with move-to-tail keeps send order stable.
    final existing = _posts.indexWhere((e) => e.id == item.id);
    if (existing != -1) _posts.removeAt(existing);
    _posts.add(item);
    while (_posts.length > maxQueued) {
      _posts.removeAt(0);
    }
  }

  /// Returns true when the post was queued (false when filtered as progress
  /// or per-app muted). Pure state change, no logging of bodies.
  bool queuePostFiltered(
    NotifItem item,
    bool Function(String packageName, bool hasProgress) shouldMirror,
  ) {
    if (!shouldMirror(item.packageName, item.hasProgress)) return false;
    queuePost(item);
    return true;
  }

  void queueDismiss(String id) {
    final clean = NotifItem.cleanId(id);
    if (clean == null) return;
    // Orphan filtering happens natively (sentIds gate): by the time a
    // removal reaches Dart it was previously posted or is a live retract.
    // Bound the queue so a dismissal storm can never grow unbounded.
    if (_dismissals.length >= maxDismissals) _dismissals.removeAt(0);
    _dismissals.add(clean);
  }

  /// Remove and return up to [limit] queued posts for one flush round.
  /// Stale posts are dropped (not returned) so reconnects never flood.
  List<NotifItem> takePosts([int limit = 10]) {
    final now = _nowUnix();
    _posts.removeWhere((e) => _isStale(e, now));
    final n = limit < _posts.length ? limit : _posts.length;
    final out = _posts.sublist(0, n);
    _posts.removeRange(0, n);
    return out;
  }

  /// Remove and return up to [limit] dismissals. Bounded take fixes the old
  /// take-all-then-lose-tail failure mode.
  List<String> takeDismissals([int limit = 10]) {
    final n = limit < _dismissals.length ? limit : _dismissals.length;
    final out = _dismissals.sublist(0, n).toList();
    _dismissals.removeRange(0, n);
    return out;
  }

  void requeuePosts(List<NotifItem> items) {
    _posts.insertAll(0, items);
    while (_posts.length > maxQueued) {
      _posts.removeLast();
    }
  }

  void requeueDismissals(List<String> ids) {
    final clean = ids
        .map(NotifItem.cleanId)
        .whereType<String>()
        .take(maxDismissals)
        .toList();
    _dismissals.insertAll(0, clean);
    while (_dismissals.length > maxDismissals) {
      _dismissals.removeLast();
    }
  }

  /// Drop everything stale without returning it (used on connect).
  void dropStale() {
    final now = _nowUnix();
    _posts.removeWhere((e) => _isStale(e, now));
  }
}
