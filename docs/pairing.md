# Pairing

How the phone and Mac trust each other once, then forget on demand.
Steady-state transport (discovery, WebSocket, gating) lives in
`connection.md`.

## What it does

One QR scan bootstraps mutual trust: the phone learns the Mac's TLS
identity out-of-band, both sides share a 256-bit token (from which
HKDF-SHA256 derives auth and storage encryption keys), and later
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
- Token ops (`pairing.go`): `RotatePairToken` (32B `crypto/rand` → 64 hex),
  `VerifyToken` (`ConstantTimeCompare`). Key derivation: `core.DeriveDBKey` (HKDF-SHA256).
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
- No packet reaches the Mac (wrong QR host, firewall, TLS pin) → both sides
  stay loud: Mac `GetPairStatus` keeps `LastAcceptUnix==0` so the PairCard
  shows "Waiting for your phone"; phone `_announcePresence` sets its
  reachability card ("Couldn't reach your Mac") instead of vague offline.
- No route to host (errno 113, `NoRouteFailure`) with a correct QR host on
  the same SSID → phone-local routing (mobile data preferred, VPN, AP
  isolation, asleep Mac), never a wrong token/version. The phone card names
  the tried `host:port` and says to turn off mobile data/VPN briefly, keep
  Wi-Fi on with the Mac awake, then Reconnect. Beacon sender IPs lead both
  the WebSocket and HTTP dial order so a beacon that knows the route heals
  first pair without a re-scan.
- Connection refused (errno 111, `RefusedFailure`) → the host is reachable
  but nothing listens on the port: stale QR, wrong device on a swept subnet,
  or the Mac app stopped. The card says to reopen the Mac app and scan its
  current QR — never network steps.
- Dial timeout (`TimeoutFailure`: WS 4s / HTTP 10s / feature 3s) → SYNs
  vanish instead of failing fast: AP client isolation between wireless
  clients, Mac asleep, or firewall drop. The card says same Wi-Fi radio for
  both, Mac awake, router client-isolation off, VPN off, then Reconnect.
  (Verified case: Mac `en0=192.168.1.71/24`, server `LISTEN *:18789`,
  firewall off, yet phone WS+HTTP+feature all time out while a sibling host
  refused — textbook isolation/sleep, not a wrong QR.)
- Default-network flap (113/timeout on first pair despite right SSID+QR) →
  the phone pins pairing sockets to Wi-Fi once (`fuseitall/wifibind`
  `bindWifi`, `MainActivity` + `net/wifi_bind.dart`), then redials; the
  re-announce renders the final card when the route is truly gone. The bind
  lasts for the session and releases on unpair/revoke/dispose, network
  change, or native Wi-Fi loss. Best-effort throughout: bind failure falls
  back to unbound dials plus the loud card above.
- Fingerprint mismatch → `certificate fingerprint mismatch`, peer kept
  (no flap), waits for authed re-pin.
- Phone-initiated unpair: both sides wipe ephemeral + remembered peer +
  TOFU pin + facts + cached caps, no token rotation.
- Mac-initiated forget (`ForgetLastDevice`): same wipe + `RotatePairFileToken`
  + new QR; old phone gets 403 → "Mac unpaired this device — scan its new QR".
  Stale-token 403 and version 426 also stamp `GetPairStatus.LastRejectKind`
  (`auth`/`update`) so the next PairCard names the cause in plain language.
  Forget/unpair clear the stamps with the identity.
