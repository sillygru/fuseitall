// SPDX-License-Identifier: AGPL-3.0-only

import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/pairing/pair_qr.dart';
import 'package:fuseitall/result.dart';

final _fp = 'aa' * 32; // 64 hex chars

String _qr(Map<String, Object?> override) {
  final base = <String, Object?>{
    'device_name': "Test Mac",
    'platform': 'mac',
    'host': '192.168.1.10',
    'port': 18443,
    'fingerprint': _fp,
    'pubkey': 'c3VwZXItc2VjcmV0LWtleQ==',
    'token': 'abcdef0123456789abcdef0123456789',
  }..addAll(override);
  base.removeWhere((key, value) => value == null);
  return jsonEncode(base);
}

void main() {
  group('parsePairQrJson', () {
    test('parses a valid QR with explicit v:1', () {
      final r = parsePairQrJson(_qr({'v': 1}));
      expect(r, isA<Ok<PairQR>>());
      final qr = (r as Ok<PairQR>).value;
      expect(qr.deviceName, "Test Mac");
      expect(qr.host, '192.168.1.10');
      expect(qr.port, 18443);
      expect(qr.fingerprint, _fp);
    });

    test('missing v defaults to 1', () {
      final r = parsePairQrJson(_qr({}));
      expect(r, isA<Ok<PairQR>>());
      expect((r as Ok<PairQR>).value.v, 1);
    });

    test('v>1 is rejected as an update error, never silent', () {
      final r = parsePairQrJson(_qr({'v': 2}));
      expect(r, isA<Err<PairQR>>());
      final f = (r as Err<PairQR>).failure;
      expect(f, isA<UnsupportedVersion>());
      expect(f.message, contains('Update'));
    });

    test('unknown keys are ignored', () {
      final r = parsePairQrJson(_qr({'v': 1, 'future_field': 'x', 'n': 42}));
      expect(r, isA<Ok<PairQR>>());
    });

    test('missing required field fails', () {
      final r = parsePairQrJson(_qr({'token': null}));
      expect((r as Err<PairQR>).failure, isA<ParseFailure>());
    });

    test('bad port fails', () {
      for (final p in [0, 99999, 'abc']) {
        expect(parsePairQrJson(_qr({'port': p})), isA<Err<PairQR>>());
      }
    });

    test('bad fingerprint fails', () {
      expect(parsePairQrJson(_qr({'fingerprint': 'zz'})), isA<Err<PairQR>>());
    });

    test('non-JSON and non-object fail', () {
      expect(parsePairQrJson('not json'), isA<Err<PairQR>>());
      expect(parsePairQrJson('[1,2]'), isA<Err<PairQR>>());
    });

    test('code passes through when valid 6 digits', () {
      final r = parsePairQrJson(_qr({'code': '482913'}));
      expect((r as Ok<PairQR>).value.code, '482913');
    });

    test('missing code becomes empty (old Mac)', () {
      final r = parsePairQrJson(_qr({}));
      expect((r as Ok<PairQR>).value.code, '');
    });

    test('malformed code fails the parse (never mislabeled as old Mac)', () {
      for (final c in ['abc', '12345', '1234567', '12345a', '', 42]) {
        final r = parsePairQrJson(_qr({'code': c}));
        expect(r, isA<Err<PairQR>>());
        expect((r as Err<PairQR>).failure, isA<ParseFailure>());
      }
    });

    test('toJson round-trips the code', () {
      final r = parsePairQrJson(_qr({'code': '000007'}));
      final qr = (r as Ok<PairQR>).value;
      final back = parsePairQr(qr.toJson());
      expect((back as Ok<PairQR>).value.code, '000007');
    });

    test('toJson without code parses back to empty code', () {
      final r = parsePairQrJson(_qr({}));
      final qr = (r as Ok<PairQR>).value;
      final back = parsePairQr(qr.toJson());
      expect((back as Ok<PairQR>).value.code, '');
    });

    test('candidates parsed from comma string or list and round-trips', () {
      final r = parsePairQrJson(_qr({'candidates': '192.168.1.5, 10.0.0.1'}));
      final qr = (r as Ok<PairQR>).value;
      expect(qr.candidates, ['192.168.1.5', '10.0.0.1']);
      final back = parsePairQr(qr.toJson());
      expect((back as Ok<PairQR>).value.candidates, ['192.168.1.5', '10.0.0.1']);

      final rList = parsePairQrJson(_qr({'candidates': ['192.168.1.5', '10.0.0.1']}));
      expect((rList as Ok<PairQR>).value.candidates, ['192.168.1.5', '10.0.0.1']);
    });
  });

  group('fingerprintsMatch', () {
    test('exact match', () => expect(fingerprintsMatch(_fp, _fp), isTrue));
    test('case and colon tolerant', () {
      expect(fingerprintsMatch('AA:bb CC', 'aa:bbcc'), isTrue);
    });
    test('one char off is a mismatch', () {
      expect(fingerprintsMatch(_fp, 'ab' * 32), isFalse);
    });
    test('different lengths mismatch', () {
      expect(fingerprintsMatch(_fp, 'aa'), isFalse);
    });
  });
}
