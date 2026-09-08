# ADR 0008 — Zero-polling real-time networking: persistent WebSocket & dual-direction discovery

## Context

Prior milestones relied on periodic polling:
1. Phone executed a 20s HTTP POST `/ping` heartbeat to maintain presence and flush features.
2. Mac executed a 20s auto-reconnect tick (`HeartbeatTick`) to refresh presence.
3. Android Dart FFI polled the Go bridge in-process event queue every 200ms (`PhonePoll`, `PhonePollEvent`).
4. Outbound Mac→Phone feature pushes (clipboard, notification dismissals, settings) dialed the phone's ephemeral HTTP server via HTTP POST and queued for heartbeat ticks.
5. Mac only broadcasted UDP discovery beacons 3 times on startup. If the phone opened later, discovery was delayed until manual tap or heartbeat timeout.

This polling wasted CPU wakeups (200ms FFI loop, 20s HTTP timer), caused battery drain, and introduced perceptible latency (up to 20 seconds) when reconnecting or updating online/offline state.

## Decision

1. **Persistent Bi-Directional TLS WebSocket**:
   - The phone maintains a persistent encrypted WebSocket connection to the Mac (`wss://<mac_host>:18789/ws`) with TOFU cert pinning.
   - Mac sends all feature envelopes (`TypeClipPush`, `TypeNotifDismiss`, `TypeSettingsSync`, etc.) directly through the active WebSocket (`WriteActiveWS`) with 0ms delivery.
   - Phone sends all feature envelopes (`TypeClipPush`, `TypeNotifPost`, `TypeNotifDismiss`, `TypeSettingsSync`, etc.) directly through the WebSocket (`sendEnvelope`).
   - The 200ms FFI polling timer in `go_server.dart` is eliminated.
   - The 20s recurring HTTP heartbeat in `ping_page.dart` and `HeartbeatTick` in `service.go` are eliminated.

2. **Instant Dual-Direction LAN Discovery**:
   - **Phone Opens / Resumes**: Phone performs parallel asynchronous dials to remembered host candidates (Happy Eyeballs) and broadcasts a UDP discovery probe on port 18790 (`DiscoveryProbe`).
   - **Mac UDP Listener**: Mac runs a continuous UDP listener on port 18790. On receiving a probe with matching fingerprint, Mac immediately responds with a unicast `DiscoveryRecord` beacon to the phone.
   - **Mac Opens / Network Changes**: Mac broadcasts UDP discovery beacons on port 18790. Phone's continuous `BeaconListener` receives it and connects immediately.
   - Both cold-start directions connect in < 30ms.

3. **Immediate Reactive Offline Detection**:
   - Sockets close on app exit, window closure, or network loss (TCP FIN / EOF).
   - Mac detects `conn.Read()` termination in `server_ws.go` and immediately fires `OnWSDisconnect`, emitting `state:changed` with `paired: false`.
   - Phone detects stream `onDone`/`onError` in `phone_websocket.dart` and immediately transitions to `disconnected`.
   - Both UIs switch to Offline with 0 delay. Disconnected devices do NOT poll.

## Consequences

- Zero CPU wakeups during idle on both Mac and Android.
- Zero network poll spam when either device is offline.
- Real-time instant delivery for clipboard, notifications, and settings.
