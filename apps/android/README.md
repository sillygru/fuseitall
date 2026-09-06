# FuseItAll (Android)

Free and open-source Android <-> Mac link (AGPL-3.0). BASE milestone:
QR pairing + manual ping to Mac.

```sh
flutter run -d <device>   # physical device (camera needed for QR scan)
flutter analyze && flutter test
```

Pairing token persists in the OS keystore; Unpair wipes it. Phone-side
ping server runs over Dart FFI (`go_bridge/`, rebuilt via
`go_bridge/build_android.sh`). Conventions live in the repo root
`AGENTS.md`.
