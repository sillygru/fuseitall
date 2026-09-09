# Presence

How the Mac knows the phone is alive, what it is, and how much battery it
has. Transport underneath: `connection.md`. Trust bootstrap: `pairing.md`.

## What it does

The phone announces itself (`ping → pong`) with device facts and battery;
the Mac renders name/model/battery and treats the peer as paired only while
the data is fresh or the WebSocket is live. Battery-only pushes keep the
tile current without a full scan.

## How it flows

1. Phone builds `ping` (`features/ping/proto_client.dart:buildPingEnvelope`):
   nonce + `reply_port`/`reply_fingerprint` (ephemeral FFI server from
   `net/go_server.dart:PhoneStart`) + facts
   `{device_name, model, battery_pct, charging, files_permission,
   photos_permission}`. Sender stamps `android/kAppBuild/kMinPeerBuild`.
2. Sends via `net/phone_transport.dart:pingWithFallback` (WS-first, then
   ordered remembered hosts, 10s/host) to `POST https://<mac>:18789/ping`.
3. Mac `server.go:handlePing` gates
   (`protocol_v → min_peer_build → capability ping → token`) then
   `service.go:WrapHandler/setPeerWithFacts` records host (from RemoteAddr),
   `reply_port`, `reply_fp`, and facts (`ParsePeerDevice`,
   `ParsePeerPingFull`). Replies `pong{nonce, receivedAt}`.
4. Mac→phone direction (`pingPhone`) uses the learned `peerHost:port` with
   the pinned TOFU client; port-less battery pushes only `mergeFacts()`,
   never evict.
5. UI: Mac `DeviceHero.svelte`/`StatusPill` via `backend.ts:getPeerDevice/
   getLastDevice`; Android `features/home/connection_hero.dart`,
   `paired_devices_card.dart`, `features/ping/ping_page.dart`
   (`_announcePresence`, `_onBatteryReading`, `_connectFast`).

## Contract

- Capability `ping` (`packages/core/envelope.go`, `packages/proto/ping.json`,
  `pong.json`). `GET /health` is the unauthenticated health check.
- Nonce echo is strict: mismatch → `NonceMismatch`, never applied.
- 426 → `UpdateRequired` (verbatim message); 403 → `AuthFailure`;
  200 → pong applied.

## Key files

- Proto: `envelope.json`, `ping.json`, `pong.json`, `update-required.json`,
  `discovery.json`.
- Core: `envelope.go`, `server.go`, `client.go` (`SendPing`), `discovery.go`
  (`MergeCandidateHosts`, `OrderedPeerTargets`).
- Mac: `backend/service.go`, `backend/device_facts.go`,
  `backend/peer_caps.go`, `backend/persist.go`
  (`LoadLastDevice/StoreLastDevice`), `frontend/src/components/DeviceHero.svelte`.
- Android: `features/ping/*`, `features/device/device_info_provider.dart`,
  `features/device/battery_watcher.dart`
  (`EventChannel fuseitall/battery` + `distinct()`),
  `features/connection/mac_locator.dart`,
  `features/connection/beacon_listener.dart`.

## Rules & limits

- Paired = `activeWS != nil` OR `lastSeen < peerTTL (60s)`. WS liveness and
  HTTP return-path freshness are different signals: a failed HTTP dial never
  clears a healthy socket (`pingPhone` keeps the WS peer, logging
  `peer-lost-ws` for the return path only). Ghost sockets are owned by the
  5s control-ping watchdog (~10-15s close, pings serialize behind data
  writes) plus the explicit local-network-down push — never by TTL expiry.
  The 5s watchdog refreshes `lastSeen` via `OnWSPing`, and the phone's own
  15s/10s keepalive converges its side, so idle-but-connected peers stay
  paired on both ends. `NotifyLocalNetworkDown` (frontend `offline` event
  push, guarded by freshness) drops the peer instantly on Mac WiFi loss.
  Disk-restored
  peers never gate authoritatively; `refreshPeer/clearPeer/isPeerLost`
  decide, with `logRotationOnce(60s)` collapsing `cert-mismatch/peer-lost`
  spam.
- Facts are fail-soft per field; rename alias overrides `device_name`
  display-only, `model` never renamed.
- Offline battery readings cache as `_pendingBattery` and ride the next
  announce; WS drops re-anchor once on reconnect (`_announcePresence`).
- DHCP heal: remembered hosts (max 4, `fuseitall_mac_hosts`) + `/24` sweep
  (cap 16) + `fastConnect` 50ms stagger.

## Failure modes

- Stale peer (>60s, no WS) → offline tile, no polling; next beacon/probe
  or manual reconnect heals in <30ms.
- 426 on ping → update banner (self vs peer via `ErrLocalOutdated`/
  `ErrPeerOutdated`), facts still shown from cache.
- 403 on ping → phone shows revoked-by-Mac ("scan its new QR").
