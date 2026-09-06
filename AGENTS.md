# AGENTS.md — FuseItAll (free, open-source Android <-> Mac link)

FOSS clone of LinkMyMac-class workflows. License: **AGPL-3.0** (see `LICENSE`).
Every new hand-written source file (Go, Dart, Svelte/TS/CSS, shell) carries the
AGPL-3.0 header notice. No exceptions. Generated files (Wails bindings, Flutter
ephemera), JSON schemas, and YAML/TOML config are exempt. No exceptions for
hand-written code.

## Layout

```
apps/mac/        # Wails v3 (Go backend) + Svelte 5 + Tailwind v4 + shadcn-svelte
apps/android/    # Flutter (Dart), feature-first
packages/core/   # pure Go: identity, pairing, transport, ping, version gate. No UI, no cgo.
packages/proto/  # the ONLY Go<->Dart contract: envelope.json, pair-qr.json, ping.json, pong.json, update-required.json
docs/adr/        # 0001 envelope, 0002 build-gating, 0003 QR versioning, 0004 loud-errors
```

## Laws

- `packages/core` never imports UI. UI depends on core, never reverse.
- `packages/proto` is the only cross-language contract. No ad-hoc JSON, no duplicated structs.
- Adapters thin: translate at the seam only, zero business logic.
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
- Thermo gate: no file past 1000 lines, no spaghetti branch in another module's flow, no thin wrappers.

## Commands

```
task test        # go test -race ./... + flutter test (when present)
task build:mac   # wails3 build (host darwin/arm64; v3 beta has no --platform flag)
task build:android  # flutter build apk (needs Android env)
flutter analyze && flutter test   # in apps/android
```

## Version-gating contract (summary, full spec in docs/adr/)

Every wire message = `Envelope{protocol_v, type, sender{platform,app_build,min_peer_build},
capabilities[]}`. Check order: protocol_v -> min_peer_build -> capability -> token -> logic.
Higher `protocol_v` or `app_build < min_peer_build` -> `error/UPDATE_REQUIRED`, canonical
message `"Update FuseItAll on <device> to build >= N"`. Never silent-drop. Unknown fields ignored.

## Skills routing (.agents/skills/)

- Go: `golang-error-handling`, `golang-security`
- Mac UI: `macos-hig-wails` (read first for any Mac-window UI work), `svelte`, `sveltekit-structure`, `tailwind-v4-shadcn`, `design-taste-frontend` (+ `vercel-react-best-practices` fallback)
- Android: `flutter`
