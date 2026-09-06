# ADR 0001 — Envelope: every wire message carries identity + version

## Context

Mac (Go/Wails) and Android (Flutter/Dart) exchange messages over LAN HTTPS.
Without a shared header, each new message type would re-litigate versioning,
sender identity, and capability checks, and old/new builds would fail in
silent, undebuggable ways. The only cross-language contract lives in
`packages/proto`; Go structs in `packages/core` must match it exactly.

## Decision

Every wire message is an `Envelope{protocol_v, type, sender{platform,
app_build, min_peer_build}, capabilities[], payload?}`. The header prefix
(`version.go: Header`) is decodable and gateable without trusting the rest
of the bytes: `ParseAndGateHeader` checks `protocol_v` before the full
decode, then peer builds. Decoders ignore unknown fields (forward tolerance);
encoders must always emit the four required header fields. Type-specific
shapes (`ping.json`, `pong.json`, `update-required.json`) only constrain
`type` and `payload`.

## Consequences

- Adding a message type means one schema + one payload struct; gating,
  errors, and client/server plumbing are reused untouched.
- Old readers tolerate newer senders' extra fields, but never newer
  `protocol_v` (fail closed, loud `error/UPDATE_REQUIRED`).
- Cost: a few dozen bytes of header per message — irrelevant on LAN ping
  cadence. Risk if ignored: any ad-hoc JSON outside the envelope forks the
  contract and breaks the Dart side silently.
