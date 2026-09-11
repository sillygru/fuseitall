// SPDX-License-Identifier: AGPL-3.0-only

import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/connection/mac_locator.dart';

import 'go_server_test.dart' show FakeKeyValueStorage;

void main() {
  test('orderedTargets puts primary first, dedupes', () {
    final out = MacLocator.orderedTargets('192.168.1.6', [
      '192.168.1.5',
      '192.168.1.6',
    ]);
    expect(out, ['192.168.1.6', '192.168.1.5']);
  });

  test('orderedTargets appends candidates and dedupes', () {
    final out = MacLocator.orderedTargets(
      '192.168.1.6',
      ['192.168.1.5'],
      ['192.168.1.5', '10.0.0.2', '192.168.1.6'],
    );
    expect(out, ['192.168.1.6', '192.168.1.5', '10.0.0.2']);
  });

  test('remember keeps most-recent-first, capped', () async {
    final locator = MacLocator(FakeKeyValueStorage());
    await locator.remember('192.168.1.5');
    await locator.remember('192.168.1.6');
    await locator.remember('192.168.1.5');
    final hosts = await locator.load();
    expect(hosts.first, '192.168.1.5');
    expect(hosts.length, 2);
  });

  test('sweepTargets stays in the same /24', () {
    final out = MacLocator.sweepTargets('192.168.1.10', cap: 4);
    expect(out.length, 4);
    expect(out.every((h) => h.startsWith('192.168.1.')), isTrue);
    expect(out.contains('192.168.1.10'), isFalse);
  });

  test('sweepTargets rejects non-IPv4', () {
    expect(MacLocator.sweepTargets('not-an-ip'), isEmpty);
  });
}
