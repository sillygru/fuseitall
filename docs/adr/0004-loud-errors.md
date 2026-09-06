# ADR 0004 — Loud errors: every rejection gets a typed reply

## Context

Silent drops are the worst LAN failure: two devices stare at each other,
both look fine, nothing works, and logs live on two different machines.
Small teams cannot afford that debug loop. Every rejection must therefore
produce a typed, displayable error on the requester's side.

## Decision

The server never silent-drops: malformed bodies, wrong types, bad tokens,
and version mismatches all get a reply envelope. Version outcomes use
`error/UPDATE_REQUIRED` (HTTP 426) with the canonical message; token failure
uses code `UNAUTHORIZED` (HTTP 403) with a generic message that reveals
nothing about why verification failed; malformed input uses `BAD_REQUEST`
(HTTP 400) with a generic message while technical detail goes only to
server-side `slog` (stable low-cardinality `reason`, no tokens, nonces, or
PII). The client maps `UPDATE_REQUIRED` to `*UpdateRequiredError`, whose
`Unwrap` yields `ErrLocalOutdated`/`ErrPeerOutdated` for `errors.Is/As`, so
UI layers branch on sentinels, not string matching.

## Consequences

- Every failure is showable in UI (`SelectableText.rich` on Android,
  real-state banners on Mac) and greppable in logs by one `reason` value.
- Generic client messages close oracle/leak avenues (no token oracle, no
  version fingerprinting beyond what the gate already states).
- Cost: one extra envelope encode per rejection and discipline in handlers
  (log-OR-write, single reply per request). Forgetting a return after a
  reject double-writes — handlers must return immediately after responding.
