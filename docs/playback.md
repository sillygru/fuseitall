# Playback

How the phone's now-playing state shows on the Mac and how the Mac controls
phone transport. Event-push only; the progress bar is a local UI clock.

## What it does

See title/artist/album/artwork/state on the Mac, follow progress, and
send play/pause/next/prev — gated by a direction mode, with a Mac-local
choice between in-app display and Control Center Now Playing.

## How it flows

1. Phone `MediaController.Callback` (`onPlaybackStateChanged`,
   `onMetadataChanged`, `onSessionDestroyed`) +
   `OnActiveSessionsChangedListener` (via the notification-listener grant,
   no new permission) → `EventChannel fuseitall/playbackEvents` → Dart
   `features/playback/playback_sync.dart` forwards every new snapshot
   immediately over WS (fallback `POST /playback`).
2. Mac `handlePlayback` gates → `playback.go:PlaybackStore.ApplyRemote`
   (latest-wins on `updated_ms`) → `playback:changed` → `MediaSlot.svelte`
   + optional `mirrorPlaybackToSystem` (fail-soft Obj-C adapter, no-op in
   tests/CI).
3. Commands: Mac `SendPlaybackCmd{play,pause,toggle,next,prev}` (nonce,
   `seenCmds{256}` idempotency) → phone executes on the active session.
4. Re-anchor only on connect, resume, and mode toggle (`resubscribe()` /
   `watch` + `_deliverLatestPlayback` one-shot redelivery). Stopped playback
   is an explicit pushed idle state; empty session list emits idle so the
   Mac clears. No pull API, nothing on a timer.

## Contract

- Capability `playback`, gate `build>=10` (`packages/core/playback.go`,
  `packages/proto/playback.json`).
- Types on `POST /playback` (+ WS canonical): `playback-state`
  (phone→Mac snapshot) and `playback-cmd` (Mac→phone transport).
- `playback_mode`: `both` (default, show + control) / `android_to_mac`
  (view-only) / `mac_to_android` (blind remote, commands-only until Mac
  capture lands additive as `origin=mac`) / `disabled` (idles the player).
  State needs `PlaybackModeAllowsState`, commands need
  `PlaybackModeAllowsCommand`; `""` means defaults.
- `playback_output` is Mac-local in effect but synced in the blob for
  single-LWW consistency: `inapp` (default) / `system`.

## Key files

- Core: `playback.go` (`TypePlaybackState/Cmd`, `ShouldSendPlaybackState`,
  `RemotePlaybackWins`, sanitize).
- Mac: `backend/playback.go`, `backend/playback_system*.go`,
  `backend/service_features.go`, `frontend/src/components/MediaSlot.svelte`,
  `frontend/src/backend.ts` (`getPlayback/normalizePlayback`).
- Android: `features/playback/playback_sync.dart`,
  `playback_models.dart`.

## Rules & limits

- Dedupe: send on identity (`title/artist/album/package`) / `state` /
  `duration` / `artwork` change only — bare progress never sends
  (`ShouldSendPlaybackState`).
- Order: strictly greater `updated_ms` wins, ties keep local, 0 never wins.
- Sanitize: nonce required, `0..24h` + `position ≤ duration+5s`, display
  128/64 truncate fail-soft, `state unknown → stopped`, artwork optional
  downscaled JPEG (~192px, ~128KB b64 cap; oversize/invalid dropped
  fail-soft, metadata survives). Titles never reach logs (package +
  lengths only).
- Settings carry both fields additive (`settings.json`): absent = compat
  defaults. Disabled/view-only modes send nothing.
- Progress display is UI-clock only: `display = position_ms +
  (now - updated_ms)` capped at `duration_ms`, 500ms `setInterval` only
  while `playing + paired`, `frozenAt` on disconnect — never the network.

## Failure modes

- Commands with mode off → loud `"commands are off for this direction"`
  so the UI disables with a note.
- Build-9 peer → 426 on playback calls via the capability check.
- Artwork oversize → metadata renders, art drops silently (fail-soft).
