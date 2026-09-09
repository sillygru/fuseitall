# ADR 0010 — Playback sync: phone now-playing to Mac player

## Context

0.9.0 proved reliable live-only notification mirror with per-app filter.
The next milestone (0.10.0) adds playback sync: the phone's now-playing
state shows on the Mac and the Mac can control phone transport. Like
clipboard, users want direction control (off, two-way, each one-way) plus
a Mac choice between in-app-only display and Control Center Now Playing.

## Decision

- New message types on a new route, same gate: `playback-state`
  (phone -> Mac snapshot) and `playback-cmd` (Mac -> phone transport) on
  `/playback` (`core/server.go: handlePlayback`). Gate order is unchanged
  (`protocol_v` -> `min_peer_build` -> `capability` -> `token` -> logic);
  every handler replies with a pong echoing the request nonce.
  `protocol_v` stays 1; `CurrentBuild` becomes 10 with `MinPeerBuild` 1
  (build-9 peers 426 playback calls via the capability check).
- Capability `playback` gates per message, never per connection.
- Phone is the state source in 0.10.0 (MediaSessionManager via the
  notification-listener grant, no new permission). Mac-to-phone state
  capture is explicitly deferred: `mac_to_android` is commands-only blind
  remote until a future Mac capture lands additive (`origin=mac`).
- Direction `playback_mode` mirrors clipboard semantics: `both` shows and
  controls, `android_to_mac` (default) shows view-only, `mac_to_android`
  controls blind, `disabled` idles the player. State display additionally
  requires `PlaybackModeAllowsState`, commands require
  `PlaybackModeAllowsCommand`.
- Presentation `playback_output` is Mac-local in effect but synced in the
  blob for single LWW consistency: `inapp` (default, no OS side effects)
  vs `system` (in-app plus MPNowPlayingInfoCenter mirror via a fail-soft
  Obj-C adapter that never breaks the in-app player).
- State is event-driven push with zero pulls: the phone registers
  `MediaController.Callback` (`onPlaybackStateChanged`,
  `onMetadataChanged`, `onSessionDestroyed`) plus
  `OnActiveSessionsChangedListener` and emits over
  `EventChannel('fuseitall/playbackEvents')`; Dart forwards every new
  snapshot immediately over the persistent WebSocket. Stopped playback is
  an explicit pushed idle state, never silence (an empty session list
  emits idle so the Mac clears). Re-anchoring (native re-register plus
  anchor push via `watch`) happens only on connect, resume, and
  mode-toggle; the latest known snapshot is redelivered once on connect
  to cover pushes lost while offline. There is no pull API and nothing
  runs on a timer. Commands are edge-triggered nonce-idempotent.
  Latest-wins on `updated_ms` (ties keep local). Artwork is optional
  downscaled JPEG (~192px, ~128KB b64 cap); oversize/invalid drops
  fail-soft, metadata survives. Titles never reach logs (package + lengths
  only).
- Settings sync carries both fields additive (`settings.json`): absent
  means phone-to-Mac + in-app for backward compat.

## Consequences

- Adding artwork detail or Mac capture later is additive: new optional
  fields, same capability, no protocol break.
- Cost: zero idle wakeups. The phone sends only on real track/state
  changes; the Mac interpolates the progress bar locally from
  `PositionMs`+`UpdatedMs` (UI clock only, never the network). Disabled
  and view-only modes send nothing. System mirror needs a real Mac run
  (MPNowPlayingInfoCenter is a no-op in tests/CI).
