// SPDX-License-Identifier: AGPL-3.0-only

// Canonical per-app notification filter. Mirrors core.ShouldMirrorNotif +
// NormalizeNotifMode + SanitizeNotifFilterList: progress always drops,
// otherwise the mode decides. Pure: unit-tested without platform channels.
class NotifFilterPolicy {
  const NotifFilterPolicy({
    this.mode = NotifFilterPolicy.allExceptMuted,
    this.mutedPackages = const {},
    this.allowedPackages = const {},
  });

  static const allExceptMuted = 'all_except_muted';
  static const onlyAllowed = 'only_allowed';
  static const maxApps = 100;

  final String mode;
  final Set<String> mutedPackages;
  final Set<String> allowedPackages;

  static String normalizeMode(String s) {
    final n = s.trim().toLowerCase();
    if (n == onlyAllowed) return onlyAllowed;
    return allExceptMuted;
  }

  static bool isValidMode(String s) {
    final n = s.trim().toLowerCase();
    return n == allExceptMuted || n == onlyAllowed;
  }

  static Set<String> sanitizeList(Iterable<String>? input) {
    if (input == null) return const {};
    final out = <String>{};
    for (final raw in input) {
      final pkg = raw.trim();
      if (pkg.isEmpty || pkg.length > 128) continue;
      out.add(pkg);
      if (out.length >= maxApps) break;
    }
    return out;
  }

  factory NotifFilterPolicy.fromJson(Map<String, dynamic> json) {
    final mode = json['notif_mode'] is String
        ? normalizeMode(json['notif_mode'] as String)
        : allExceptMuted;
    Set<String> readList(Object? v) {
      if (v is! List) return const {};
      return sanitizeList(v.whereType<String>());
    }

    return NotifFilterPolicy(
      mode: mode,
      mutedPackages: readList(json['muted_packages']),
      allowedPackages: readList(json['allowed_packages']),
    );
  }

  Map<String, dynamic> toJson() => {
        'notif_mode': mode,
        'muted_packages': mutedPackages.toList()..sort(),
        'allowed_packages': allowedPackages.toList()..sort(),
      };

  /// Single canonical predicate. Empty package is never list-filtered
  /// (absent field from old senders). Pure.
  bool shouldMirror(String packageName, {bool hasProgress = false}) {
    if (hasProgress) return false;
    final pkg = packageName.trim();
    if (mode == onlyAllowed) {
      if (pkg.isEmpty) return false;
      return allowedPackages.contains(pkg);
    }
    if (pkg.isEmpty) return true;
    return !mutedPackages.contains(pkg);
  }

  NotifFilterPolicy withMode(String next) => NotifFilterPolicy(
        mode: normalizeMode(next),
        mutedPackages: mutedPackages,
        allowedPackages: allowedPackages,
      );

  NotifFilterPolicy withMutedToggled(String pkg) {
    final p = pkg.trim();
    if (p.isEmpty) return this;
    final next = Set<String>.from(mutedPackages);
    if (!next.remove(p)) {
      if (next.length >= maxApps) return this;
      next.add(p);
    }
    return NotifFilterPolicy(
        mode: mode, mutedPackages: next, allowedPackages: allowedPackages);
  }

  NotifFilterPolicy withAllowedToggled(String pkg) {
    final p = pkg.trim();
    if (p.isEmpty) return this;
    final next = Set<String>.from(allowedPackages);
    if (!next.remove(p)) {
      if (next.length >= maxApps) return this;
      next.add(p);
    }
    return NotifFilterPolicy(
        mode: mode, mutedPackages: mutedPackages, allowedPackages: next);
  }
}
