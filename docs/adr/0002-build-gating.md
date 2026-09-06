# ADR 0002 — Build gating: protocol first, then builds, then capability

## Context

Two independently-updated apps (Mac store builds, Android APKs) will skew.
A v2 phone talking to a v1 Mac must get a human-actionable answer, not a
hang, a 500, or a silently dropped packet. Both sides need one deterministic
check order with no drift between implementations.

## Decision

Gate in strict order — `protocol_v` → `min_peer_build` → `capability` →
`token` → logic (`server.go: handlePing`, mirrored by `ParseAndGateHeader`
+ `CheckPeerVersion` for embedders). Version state is three numbers:
`CurrentProtocolV`, `CurrentBuild`, `CurrentMinPeerBuild`, plus
`MinPeerBuildByProtocol` for known versions. Any higher `protocol_v`, any
`app_build < min_peer_build` (either direction), or any missing capability
yields `error/UPDATE_REQUIRED` with the canonical message `Update FuseItAll
on <device> to build >= N` (`NewUpdateRequiredPayload`). Local-outdated is
checked before peer-outdated so a device accuses itself before the peer.

## Consequences

- Users always see which device to update and to what build; support gets
  one sentence instead of a packet capture.
- Bumping compatibility is data (`MinPeerBuildByProtocol`, constants), not
  branching — no new code paths per release.
- Cost: capability checks can false-positive into UPDATE_REQUIRED for peers
  that simply disabled a feature; acceptable in BASE where `ping` is the
  only capability. Token checks run after version checks, so an outdated
  peer learns it is outdated rather than merely unauthorized.
