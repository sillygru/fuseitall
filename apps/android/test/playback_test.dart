// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:fuseitall/features/playback/playback_models.dart';
import 'package:fuseitall/features/playback/playback_sync.dart';
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

    test('dedupe: identity changes send, bare progress never sends', () {
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
      expect(same.shouldSendAfter(last, 700000), isFalse);
      const artChanged = PlaybackState(
        title: 'A',
        artist: '',
        album: '',
        packageName: 'com.x',
        app: '',
        state: PlaybackState.playing,
        positionMs: 500,
        durationMs: 1000,
        updatedMs: 1001,
        artworkB64: 'abc',
      );
      expect(artChanged.shouldSendAfter(last, 700000), isTrue);
    });

    test('commands validate', () {
      expect(PlaybackCmd.isValid('next'), isTrue);
      expect(PlaybackCmd.isValid('dance'), isFalse);
    });

    test('playbackEvents decodes a pushed snapshot', () async {
      TestWidgetsFlutterBinding.ensureInitialized();
      const channel = EventChannel('fuseitall/playbackEvents');
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockStreamHandler(
        channel,
        MockStreamHandler.inline(onListen: (args, events) {
          events.success({
            'title': 'Song',
            'artist': 'Band',
            'state': 'playing',
            'position_ms': 1000,
            'duration_ms': 200000,
            'updated_ms': 5,
            'package_name': 'com.spotify.music',
            'origin': 'android',
          });
        }),
      );
      addTearDown(() {
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
            .setMockStreamHandler(channel, null);
      });
      final first = await PlaybackSync().playbackEvents.first;
      expect(first?.title, 'Song');
      expect(first?.state, PlaybackState.playing);
      expect(first?.packageName, 'com.spotify.music');
    });

    test('command routes with package hint', () async {
      TestWidgetsFlutterBinding.ensureInitialized();
      Map<String, dynamic>? seenArgs;
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(
        const MethodChannel('fuseitall/playback'),
        (call) async {
          if (call.method == 'command') {
            seenArgs = Map<String, dynamic>.from(call.arguments as Map);
            return true;
          }
          return null;
        },
      );
      addTearDown(() {
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
            .setMockMethodCallHandler(
                const MethodChannel('fuseitall/playback'), null);
      });
      final ok = await PlaybackSync()
          .command('next', packageHint: 'com.spotify.music');
      expect(ok, isTrue);
      expect(seenArgs?['cmd'], 'next');
      expect(seenArgs?['package_name'], 'com.spotify.music');
    });

    test('resubscribe asks native to re-anchor with zero pulls', () async {
      TestWidgetsFlutterBinding.ensureInitialized();
      String? seenMethod;
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(
        const MethodChannel('fuseitall/playback'),
        (call) async {
          seenMethod = call.method;
          return true;
        },
      );
      addTearDown(() {
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
            .setMockMethodCallHandler(
                const MethodChannel('fuseitall/playback'), null);
      });
      await PlaybackSync().resubscribe();
      expect(seenMethod, 'watch');
    });

    test('idle after playing sends, repeat idle deduped', () {
      const playing = PlaybackState(
        title: 'A',
        artist: 'Band',
        album: '',
        packageName: 'com.x',
        app: 'X',
        state: PlaybackState.playing,
        positionMs: 1000,
        durationMs: 200000,
        updatedMs: 1000,
        artworkB64: 'abc',
        artworkMime: 'image/jpeg',
      );
      const idle = PlaybackState(
        title: '',
        artist: '',
        album: '',
        packageName: '',
        app: '',
        state: PlaybackState.stopped,
        positionMs: 0,
        durationMs: 0,
        updatedMs: 2000,
      );
      // App close emits idle: title/state/artwork all differ -> must send.
      expect(idle.shouldSendAfter(playing, 2000), isTrue);
      // Steady idle (anchors on every watch) never re-sends.
      expect(idle.shouldSendAfter(idle, 999999), isFalse);
    });
  });

  group('Playback settings', () {
    test('absent means two-way + inapp', () {
      final st = AppSettings.fromJson({'updated_unix': 7});
      expect(st.playbackMode, AppSettings.playbackDefault);
      expect(st.playbackMode, AppSettings.both);
      expect(st.playbackOutput, AppSettings.playbackOutputInApp);
      expect(AppSettings.playbackAllowsState(st.playbackMode), isTrue);
      expect(AppSettings.playbackAllowsCommand(st.playbackMode), isTrue);
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
