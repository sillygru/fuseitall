# ADR 0006 — App version 0.1.0: builds gate, versions display

## Context

The wire already gates on numbers (`protocol_v`, `app_build`,
`min_peer_build` → `error/UPDATE_REQUIRED`, ADR 0002). But users saw only
"build >= N" with no human version, and the three version declarations
drifted (`pubspec 1.0.0+1`, `package.json 0.0.0`, no Go constant). A
non-backwards-compatible release needs a reliable "requires X, current Y"
answer in both directions (new Android vs old Mac and vice versa).

## Decision

- Single source of truth: `packages/core/version.go: CurrentAppVersion =
  "0.1.0"`, `CurrentBuild = 1`, plus `BuildToVersion{1: "0.1.0"}`. Manifests
  mirror it (`pubspec 0.1.0+1`, `frontend/package.json 0.1.0`); `task
  version:check` fails the build on drift.
- Wire: `sender.app_version` (optional, display-only) and
  `payload.required_version/current_version/current_build` (optional) in
  `packages/proto` (`envelope`, `ping`, `pong`, `update-required`). Required
  fields unchanged; old peers omit the keys, new decoders ignore unknown
  fields. **Builds gate, versions display** — `CheckPeerVersion` never reads
  the version string.
- Message: `Update FuseItAll on <device> to <reqVer> (build >= N); current
  <curVer> (build M)`, falling back to legacy `to build >= N` when the
  required build is unmapped. Branch on `code == UPDATE_REQUIRED`, never on
  the string (ADR 0004).
- Typed state carries versions end-to-end: Go `UpdateRequiredPayload` /
  `UpdateRequiredError`, Mac `UpdateNotice{RequiredVersion, CurrentVersion,
  RequiredBuild}` + `GetAppVersion` binding, Dart `UpdateRequired` failure +
  `lib/version.dart` mirror. Banners render "Requires X, current Y" plus the
  verbatim message; footers show `v0.1.0 (build 1)`.
- Bump procedure for a breaking change: bump `CurrentAppVersion` +
  `CurrentBuild`, add the `BuildToVersion` entry, raise `CurrentMinPeerBuild`
  only when old peers must be cut off, mirror manifests, add the new version
  to the Dart map. No new code paths per release (data, not branching).

## Consequences

- Users always see which device to update and to what human version;
  support gets one sentence instead of a build-number lookup.
- Old builds keep working: unknown version keys are ignored, missing keys
  degrade to build-only messaging, and the 426/verb stays loud (never
  silent-drop).
- Cost: three places must be bumped in lockstep (core, pubspec,
  package.json) — enforced by `task version:check` instead of discipline.
