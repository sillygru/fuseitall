// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// App version mirror of packages/core/version.go (the only cross-language
// contract lives in packages/proto). Builds gate compatibility; appVersion
// is display-only. Keep in sync with core CurrentAppVersion/CurrentBuild
// (task version:check enforces the match).
const kAppVersion = '0.13.0';
const kAppBuild = 13;
const kMinPeerBuild = 1;
const kProtocolV = 1;

/// Human version for a known build, or empty when unknown (caller falls
/// back to build-only messaging). Mirrors core BuildToVersion.
String appVersionForBuild(int build) {
  const versions = <int, String>{1: '0.1.0', 2: '0.2.0', 3: '0.3.0', 4: '0.4.0', 5: '0.5.0', 6: '0.6.0', 7: '0.7.0', 8: '0.8.0', 9: '0.9.0', 10: '0.10.0', 11: '0.11.0', 12: '0.12.0', 13: '0.13.0'};
  return versions[build] ?? '';
}
