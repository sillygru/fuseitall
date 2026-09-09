// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter/services.dart';

// One OS-pushed battery sample: level 0..100 plus the charging flag.
// Value type so the stream can dedupe identical consecutive readings.
class BatteryReading {
  const BatteryReading({required this.pct, required this.charging});

  final int pct;
  final bool charging;

  @override
  bool operator ==(Object other) =>
      other is BatteryReading &&
      other.pct == pct &&
      other.charging == charging;

  @override
  int get hashCode => Object.hash(pct, charging);
}

/// Validate one native `fuseitall/battery` event into a reading.
/// Null when malformed (missing keys, wrong types, out-of-range level):
/// callers skip, never throw. Pure: unit-tested without a platform channel.
BatteryReading? parseBatteryEvent(Object? event) {
  if (event is! Map) return null;
  final pct = event['battery_pct'];
  final charging = event['charging'];
  if (pct is! int || pct < 0 || pct > 100) return null;
  if (charging is! bool) return null;
  return BatteryReading(pct: pct, charging: charging);
}

/// Reads OS-pushed battery samples. Fail-soft by contract: implementations
/// never throw, they skip malformed events instead.
abstract class BatteryReadings {
  Stream<BatteryReading> get readings;
}

/// Production source backed by the native ACTION_BATTERY_CHANGED
/// EventChannel (see BatteryChannel.kt). The OS pushes on every level and
/// status change, so there is no polling: identical consecutive readings
/// are dropped with [Stream.distinct] and channel errors end the stream
/// silently instead of crashing the page.
class BatteryWatcher implements BatteryReadings {
  BatteryWatcher({EventChannel? channel})
      : _channel = channel ?? const EventChannel('fuseitall/battery');

  final EventChannel _channel;

  @override
  Stream<BatteryReading> get readings {
    Stream<Object?> raw;
    try {
      raw = _channel.receiveBroadcastStream();
    } catch (_) {
      return const Stream.empty();
    }
    return raw
        .map(parseBatteryEvent)
        .where((reading) => reading != null)
        .cast<BatteryReading>()
        .distinct()
        .handleError((_) {});
  }
}
