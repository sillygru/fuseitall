// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/playback/playback_models.dart';
import 'package:fuseitall/features/settings/app_settings.dart';

void main() {
  group('PlaybackState', () {
    test('decode truncates and drops bad artwork fail-soft', () {
      final st = PlaybackState.fromJson({
        'title': '  Song  ',
        'state': 'PLAYING',
        'position_ms': 1000,
        'duration_ms': 200000,
        'updated_ms': 5,
        'artwork_b64': '!!!not-base64!!!',
        'artwork_mime': 'image/jpeg',
      })!;
      expect(st.title, 'Song');
      expect(st.state, PlaybackState.playing);
      expect(st.artworkB64, isEmpty);
    });

    test('negative updated_ms fails closed', () {
      expect(PlaybackState.fromJson({'updated_ms': -1}), isNull);
    });

    test('remote wins latest only', () {
      expect(PlaybackState.remoteWins(10, 11), isTrue);
      expect(PlaybackState.remoteWins(11, 11), isFalse);
      expect(PlaybackState.remoteWins(5, 0), isFalse);
    });

    test('throttle: changes immediate, progress 5s', () {
      const last = PlaybackState(
        title: 'A',
        artist: '',
        album: '',
        packageName: 'com.x',
        app: '',
        state: PlaybackState.playing,
        positionMs: 0,
        durationMs: 1000,
        updatedMs: 1000,
      );
      const changed = PlaybackState(
        title: 'B',
        artist: '',
        album: '',
        packageName: 'com.x',
        app: '',
        state: PlaybackState.playing,
        positionMs: 0,
        durationMs: 1000,
        updatedMs: 1001,
      );
      expect(changed.shouldSendAfter(last, 1001), isTrue);
      const same = PlaybackState(
        title: 'A',
        artist: '',
        album: '',
        packageName: 'com.x',
        app: '',
        state: PlaybackState.playing,
        positionMs: 500,
        durationMs: 1000,
        updatedMs: 1001,
      );
      expect(same.shouldSendAfter(last, 2000), isFalse);
      expect(same.shouldSendAfter(last, 7000), isTrue);
    });

    test('commands validate', () {
      expect(PlaybackCmd.isValid('next'), isTrue);
      expect(PlaybackCmd.isValid('dance'), isFalse);
    });
  });

  group('Playback settings', () {
    test('absent means phone-to-mac view-only + inapp', () {
      final st = AppSettings.fromJson({'updated_unix': 7});
      expect(st.playbackMode, AppSettings.playbackDefault);
      expect(st.playbackMode, AppSettings.androidToMac);
      expect(st.playbackOutput, AppSettings.playbackOutputInApp);
      expect(AppSettings.playbackAllowsState(st.playbackMode), isTrue);
      expect(AppSettings.playbackAllowsCommand(st.playbackMode), isFalse);
    });

    test('both allows all, disabled blocks all', () {
      expect(AppSettings.playbackAllowsState('both'), isTrue);
      expect(AppSettings.playbackAllowsCommand('both'), isTrue);
      expect(AppSettings.playbackAllowsState('disabled'), isFalse);
      expect(AppSettings.playbackAllowsCommand('disabled'), isFalse);
      expect(AppSettings.playbackAllowsCommand('mac_to_android'), isTrue);
      expect(AppSettings.playbackAllowsState('mac_to_android'), isFalse);
    });

    test('round-trips playback fields', () {
      final st = AppSettings.fromJson({'updated_unix': 7});
      final next = st.withPlaybackMode('both', nowUnix: 8);
      expect(next.playbackMode, 'both');
      final rt = AppSettings.fromJson(next.toJson());
      expect(rt.playbackMode, 'both');
    });
  });
}
