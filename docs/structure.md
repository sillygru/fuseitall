# Project structure

Where code lives, what may depend on what, and how to build/test.
Details of the wire and each feature live in `connection.md` and the
per-feature files — this file is only the map.

## Layout

```
apps/mac/        # Wails v3 (Go backend) + Svelte 5 + Tailwind v4
  main.go        # wiring only: identity/token/cert -> core.Server -> backend.Service
  backend/       # thin Wails adapter: intents -> core, no pairing/gating/transport logic
  frontend/src/  # Svelte 5 runes: App.svelte, backend.ts, components/, lib/paneCache.ts
apps/android/    # Flutter (Dart), feature-first
  lib/features/  # pairing, ping, connection, device, notifications, clipboard,
                 # settings, files, photos, playback, contacts, messages, home, permissions
  lib/net/       # phone_transport.dart, phone_websocket.dart, go_server.dart,
                 # phone_identity_store.dart
  lib/version.dart, result.dart (Either), navigation/, widgets/
  go_bridge/     # thin c-shared adapter over core (PhoneStart/Stop/CertPEM/...),
                 # no poll queue; realtime rides the WebSocket
packages/core/   # pure Go: identity, pairing, transport, ping, version gate.
                 # No UI, no cgo. Only dep: coder/websocket (indirect).
  version.go, envelope.go, client.go, server.go, server_ws.go,
  identity.go, pairing.go, discovery.go,
  features.go, files.go, photos.go, playback.go, contacts.go, messages.go
packages/proto/  # the ONLY Go<->Dart contract (JSON Schema draft-07, 16 files)
  envelope, pair-qr, ping, pong, update-required, discovery,
  clipboard, notifications, notif-apps, files, photos, playback,
  settings, unpair, contacts, messages
docs/            # this file + connection.md + one file per feature (flat)
```

## Laws (from `AGENTS.md`)

- `packages/core` never imports UI. UI depends on core, never reverse.
  `packages/core/go.mod` depends only on `coder/websocket`;
  `apps/mac/go.mod` and `go_bridge/go.mod` `replace fuseitall/core`.
- `packages/proto` is the only cross-language contract. No ad-hoc JSON,
  no duplicated structs. Decoders ignore unknown fields; encoders always
  emit the four required header fields. Dart `lib/version.dart` mirrors
  Go `version.go`.
- Adapters thin: translate at the seam only, zero business logic
  (`apps/mac/backend`, `apps/android/go_bridge`).
- Never polling: native callbacks → `EventChannel` → persistent WebSocket
  → Wails event → runes. One-shot fetches only on events (connect, resume,
  mode toggle, explicit retry with backoff). Allowed timers only:
  local UI-clock interpolation (playback progress, never the network),
  WS ping keepalive (protocol health), and the single documented Mac
  clipboard `changeCount` guard (see `clipboard.md`).
- Thermo gate: no file past 1000 lines, no spaghetti branch in another
  module's flow, no thin wrappers.
- Go: check every error, `fmt.Errorf("...: %w", err)`, `errors.Is/As`,
  log-OR-return, `slog` structured, `crypto/rand` tokens,
  `ConstantTimeCompare` secrets, fail closed.
- UI: real objects/states only; Tailwind semantic tokens, never `@apply`;
  Svelte 5 runes; Flutter `Either`, short fns, errors via
  `SelectableText.rich`. No Liquid Glass; classic frost only.

## Wire in one paragraph

Every message is `Envelope{protocol_v, type, sender{platform, app_build,
min_peer_build, app_version?}, capabilities[], payload?}`.
Gate order: `protocol_v → min_peer_build → capability → token → logic`.
Mismatch → `error/UPDATE_REQUIRED` with canonical
`Update FuseItAll on <device> to <ver> (build >= N); current <ver>`
(never silent-drop). Full spec: `connection.md`.

## Commands (`Taskfile.yml`)

```
task test               # version:check + test:core + test:android
task version:check      # core/version.go == pubspec.yaml == package.json == version.dart
task test:core          # cd packages/core && go test -race ./...
task test:android       # cd apps/android && flutter analyze && flutter test
task vet                # go vet + gosec + govulncheck on core
task build:mac          # cd apps/mac && wails3 build (host darwin/arm64)
task build:android      # sh apps/android/go_bridge/build_android.sh
task build:android:quick# ... build_android.sh arm64 (~8s debug)
task dev:mac            # cd apps/mac && wails3 dev
```

## Versions

Single source of truth: `packages/core/version.go`
(`CurrentProtocolV=1`, `CurrentBuild=13`, `CurrentMinPeerBuild=1`,
`CurrentAppVersion="0.13.0"`, `BuildToVersion`, `MinPeerBuildByProtocol`).
Mirrors enforced by `task version:check`: `apps/android/pubspec.yaml`,
`apps/android/lib/version.dart`, `apps/mac/frontend/package.json`.
Bump via `scripts/bump_version.py`. Builds gate, versions display only.

## Docs index

- `connection.md` — discovery, WebSocket, envelope/gating, loud errors.
- `pairing.md` — QR, TOFU, confirm, unpair/rotation.
- `presence.md` — ping, device facts, battery.
- `notifications.md`, `clipboard.md`, `settings.md`, `files.md`,
  `photos.md`, `playback.md`, `contacts.md`, `messages.md` — one file per feature.
