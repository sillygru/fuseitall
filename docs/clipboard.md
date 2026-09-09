# Clipboard

How text and images sync between devices, in the direction the user chose.
In-memory only; bodies never reach logs (lengths only).

## What it does

Copy on one device, paste on the other — or restrict to one direction, or
turn it off. Manual push always works; auto-sync follows the mode.

## How it flows

1. Watcher fires: Mac `backend/clipboard_watcher.go` (`changeCount` guard +
   450ms debounce) or Android `clipboard_watcher.dart`
   (`OnPrimaryClipChangedListener → EventChannel
   fuseitall/clipboardEvents`, 350ms debounce).
2. Sender checks `ClipboardModeAllowsSend`, builds `clip-push{kind:text|image,
   origin:mac|android, changed_at, payload}`, stores latest-wins pending,
   sends via `WriteActiveWS` / `sendEnvelope` (fallback `POST /clip`).
3. Receiver checks `ClipboardModeAllowsReceive` + `RemoteClipWins` (strictly
   newer `changed_at` wins, ties keep local, 0 never wins), applies via
   `ApplyRemote`, writes the system pasteboard (`pbcopy`/`Clipboard.setData`,
   `writeText/writeImage`), and suppresses its own watcher echo
   (Mac 900ms `NoteRemoteCopy/Image`; Android 800ms `_ignoreClipUntil`).

## Contract

- Capability `clipboard` (`packages/core/features.go`,
  `packages/proto/clipboard.json`).
- Type `clip-push` on `POST /clip` (+ WS). Manual
  `PushClipboard/Current/Image` bypasses the send-gate; auto watcher and
  ingest respect the mode.
- `clipboard_mode`: `both` (default) / `android_to_mac` / `mac_to_android`
  / `disabled`. `origin` (`mac`/`macos`→normalized→`mac`/`android`) breaks
  echo loops: receivers drop their own origin.

## Key files

- Core: `features.go` (`TypeClipPush`, `RemoteClipWins`,
  `ClipboardMode*`, `SanitizeClip*`).
- Mac: `backend/clipboard.go` (`ClipStore`), `backend/clipboard_watcher.go`,
  `backend/clipboard_normalize.go`, `backend/pasteboard_change_*.go`,
  `backend/auto_sync.go`, `backend/service_features.go`,
  `frontend/src/components/ClipboardPane.svelte`.
- Android: `features/clipboard/clipboard_sync.dart` (`ClipState`),
  `features/clipboard/clipboard_watcher.dart`.

## Rules & limits

- Caps: text ≤262144B; image 5MiB raw (~7.1MiB b64), `mime ∈
  PNG/JPEG/WEBP/GIF/TIFF/HEIC/HEIF` + magic sniff + `SanitizeClipImage`;
  `filename` is a sanitized basename ≤255 else `clip-<ts>.<ext>`.
- Mac normalize: `TIFF/HEIC/HEIF → PNG` via `sips`; over-cap `PNG → JPEG
  q85`; bytes-exact `setDataForType` + `public.file-url` so Finder drags
  keep working.
- The 300ms `changeCount` guard is the single documented polling exception:
  one cgo int compare per tick while paired; unpaired or send-disabled
  short-circuits before any cgo/shell work; shells run only after the count
  changes. No network on a schedule — sends ride the push path.
- Android 10+ background clipboard reads return null: sync is
  foreground-driven, documented, never silent-failed.

## Failure modes

- Mode disables a direction → watcher/ingest drops loud with the mode
  reason; manual push still sends.
- Over-cap/foreign-mime → dropped fail-soft, metadata survives, lengths
  logged only.
- Clock skew: `now <= cur → now = cur+1` so `changed_at` stays monotonic.
