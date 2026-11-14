#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

IMAGE="system-images;android-37.0;google_apis;arm64-v8a"
IMAGE_DIR="$SDK/system-images/android-37.0/google_apis/arm64-v8a"
SDKMANAGER="$SDK/cmdline-tools/latest/bin/sdkmanager"

if [[ ! -f "$IMAGE_DIR/system.img" ]]; then
  echo "Installing $IMAGE ..."
  export JAVA_HOME="$ROOT/jdk/Contents/Home"
  export PATH="$JAVA_HOME/bin:$PATH"
  yes | "$SDKMANAGER" --sdk_root="$SDK" "$IMAGE"
fi

"$BENCH_DIR/scripts/install-preview-emulator.sh"
"$BENCH_DIR/scripts/setup-gateway.sh" >/dev/null
"$BENCH_DIR/scripts/setup-web.sh"
if [[ ! -f "$BENCH_DIR/motion-app/build/webrtc-motion.apk" ]]; then
  "$BENCH_DIR/scripts/build-motion-apk.sh" >/dev/null
fi

echo "Host-GPU environment ready."
