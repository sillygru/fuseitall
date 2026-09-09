# Pairing

How the phone and Mac trust each other once, then forget on demand.
Steady-state transport (discovery, WebSocket, gating) lives in
`connection.md`.

## What it does

One QR scan bootstraps mutual trust: the phone learns the Mac's TLS
identity out-of-band, both sides share a 128-bit token, and later
connections authenticate with that token plus pinned certs. Either side can
unpair; Mac-initiated unpair rotates the token so the old QR dies.

## How it flows

1. Mac mints QR (`apps/mac/main.go`, `packages/core/pairing.go`):
   `MakePairPayload` (fixed QR format `v=1`) + `EncodePairQR` over
   `{v, device_name, platform, host, port=18789, fingerprint, pubkey,
   token, code}` (`packages/proto/pair-qr.json`). `host` is the ranked LAN
   IP (192.168 > 172.16 > 10/8). Identity (`identity.go`: ed25519 +
   `crypto/rand`) and cert (ECDSA P-256, 5yr) persist in `pair.json`.
2. Phone scans (`features/pairing/scan_qr_page.dart`, `pair_qr.dart`):
   `parsePairQrJson` validates (`v??1`, `v!=1 → UnsupportedVersion`,
   port range, 64-hex fingerprint, 6-digit code). Camera + paste-JSON
   fallback both funnel into `_accept → onScanned`.
3. User confirms (`confirm_fingerprint_page.dart`): typed code must equal
   the QR `code`; empty code on old Macs shows an "Update required" banner.
   The 6-digit `PairingCode` (`SHA256("fuseitall-code|1|"+token) % 1e6`) is
   anti-mistake UX and consent, not the security boundary — the token is.
4. Both sides store (`pairing_store.dart` → `flutter_secure_storage`
   `fuseitall_pairing`, fail-closed null; Mac `persist.go`).
5. First ping/WebSocket authenticates with `Authorization: Bearer <token>`
   and pins certs (below). Rejected pings never teach return-path state.

## Contract

- QR format `v` is independent of wire `protocol_v`. Missing `v` means 1;
  `v>1` fails closed (`ErrUnsupportedProtocol`); unknown fields ignored;
  writers emit v1 only (`version.go:ParsePairQR/EncodePairQR`).
- Token ops (`pairing.go`): `RotatePairToken` (16B `crypto/rand` → 32 hex),
  `VerifyToken` (`ConstantTimeCompare`).
- Goodbye (`packages/proto/unpair.json`, capability `ping`):
  `unpair{nonce} → pong{nonce}` on `POST /unpair` or WS `TypeUnpair`.

## Key files

- Proto: `pair-qr.json`, `unpair.json`.
- Core: `pairing.go`, `identity.go`, `version.go` (QR parse/encode),
  `server.go` (token gate), `client.go` (`NewTOFUClient`).
- Mac: `main.go` (mint/serve), `backend/service.go` (`phoneClient`,
  `pinPeerFingerprint`), `backend/persist.go`, `backend/pairing_rotation.go`.
- Android: `features/pairing/*`, `features/ping/proto_client.dart`
  (`createTofuClient`), `net/phone_websocket.dart`, `net/go_server.dart`,
  `net/phone_identity_store.dart`.

## Rules & limits

- Phone→Mac is strict TOFU: `badCertificateCallback` accepts only the QR
  `fingerprint` (`sha256(cert.der)`), else fail closed.
- Mac→phone is blind-TOFU with narrow blast radius: the return path is
  learned only from an accepted (200, fully gated) phone ping; the first
  outbound ping pins the presented cert (`phone cert pinned
  fingerprint=<hex>`), later pings use `NewTOFUClient(pin)` and a changed
  cert fails closed. Auth still rests on the token the phone proved over
  the QR-pinned channel. Residual risk (LAN MITM exactly at first outbound
  ping) is accepted and logged; fix would be phone-advertised cert fp in
  the ping payload or mutual TLS (both deferred, both need schema/core
  changes).
- Phone identity survives unpair (`phone_identity_store.dart`); `reply_fp`
  over the token-authed channel re-pins the Mac side.

## Failure modes

- Wrong/newer QR version → loud scan-time error, never a wrong connection.
- Fingerprint mismatch → `certificate fingerprint mismatch`, peer kept
  (no flap), waits for authed re-pin.
- Phone-initiated unpair: both sides wipe ephemeral + remembered peer +
  TOFU pin + facts + cached caps, no token rotation.
- Mac-initiated forget (`ForgetLastDevice`): same wipe + `RotatePairFileToken`
  + new QR; old phone gets 403 → "Mac unpaired this device — scan its new QR".
