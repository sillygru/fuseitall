# ADR 0005 — First-use trust on the Mac → phone return path

Date: 2026-09-06. Status: accepted (BASE scope).

## Context

The phone pins the Mac's cert via the scanned QR (TOFU with an
out-of-band fingerprint — strict). The reverse direction has no such
out-of-band channel: the phone presents no client cert to our server
(`core.Serve` has no ClientAuth, and adding it would change the frozen
core contract), so the Mac cannot learn the phone's cert fingerprint
from any handshake. Yet the [Ping phone] button needs a TLS client to
the phone's listener.

## Decision

Blind-TOFU with the narrowest possible blast radius, no core changes:

1. The return path is only learned from an **accepted** phone ping
   (HTTP 200 through the full version + token gate). Rejected pings
   never teach us coordinates.
2. The first outbound ping TOFU-accepts the presented phone cert, pins
   it (`phone cert pinned fingerprint=<hex>` in the log), and all later
   pings use `core.NewTOFUClient(pin)` — a changed cert fails closed.
3. The return path stays authenticated by the pair token (`Authorization:
   Bearer`), which the phone proved over the QR-pinned channel when it
   pinged us first. An attacker winning the first-use race still needs
   the 128-bit token to get a 200 from either side.

## Threat model (honest)

A LAN MITM present **exactly** at the first outbound ping can pin its
own cert into the Mac permanently. Token auth still blocks API abuse,
but transport identity would be the attacker's. Mitigations deferred
out of BASE: phone advertises its cert fingerprint inside the first
ping payload (schema addition), or mutual TLS via a core change.

## Consequences

- No silent trust: the pin event is logged, and this ADR records the
  residual risk so a future review (or the phone-fingerprint-payload
  upgrade) can close it deliberately.
- `backend/service.go:phoneClient` keeps the `#nosec G402` justification
  next to the mechanism.
