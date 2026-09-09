// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:battery_plus/battery_plus.dart';
import 'package:device_info_plus/device_info_plus.dart';

// Self-advertised phone identity carried on every ping (heartbeat included).
// All fields are optional: null means unknown and the Mac keeps its previous
// reading. Matches packages/proto/ping.json (device_name, model,
// battery_pct, charging).
class DeviceFacts {
  const DeviceFacts({
    this.deviceName,
    this.model,
    this.batteryPct,
    this.charging,
  });

  /// Friendly name, e.g. "OnePlus CPH2767". The Mac shows its local rename
  /// alias when one exists.
  final String? deviceName;

  /// Hardware model, e.g. "CPH2767". Never renamed.
  final String? model;

  /// Battery level 0..100, null while unknown.
  final int? batteryPct;

  /// True while charging, null while unknown.
  final bool? charging;
}

/// Reads [DeviceFacts]. Fail-soft by contract: implementations never throw,
/// they return absent fields instead.
abstract class DeviceFactsProvider {
  Future<DeviceFacts> currentFacts();
}

/// Production provider backed by device_info_plus + battery_plus. Device
/// name/model are stable per install so they are read once and cached;
/// battery is read fresh on every call (announce freshness). Every read is
/// individually guarded: one failing source never blocks the others.
class LiveDeviceFactsProvider implements DeviceFactsProvider {
  LiveDeviceFactsProvider({DeviceInfoPlugin? info, Battery? battery})
      : _info = info ?? DeviceInfoPlugin(),
        _battery = battery ?? Battery();

  final DeviceInfoPlugin _info;
  final Battery _battery;
  String? _cachedName;
  String? _cachedModel;
  bool _identityRead = false;

  @override
  Future<DeviceFacts> currentFacts() async {
    if (!_identityRead) {
      _identityRead = true;
      try {
        final android = await _info.androidInfo;
        _cachedModel = cleanLabel(android.model);
        _cachedName = friendlyName(cleanLabel(android.manufacturer),
            _cachedModel);
      } catch (_) {
        _cachedName = null;
        _cachedModel = null;
      }
    }
    int? pct;
    try {
      final level = await _battery.batteryLevel;
      if (level >= 0 && level <= 100) pct = level;
    } catch (_) {
      pct = null;
    }
    bool? charging;
    try {
      charging = await _battery.batteryState == BatteryState.charging;
    } catch (_) {
      charging = null;
    }
    return DeviceFacts(
      deviceName: _cachedName,
      model: _cachedModel,
      batteryPct: pct,
      charging: charging,
    );
  }
}

/// "OnePlus" + "CPH2767" -> "OnePlus CPH2767". Pure: unit-tested without
/// plugins. Returns the model alone when the manufacturer is absent or
/// already prefixes it; null when the model itself is unknown.
String? friendlyName(String? manufacturer, String? model) {
  if (model == null || model.isEmpty) return null;
  if (manufacturer == null || manufacturer.isEmpty) return model;
  if (model.toLowerCase().startsWith(manufacturer.toLowerCase())) {
    return model;
  }
  return '$manufacturer $model';
}

/// Trim + drop empties + cap at the 64-char contract limit. Pure.
String? cleanLabel(String? raw) {
  if (raw == null) return null;
  final trimmed = raw.trim();
  if (trimmed.isEmpty) return null;
  if (trimmed.runes.length > 64) {
    return String.fromCharCodes(trimmed.runes.take(64)).trim();
  }
  return trimmed;
}
