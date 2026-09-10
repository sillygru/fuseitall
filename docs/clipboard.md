# Clipboard

How text and images sync between devices, in the direction the user chose.
In-memory only; bodies never reach logs (lengths only).

## What it does

Copy on one device, paste on the other — or restrict to one direction, or
turn it off. Manual push always works (bypasses mode + sensitive gates);
auto-sync follows the mode and the sensitive opt-in.

## How it flows

1. Watcher fires: Mac `backend/clipboard_watcher.go` (`changeCount` guard +
   450ms debounce, types-first sensitive check) or Android
   `clipboard_watcher.dart` (`OnPrimaryClipChangedListener → EventChannel
   fuseitall/clipboardEvents`, 350ms debounce). One-shot fetches run only
   on WS connect, resume, or mode toggle — never on a schedule.
   Phone → Mac from background goes through the focus trigger instead
   (Android 10+ blocks background reads at the OS level, see Rules):
   tapping the link notification's `Send to Mac` action or the Quick
   Settings tile opens the invisible `ClipSendActivity`, which takes window
   focus and invokes `fuseitall/clipSend.onClipFocus`; Dart then runs the
   normal manual send and acks so the activity closes. The opt-in
   `clipboard_auto_background` toggle (Settings, off by default) starts
   `ClipAutoWatcher`, which tails logcat for ClipboardService's denial line
   for our package and fires the same activity automatically.
2. Sender checks `ClipboardModeAllowsSend` (+ sensitive opt-in for auto),
   builds `clip-push{kind, origin, changed_at, changed_c, content_hash,
   sensitive, payload}`, stores latest-wins pending, sends via `WriteActiveWS`
   / `sendEnvelope` (fallback `POST /clip`). Large images (>5 MiB, ≤25 MiB)
   ride `clip-image-manifest` + N `clip-image-chunk` (1 MiB stride,
   sha256-verified) on the same `/clip` lane.
3. Receiver checks `ClipboardModeAllowsReceive` + HLC wins (lexicographic
   `(changed_at, changed_c)` strictly greater wins, ties keep local, 0 never
   wins) + nonce/hash dedupe, applies via `ApplyRemote`, arms echo
   suppression BEFORE the pasteboard write (disarms on failure), writes
   (`pbcopy`/`Clipboard.setData`, `writeText/writeImage` with sensitive
   flag), and emits. Stale/conflict drops log feed-only with lengths.
   Echo layers: origin + nonce LRU + content-hash + suppress window
   (Mac 900ms, Android 800ms) + `changeCount` baseline advance.

## Contract

- Capability `clipboard` (`packages/core/features.go`,
  `clipboard_sync.go`, `packages/proto/clipboard.json`).
- Types `clip-push` + `clip-image-manifest` + `clip-image-chunk` on
  `POST /clip` (+ WS). Manual `PushClipboard/Current/Image` bypasses the
  send-gate and the sensitive auto-gate; auto watcher and ingest respect
  both. Receivers always apply explicit sends (sender intent wins) and flag
  sensitivity on write so overlays redact.
- `clipboard_mode`: `both` (default) / `android_to_mac` / `mac_to_android`
  / `disabled`. `origin` (`mac`/`macos`→normalized→`mac`/`android`) breaks
  echo loops: receivers drop their own origin.
- `clipboard_allow_sensitive`: opt in to auto-syncing OS-flagged secrets
  (default false). Absent means false. Manual Send always bypasses.

## Key files

- Core: `features.go` (`TypeClipPush`, `RemoteClipWins`,
  `ClipboardMode*`, `SanitizeClip*`), `clipboard_sync.go` (HLC,
  `ContentHash*`, `TypeClipManifest/Chunk`, chunk sanitize, concealed types).
- Mac: `backend/clipboard.go` (`ClipStore` HLC/hash/nonce LRU),
  `backend/clipboard_chunk.go` (chunk hub + send splitter),
  `backend/clipboard_watcher.go`, `backend/clipboard_normalize.go`,
  `backend/pasteboard_change_*.go`, `backend/auto_sync.go` (dual
  `setDataForType` + retained `public.file-url`),
  `backend/service_features.go`, `frontend/src/components/ClipboardPane.svelte`,
  `frontend/src/components/SettingsPane.svelte`.
- Android: `features/clipboard/clipboard_sync.dart` (`ClipState` HLC/hash),
  `features/clipboard/clipboard_chunks.dart` (hub + Binder-safe channel IO),
  `features/clipboard/clipboard_watcher.dart`,
  `features/clipboard/clip_send.dart` (focus-trigger bridge),
  `features/settings/app_settings.dart` (`clipboard_auto_background`,
  local-only, stripped from the sync wire via `toSyncJson`, preserved
  across remote adopts via `withLocalFlagsFrom`), `MainActivity.kt`
  (listener sensitive flag, chunked `readLargeImageMeta/Chunk`,
  `begin/append/finishLargeImageWrite`, `fuseitall/clipSend` +
  `fuseitall/permissions` clip-auto methods), `ClipSendActivity.kt`
  (invisible transient focus activity, 12s failsafe), `ClipSendTileService.kt`
  (Quick Settings tile, PendingIntent variant on API 34+), `LinkService.kt`
  (`Send to Mac` notification action, watcher re-arm), `ClipAutoWatcher.kt`
  (opt-in logcat tail, adb `READ_LOGS` + overlay).

## Rules & limits

- Caps: text ≤262144B; inline image 5MiB raw (~7.1MiB b64); chunked image
  ≤25 MiB total (1 MiB stride, ≤32 chunks, sha256-verified). `mime ∈
  PNG/JPEG/WEBP/GIF/TIFF/HEIC/HEIF` + brand-aware magic sniff
  (PNG 8B, GIF87a/89a, WEBP RIFF-size, HEIC brand allowlist) +
  `SanitizeClipImage`; `filename` sanitized basename ≤255 else synthesized.
- Mac normalize: `TIFF/HEIC/HEIF → PNG` via `sips`; over-cap `PNG → JPEG
  q85` once; large images chunk without silent downscale. Bytes-exact
  `setDataForType` + retained `public.file-url` temp (deleted on next write,
  1h sweep) so Finder drags keep working.
- Sensitive: Android `EXTRA_IS_SENSITIVE`, macOS `ConcealedType` /
  `TransientType` / `AutoGeneratedType` / 1Password type. Auto skips loud
  unless `clipboard_allow_sensitive`; manual flags and sends. Receivers set
  the flag on write (redacted overlay).
- The 300ms `changeCount` guard is the single documented polling exception:
  one cgo int compare per tick while paired; unpaired or send-disabled
  short-circuits before any cgo/shell work; shells run only after the count
  changes. No network on a schedule — sends ride the push path. Allowed
  timers only: UI-clock interpolation, WS ping keepalive, this guard.
- Android 10+ background clipboard reads are blocked by the OS
  (`ClipboardService`: only the focused UID, the default IME, or a
  signature holder may read; a foreground service does NOT count, and
  background listeners are not even dispatched). So phone → Mac sync is
  focus-driven, never silent-failed:
  - Default one-tap (no extra permissions): copy anywhere, then tap `Send
    to Mac` in the FuseItAll link notification or add its Quick Settings
    tile. The tap is a BAL-exempt user interaction that opens the
    invisible `ClipSendActivity`; the read runs while our UID is focused
    (text + images, same caps and chunk lanes), then the activity closes.
  - Opt-in auto (`clipboard_auto_background`, Settings, off by default):
    one-time `adb shell pm grant <pkg> android.permission.READ_LOGS` +
    `adb shell appops set <pkg> SYSTEM_ALERT_WINDOW allow` +
    `adb shell am force-stop <pkg>`. The watcher tails its own denial
    lines and fires the same activity. The toggle is device-local: never
    on the settings-sync wire, never clobbered by Mac sync.
  Binder ~1 MiB ceiling:
  large payloads use chunked MethodChannel lanes, never single 7 MiB calls.

## Failure modes

- Mode disables a direction → watcher/ingest drops loud with the mode
  reason; manual push still sends.
- Sensitive auto → skipped (feed reason); manual still sends with flag.
- Over-cap/foreign-mime → dropped fail-soft, metadata survives, lengths
  logged only. Over 25 MiB → loud reject, use files lane.
- Chunk hash mismatch / missing chunk → session dropped fail-closed, loud.
- Conflict/stale (simultaneous copies) → last-writer-wins by HLC, loser
  logs feed-only with lengths (never bodies), no banner.
- Clock skew: HLC (`ClipStampNext/OnReceive`, lexicographic compare) absorbs
  skew; `now <= cur → cur+1` retained as the wall promotion.
