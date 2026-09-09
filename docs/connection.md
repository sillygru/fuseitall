# Connection

How the two apps find each other, stay connected in realtime, and reject
bad peers loudly. Pairing (QR/TOFU/unpair) lives in `pairing.md`;
presence/battery rides on top — see `presence.md`.

## Topology and ports

| Endpoint | Use |
|---|---|
| `18789/TCP/TLS` Mac HTTPS + `wss://<mac>:18789/ws` | `/ping`, `/health`, `/notif`, `/clip`, `/settings`, `/playback`, `/unpair`, `/files`, `/photos`, `/ws` |
| `18790/UDP` LAN discovery | beacons (Mac→LAN) + probes (phone→LAN), JSON per `discovery.json` |
| Phone ephemeral port | `go_server.dart` FFI `PhoneStart` result, advertised as `reply_port` + `reply_fingerprint` on every ping |

State files: Mac `~/Library/.../FuseItAll/pair.json` (token+cert, 0600) and
`device.json` (remembered peer); Android `flutter_secure_storage`
(`fuseitall_pairing`, phone identity, settings). Deleting `pair.json` is the
documented Mac reset.

## Discovery (dual-direction, <30ms, no polling)

- Mac: one `BroadcastBeaconUDP` on boot + continuous `ListenUDP :18790`
  (`packages/core/discovery.go`, `apps/mac/main.go`). A probe with matching
  fingerprint gets an immediate unicast `DiscoveryRecord` beacon in reply.
- Phone: continuous `BeaconListener` (`RawDatagramSocket.bind(anyIPv4,18790)`)
  + `broadcastProbe {v:1,type:probe,fp}` on open/resume
  (`features/connection/beacon_listener.dart`), plus parallel Happy-Eyeballs
  dials to remembered candidates (`features/connection/mac_locator.dart`,
  `net/phone_transport.dart`: `orderedTargets`, `remember winner`,
  ping 10s/host, feature 3s/host, `/24` sweep cap 16).
- Result: whichever side opens last connects immediately; offline devices
  do not poll.

## Persistent WebSocket (the only realtime path)

- Phone holds one TLS WebSocket to the Mac (`wss://host:port/ws`,
  `Authorization: Bearer <token>`, TOFU-pinned — see `pairing.md`).
  `packages/core/server_ws.go:handleWS` authenticates (header or `?token`),
  enforces 8 MiB read limit and a 15s/10s ping watchdog (2 strikes → close).
- Mac `apps/mac/backend/service_ws.go`: `OnWSConnect` (store `activeWS`,
  emit `state:changed`, flush pending) → `OnWSEnvelope` (gate + ingest per
  type) → `OnWSDisconnect` (clear peer, fail pending, emit offline).
  `WriteActiveWS` delivers Mac→phone pushes in ~0ms.
- Phone `net/phone_websocket.dart`: `fastConnect` (50ms-staggered Happy
  Eyeballs, epoch-guarded), `sendEnvelope`, `onDone/onError → disconnected`.
  `transport.sendFeatureWithFallback` prefers WS, falls back to HTTPS.
- Offline detection is socket-close driven (TCP FIN/EOF → both UIs go
  offline immediately). One-shot fetches happen only on connect, resume,
  mode toggle, or explicit retry with backoff.

## Envelope and version gate

Shape (`packages/proto/envelope.json`, `packages/core/envelope.go`,
`version.go`): `Envelope{protocol_v, type, sender{platform, app_build,
min_peer_build, app_version?}, capabilities[], payload?}`.
Current: `protocol_v=1`, `build=10` (`0.10.0`), `min_peer_build=1`.

Gate order on every message (`server.go:handlePing/handleFeature`,
`server_ws.go`): `protocol_v → min_peer_build (both directions, local first)
→ capability → token → logic`. Any version/capability failure answers
`error/UPDATE_REQUIRED` (`packages/proto/update-required.json`,
`NewUpdateRequiredPayload`): `Update FuseItAll on <device> to <reqVer>
(build >= N); current <curVer> (build M)`, falling back to build-only when
unmapped. Every handler echoes the request nonce in its pong so senders can
match acks fail-closed (`ErrNonceMismatch` otherwise). Unknown fields are
ignored (forward tolerance). Capabilities: `ping`, `notifications`,
`clipboard`, `settings-sync`, `files`, `photos`, `playback`.

## Loud errors (never silent-drop)

| Wire | Meaning |
|---|---|
| `426 error/UPDATE_REQUIRED` | outdated peer or missing capability → `*UpdateRequiredError` (`ErrLocalOutdated`/`ErrPeerOutdated` via `errors.Is/As`); UI shows banner with "Requires X, current Y" + verbatim message |
| `403 UNAUTHORIZED` | token failure, generic message (no oracle); Mac-unpaired phones get "scan its new QR" |
| `400 BAD_REQUEST` | malformed input, generic message; detail only in server `slog` (`reason`, no tokens/PII) |

Clients branch on `code`, never on the message string. Android renders via
`result.dart` (`UpdateRequired/AuthFailure/NetworkFailure/...`) +
`SelectableText.rich`; Mac via `UpdateNotice` + state banners.

## Debug pointers

- Mac `GetPeerAddr/IsPaired/GetUpdateNotice/GetLastDevice/GetPeerDevice`;
  log prefixes: `UPDATE_REQUIRED[_SELF]`, `phone peer captured/lost`,
  `phone cert pinned`.
- Phone: `adb logcat` for `BeaconListener`/`phone_websocket`/`proto_client`;
  Mac: activity feed for WS connect/disconnect and gate rejections.
- `MaxBodyBytes 8MiB`; chunks are 1 MiB raw (see `files.md`, `photos.md`).
