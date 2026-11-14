#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

export TARGET_API="${TARGET_API:-37.0}"
export SHOW_WINDOW=1
export CODEC="${CODEC:-h264}"
export VSYNC="${VSYNC:-120}"
export RESULT_TAG="${RESULT_TAG:-api37-hostgpu}"
export RESOLUTIONS="${RESOLUTIONS:-720x1280 1080x1920 1440x2560 2160x3840}"
export DURATION="${DURATION:-20}"
export WARMUP="${WARMUP:-5}"

if [[ ! -f "$SDK/system-images/android-37.0/google_apis/arm64-v8a/system.img" ]]; then
  "$BENCH_DIR/scripts/setup-hostgpu.sh"
fi

exec "$BENCH_DIR/scripts/run-benchmark.sh"
