# AGENTS.md — FuseItAll (free, open-source Android <-> Mac link)

FOSS clone of LinkMyMac-class workflows. License: **AGPL-3.0-only** (see `LICENSE`).
Every new hand-written source file carries a one-line SPDX header, never the
full license text. No exceptions for hand-written code. Generated files (Wails
bindings, Flutter ephemera), JSON schemas, and YAML/TOML config are exempt.
Forms: `// SPDX-License-Identifier: AGPL-3.0-only` (Go, Dart, Kotlin, TS without
doc block), `# SPDX-License-Identifier: AGPL-3.0-only` (shell, Python),
`/* SPDX-License-Identifier: AGPL-3.0-only */` (CSS) or first line of the leading
doc block (`/*` / `<!--`) for TS/Svelte with docs, `<!-- SPDX-License-Identifier:
AGPL-3.0-only -->` (HTML, SVG, XML, Markdown).

## Layout

```
apps/mac/        # Wails v3 (Go backend) + Svelte 5 + Tailwind v4 + shadcn-svelte
apps/android/    # Flutter (Dart), feature-first
packages/core/   # pure Go: identity, pairing, transport, ping, version gate. No UI, no cgo.
packages/proto/  # the ONLY Go<->Dart contract: envelope.json, pair-qr.json, ping.json, pong.json, update-required.json
docs/            # structure.md, connection.md + one file per feature (flat)
```

## Laws

- `packages/core` never imports UI. UI depends on core, never reverse.
- `packages/proto` is the only cross-language contract. No ad-hoc JSON, no duplicated structs.
- Adapters thin: translate at the seam only, zero business logic.
- NEVER polling: no `Timer.periodic`, `Ticker`, FFI poll loops, or HTTP heartbeat timers for sync/state. Use push instead: native callbacks → `EventChannel` → persistent WebSocket → Wails event → runes. One-shot fetches only on events (connect, resume, mode toggle, explicit retry with backoff). The sole timer allowed is a local UI-clock interpolation (e.g. playback progress from `PositionMs`+`UpdatedMs`) that never touches the network. If an OS truly offers no callback, document why in the feature doc and scope the fallback to that seam only. See `docs/connection.md` and `docs/playback.md`.
- Skills live ONLY in `.agents/skills/`.
- Go: check every error, `fmt.Errorf("...: %w", err)`, `errors.Is/As`, log-OR-return, `slog`
  structured, `crypto/rand` tokens, `ConstantTimeCompare` secrets, fail closed.
- UI: real objects/states only; Tailwind semantic tokens, never `@apply`; Svelte 5 runes;
  Flutter `Either`, short fns, errors via `SelectableText.rich`.
- Mac UI: read `.agents/skills/macos-hig-wails/SKILL.md` before any Mac-window UI work
  and follow it; project override (taste, not Apple): no Liquid Glass anywhere —
  no refraction, no specular highlights, no morphing glass. Classic pre-glass
  frost is welcome: translucent sidebar/toolbar materials with blur + saturation
  and vibrant foregrounds (`NSVisualEffectView`-style: semantic material per
  surface, solid opaque content layer), with a solid-fill fallback under
  `prefers-reduced-transparency`.
- Android UI: read `.agents/skills/android-hig-flutter/SKILL.md` before any Android Flutter UI work
  and follow it; project override (taste, not Apple/Material): no Liquid Glass anywhere —
  no refraction, no specular highlights, no morphing glass. Classic pre-glass
  frost is welcome: Material 3 surface + tonal elevation (translucent AppBar/sheet scrim with
  blur + saturation only as a transient overlay, never as content-layer glass), solid opaque
  content layer, with a solid-fill fallback when blur is unavailable or `disableAnimations` is set.
- Thermo gate: no file past 1000 lines, no spaghetti branch in another module's flow, no thin wrappers.
- Outlines strictly forbidden: NO CSS outlines, NO ring-* classes, NO decorative border rings or simulated box-shadow outlines (such as 0 0 0 1px ...) anywhere in the UI. Focus and structure come strictly from subtle tonal surfaces, fills, shadows, and spacing.

## Commands

```
task test        # go test -race ./... + flutter test (when present)
task build:mac   # wails3 build (host darwin/arm64; v3 beta has no --platform flag)
task build:android  # flutter build apk (needs Android env)
flutter analyze && flutter test   # in apps/android
```

## Version-gating contract (summary, full spec in docs/connection.md)

Every wire message = `Envelope{protocol_v, type, sender{platform,app_build,min_peer_build},
capabilities[]}`. Check order: protocol_v -> min_peer_build -> capability -> token -> logic.
Higher `protocol_v` or `app_build < min_peer_build` -> `error/UPDATE_REQUIRED`, canonical
message `"Update FuseItAll on <device> to build >= N"`. Never silent-drop. Unknown fields ignored.

## Skills routing (.agents/skills/)

- Go: `golang-error-handling`, `golang-security`
- Mac UI: `macos-hig-wails` (read first for any Mac-window UI work), `svelte`, `sveltekit-structure`, `tailwind-v4-shadcn`, `design-taste-frontend` (+ `vercel-react-best-practices` fallback)
- Android: `android-hig-flutter` (read first for any Android Flutter UI work), `flutter`
