#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

export TARGET_API="${TARGET_API:-37.0}"
export SHOW_WINDOW=1
export VSYNC="${VSYNC:-120}"
export CODEC="${CODEC:-h264}"
exec "$BENCH_DIR/scripts/dev.sh" "${1:-1080x1920}"
