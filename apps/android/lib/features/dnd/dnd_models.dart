// SPDX-License-Identifier: AGPL-3.0-only

import 'package:flutter/foundation.dart';

/// Snapshot of the phone's Do Not Disturb status.
@immutable
class DndState {
  const DndState({
    required this.enabled,
    required this.hasPermission,
    this.updatedMs = 0,
  });

  final bool enabled;
  final bool hasPermission;
  final int updatedMs;

  factory DndState.fromJson(Map<String, dynamic> json) {
    return DndState(
      enabled: json['enabled'] == true,
      hasPermission: json['has_permission'] == true,
      updatedMs: (json['updated_ms'] as num?)?.toInt() ?? 0,
    );
  }

  Map<String, dynamic> toJson({String origin = 'android'}) {
    return {
      'enabled': enabled,
      'has_permission': hasPermission,
      'updated_ms': updatedMs,
      'origin': origin,
    };
  }

  DndState copyWith({
    bool? enabled,
    bool? hasPermission,
    int? updatedMs,
  }) {
    return DndState(
      enabled: enabled ?? this.enabled,
      hasPermission: hasPermission ?? this.hasPermission,
      updatedMs: updatedMs ?? this.updatedMs,
    );
  }

  @override
  bool operator ==(Object other) =>
      identical(this, other) ||
      other is DndState &&
          runtimeType == other.runtimeType &&
          enabled == other.enabled &&
          hasPermission == other.hasPermission &&
          updatedMs == other.updatedMs;

  @override
  int get hashCode => Object.hash(enabled, hasPermission, updatedMs);

  @override
  String toString() =>
      'DndState(enabled: $enabled, hasPermission: $hasPermission, updatedMs: $updatedMs)';
}
