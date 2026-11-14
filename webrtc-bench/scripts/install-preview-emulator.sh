#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

BUILD_ID="${PREVIEW_EMULATOR_BUILD:-16833367}"
ARCHIVE="$BENCH_DIR/.tools/emulator_darwin_aarch64-${BUILD_ID}.zip"
EXPECTED_SHA1="${PREVIEW_EMULATOR_SHA1:-22fd0b85b2ba812aeb50dc4a78488480cf01801f}"
INSTALL_DIR="$BENCH_DIR/.tools/emulator-preview-37.1.8"
URL="https://dl.google.com/android/repository/emulator_darwin_aarch64-${BUILD_ID}.zip"

mkdir -p "$BENCH_DIR/.tools"
if [[ ! -x "$INSTALL_DIR/emulator/emulator" ]]; then
  if [[ ! -f "$ARCHIVE" ]]; then
    echo "Downloading Android Emulator Preview $BUILD_ID..."
    curl -fL --retry 3 --connect-timeout 15 -o "$ARCHIVE" "$URL"
  fi
  printf '%s  %s\n' "$EXPECTED_SHA1" "$ARCHIVE" | shasum -a 1 -c -
  mkdir -p "$INSTALL_DIR"
  unzip -q "$ARCHIVE" -d "$INSTALL_DIR"
fi

"$INSTALL_DIR/emulator/emulator" -version 2>&1 | grep -E 'Version:|Welcome to goldfish' | head -2 || true
echo "Preview emulator ready: $INSTALL_DIR/emulator/emulator"
