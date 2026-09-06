# ADR 0003 — QR versioning: missing v means 1, newer v is rejected

## Context

The QR is scanned by a camera and parsed with no handshake to negotiate
format, so the payload must be self-describing yet decodable by the very
first shipped build. Future QR fields (new transports, new identity types)
must not brick old scanners, and old QRs (screenshots, prints) must not
confuse new scanners.

## Decision

The QR JSON carries `v` (QR *format* version, independent of wire
`protocol_v`). `ParsePairQR`: missing `v` defaults to 1; `v > 1` (or `< 1`)
fails closed with `ErrUnsupportedProtocol`; unknown fields are ignored so v1
readers tolerate v2 QRs' extras (they still reject on the version number
itself). Writers emit v1 only — `MakePairPayload` takes no version args and
`EncodePairQR` normalizes to 1. The rest of the payload is the minimal join
kit: endpoint (`host`/`port`), TOFU pin (`fingerprint`), device identity
(`pubkey`), and authenticator (`token`).

## Consequences

- v1 scanners survive unknown future fields; v2 scanners still read v1 QRs
  (screenshots keep working).
- A newer QR on an old phone errors loudly at scan time instead of
  connecting to the wrong thing.
- Cost: format evolution is capped at additive changes until a v2 reader
  ships; breaking QR changes require a new `v` and an app update, which is
  exactly the failure mode the version gate already messages.
