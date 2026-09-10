# Settings

How app preferences stay in sync so both devices always agree. One blob,
last-writer-wins, fail-soft on unknown fields.

## What it does

Every settings change (notifications toggle, clipboard direction, notif
filter, playback mode/output) persists locally, pushes immediately when
paired, and converges to the newest blob. Absent fields mean "old peer" and
resolve to safe defaults — never an error.

## How it flows

1. User toggles: Mac `service_features.go:Set*` / Android
   `settings_store.dart` → `SanitizeSettings` → local persist →
   `TakePending` (latest-wins single slot) → `flushPendingToPhone()` /
   `_flushSettings()` over WS (fallback `POST /settings`).
2. Receiver `ApplyRemote` + `persistSnapshot`, then fans out: native
   `updateNotifFilter`, playback re-anchor, notif/clip gate re-eval.
3. Offline changes queue as the single latest blob and flush on the next
   WS connect.

## Contract

- Capability `settings-sync` (`packages/core/features.go`,
  `packages/proto/settings.json`).
- Type `settings-sync` on `POST /settings` (+ WS).
- Ordering: greater `updated_unix` wins; ties go to Mac
  (`RemoteSettingsWins` / Dart `remoteSettingsWins`); `UpdatedUnix<0`
  fails closed.
- Defaults for absent (backward compat): `notifications_enabled=true`,
  `clipboard_mode=both`, `notif_mode=all_except_muted + []`,
  `playback_mode=android_to_mac`, `playback_output=inapp`.
- `clipboard_auto_background` is device-local only: persisted in the same
  Android blob, stripped from the sync wire (`toSyncJson`), preserved
  across remote adopts (`withLocalFlagsFrom`). The Mac never sees it.

## Key files

- Core: `features.go` (`TypeSettingsSync`, `RemoteSettingsWins`,
  `SanitizeSettings`, `SanitizeNotifFilterList`).
- Mac: `backend/settings.go` (`AppSettings`, `SettingsStore`,
  `settings.json` 0600/0700), `backend/service_features.go`,
  `frontend/src/components/SettingsPane.svelte`,
  `frontend/src/backend.ts` (`getSettings/normalizeSettings`).
- Android: `features/settings/app_settings.dart`,
  `settings_store.dart`, `settings_page.dart`.

## Rules & limits

- Sanitize canonicalizes unknown → default fail-soft; notif filter lists
  are trimmed/deduped/sorted/capped at 100.
- Persist: Mac `~/Library/Application Support/FuseItAll/settings.json`;
  Android `SecureStorage fuseitall_settings`. Corrupt → defaults, never
  throw.
- Clipboard and notification bodies never reach logs; settings sync logs
  lengths/IDs only.

## Failure modes

- Old peer omits new keys → defaults apply, sync still converges.
- Missing capability → `UPDATE_REQUIRED` like any gated feature.
- Conflict → newest wins deterministically; no merge UI needed.
