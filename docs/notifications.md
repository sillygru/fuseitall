# Notifications

How phone notifications appear on the Mac, stay filtered per app, and
dismiss both ways. Live-only mirror: restart shows only new items.

## What it does

The phone posts notifications to the Mac; the Mac banners them, lets the
user dismiss (which clears the phone too), and lets the user mute/allow
whole apps. A phone-side app inventory powers the per-app picker.

## How it flows

1. Phone `notif_listener.dart` (needs user-granted notification-listener
   grant) → `notif_filter.dart:shouldMirror()` pre-queue check →
   `NotifOutbox` (cap 50 posts + 50 dismissals) → `sendEnvelope` over WS
   (fallback `POST /notif`).
2. Mac `server.go:handleFeature` gates
   (`protocol_v → min_peer_build → capability notifications → token`) →
   `service_features.go:ingestNotifBody` → `notifications.go:NotifStore`
   (cap 100) → Wails event → `NotificationsPane.svelte`/`NoticeRow.svelte`.
3. Dismiss either direction: local `Dismiss()` + `pendingDismiss[50]` +
   `flushPendingToPhone()` → `ApplyRemoteDismiss` on the other side.
4. App inventory: Mac `notif_apps.go:RequestPhoneNotifApps` sends
   `notif-apps-req{req_id, cursor, limit}`; phone `notif_apps_sync.dart`
   answers one `notif-apps-resp{req_id, entries, next_cursor}` per request
   over the same `/notif` lane. Mac caches 30s (`GetKnownNotifApps`).

## Contract

- Capability `notifications` (`packages/core/features.go`,
  `packages/proto/notifications.json`, `notif-apps.json`).
- Types on `POST /notif` (+ WS): `notif-post`, `notif-dismiss` (either
  direction), `notif-apps-req` (Mac→phone), `notif-apps-resp` (phone→Mac).
- Filter fields ride `settings.json`: `notif_mode` (`all_except_muted`
  default allow-all / `only_allowed`) + `muted_packages[100]` /
  `allowed_packages[100]`. Single predicate `core.ShouldMirrorNotif`
  mirrored in Dart + native `updateNotifFilter` pre-queue.
- Inventory paging: cursor = last package, limit 1..50 (default 50),
  500 total cap, `next_cursor` empty = done.

## Key files

- Core: `features.go` (`TypeNotifPost/Dismiss/AppsReq/Resp`,
  `ShouldMirrorNotif`, `SanitizeNotif*`).
- Mac: `backend/notifications.go` (store, 30s per-ID banner cooldown,
  300s stale horizon, icon cache), `backend/notif_apps.go`,
  `backend/service_features.go`, `backend/notifications_notify*.go`,
  `frontend/src/components/NotificationsPane.svelte`, `MirrorPane.svelte`.
- Android: `features/notifications/notif_models.dart`,
  `notif_listener.dart`, `notif_filter.dart`, `notif_apps_sync.dart`.

## Rules & limits

- Stale horizon: `posted_at` (from `sbn.postTime`) >300s old is dropped
  both sides (`IsStaleNotifPost`); stopped/progress states are explicit
  pushes or nothing — never silence that leaves the Mac stale.
- Progress notifications never banner: sender drops `has_progress`
  pre-send; native + Dart + Mac triple-guard, fail-soft.
- Identical same-ID repost = silent refresh (no +unseen); changed content
  = +unseen subject to the 30s per-ID cooldown.
- Caps (truncate fail-soft, never log verbatim): id 1..256, app 64,
  title 128, text 512. Icons: PNG 96px b64 ≤32768, cached by
  `package_name`, `_sentIconPackages{100}` dedupe, fail-soft drop.
- Old phone answers `400 wrong_type` to `notif-apps-req` → Mac falls back
  to the mirrored known-apps view, never blocks the update banner.

## Failure modes

- Missing capability / old build → `UPDATE_REQUIRED` (a disabled feature
  reads as outdated — accepted).
- Over-cap fields → truncated/dropped loud (ID only in logs), never a
  full-page failure; one bad thumb never fails the list.
