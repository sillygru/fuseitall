# ADR 0007 — Features sync: notifications, clipboard, settings

## Context

BASE (0.1.0) proved presence: ping, battery, TOFU, version gating. The next
milestone (0.2.0) adds the first real features — notification mirroring,
clipboard sync, and app settings — without forking the contract or the
trust model. All three are small-payload, low-permission flows that reuse
the envelope, so they validate the multi-feature architecture before bulk
transfer (files/photos/mirror) forces a transport upgrade.

## Decision

- New message types on new routes, same gate: `notif-post/notif-dismiss`
  on `/notif`, `clip-push/clip-request` on `/clip`, `settings-sync` on
  `/settings` (`core/server.go: handleFeature`). Gate order is unchanged
  (`protocol_v` → `min_peer_build` → `capability` → `token` → logic);
  every handler replies with a pong echoing the request nonce, so senders
  match acks fail-closed with the existing nonce check. `protocol_v`
  stays 1; `CurrentBuild` becomes 2 with `MinPeerBuild 1` (build-1 peers
  still ping, they 426 feature calls via the capability check).
- Capabilities `notifications/clipboard/settings-sync` gate per message,
  never per connection. Missing capability answers `UPDATE_REQUIRED`
  like ping (a disabled feature reads as outdated — acceptable, ADR 0002).
- Settings sync is last-writer-wins on `updated_unix` (ties go to mac),
  sent immediately when paired else on the next heartbeat; clipboard is
  latest-wins on `changed_at` with origin echo-suppression; notifications
  are a bounded mirror (100 Mac / 50-phone outbox) with dismissals
  converging both ways.
- Privacy: clipboard and notification bodies never reach logs (lengths and
  IDs only), clipboard lives in memory only, settings persist locally
  (`settings.json`, 0600). Android 10+ background clipboard reads return
  null — sync is foreground-driven, documented, never silent-failed.
- Sidebar becomes a feature registry (Notifications, Clipboard, Phone,
  Settings) over the assumed single phone; no multi-phone model is added.

## Consequences

- Adding the next feature is one schema + one payload + one route entry;
  gating, acking, TOFU, and tests are reused untouched (ADR 0001).
- Mac→phone pushes require the bridge event queue (`PhonePollEvent`):
  Dart polls feature envelopes the core server accepted and applies them;
  older `.so` builds miss pushes but keep presence (symbol is optional).
- Cost: heartbeat now flushes queues (bounded batches so one burst never
  blocks presence), and the phone needs a user-granted notification
  listener plus an `.so` rebuild (`task build:android`).
