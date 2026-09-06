// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/device/device_info_provider.dart';

void main() {
  group('friendlyName', () {
    test('combines manufacturer and model', () {
      expect(friendlyName('OnePlus', 'CPH2767'), 'OnePlus CPH2767');
    });

    test('model alone when manufacturer is absent', () {
      expect(friendlyName(null, 'CPH2767'), 'CPH2767');
      expect(friendlyName('', 'CPH2767'), 'CPH2767');
    });

    test('model alone when it already carries the manufacturer', () {
      expect(friendlyName('Google', 'Google Pixel 8'), 'Google Pixel 8');
      expect(friendlyName('samsung', 'Samsung SM-S921B'), 'Samsung SM-S921B');
    });

    test('null when the model is unknown', () {
      expect(friendlyName('OnePlus', null), isNull);
      expect(friendlyName('OnePlus', ''), isNull);
    });
  });

  group('cleanLabel', () {
    test('trims and drops empties', () {
      expect(cleanLabel('  CPH2767  '), 'CPH2767');
      expect(cleanLabel('   '), isNull);
      expect(cleanLabel(null), isNull);
    });

    test('caps at the 64-char contract limit', () {
      final long = 'x' * 70;
      expect(cleanLabel(long)!.runes.length, 64);
      expect(cleanLabel('x' * 64)!.runes.length, 64);
    });
  });
}
