#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
# Build libfuseitall.so (c-shared bridge over fuseitall/core) for Android
# device (arm64-v8a) and emulator (x86_64) using the NDK clang toolchain,
# and stage the .so files into android/app/src/main/jniLibs/<abi>/.
set -eu

BRIDGE_DIR="$(cd "$(dirname "$0")" && pwd)"
mkdir -p "$BRIDGE_DIR/../android/app/src/main/jniLibs"
OUT_BASE="$(cd "$BRIDGE_DIR/../android/app/src/main/jniLibs" && pwd)"
# Env contract: ANDROID_NDK_HOME (full ndk/<ver> dir) wins; else
# ANDROID_SDK_ROOT / ANDROID_HOME / default sdk with newest ndk picked.
# HOST_TAG follows uname so Linux CI and arm64/x86_64 Macs all resolve
# (the macOS NDK prebuilt is darwin-x86_64 even on Apple Silicon).
API=21

fail() { echo "build_android.sh: error: $1" >&2; exit 1; }

command -v go >/dev/null 2>&1 || fail "go not found in PATH"

if [ -n "${ANDROID_NDK_HOME:-}" ]; then
  NDK="$ANDROID_NDK_HOME"
  NDK_VER="$(basename "$NDK")"
else
  SDK="${ANDROID_SDK_ROOT:-${ANDROID_HOME:-$HOME/Library/Android/sdk}}"
  NDK_BASE="$SDK/ndk"
  [ -d "$NDK_BASE" ] || fail "no NDK under $NDK_BASE (set ANDROID_NDK_HOME or install via SDK Manager)"
  NDK_VER="$(ls "$NDK_BASE" | sort -t . -k1,1n -k2,2n -k3,3n | tail -n 1)"
  [ -n "$NDK_VER" ] || fail "NDK directory $NDK_BASE is empty"
  NDK="$NDK_BASE/$NDK_VER"
fi

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  darwin) HOST_TAG="darwin-x86_64" ;;
  linux) HOST_TAG="linux-x86_64" ;;
  *) fail "unsupported host OS: $OS (need darwin or linux)" ;;
esac
echo "Using NDK $NDK_VER ($HOST_TAG)"

PREBUILT="$NDK/toolchains/llvm/prebuilt/$HOST_TAG"
[ -d "$PREBUILT" ] || fail "expected toolchain dir missing: $PREBUILT"

# go_bridge is a standalone module (root go.work must stay untouched), so
# every go invocation runs with GOWORK=off against go_bridge/go.mod.
export GOWORK=off

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM

build_one() {
  abi="$1"; goarch="$2"; clang="$3"
  cc="$PREBUILT/bin/$clang"
  [ -x "$cc" ] || fail "clang missing: $cc"
  echo "Building $abi ($goarch)..."
  (
    cd "$BRIDGE_DIR"
    GOOS=android GOARCH="$goarch" CGO_ENABLED=1 CC="$cc" \
      go build -buildmode=c-shared -trimpath -o "$TMP/libfuseitall.so" .
  )
  mkdir -p "$OUT_BASE/$abi"
  cp "$TMP/libfuseitall.so" "$OUT_BASE/$abi/libfuseitall.so"
  echo "Staged $OUT_BASE/$abi/libfuseitall.so"
}

if [ "${1:-}" = "arm64" ]; then
  build_one "arm64-v8a" "arm64" "aarch64-linux-android$API-clang"
  echo "Done. arm64-v8a staged under $OUT_BASE (quick debug, x86_64 skipped)."
else
  build_one "arm64-v8a" "arm64" "aarch64-linux-android$API-clang"
  build_one "x86_64" "amd64" "x86_64-linux-android$API-clang"
  echo "Done. Both ABIs staged under $OUT_BASE."
fi
