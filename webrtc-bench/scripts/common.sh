#!/usr/bin/env bash
set -euo pipefail

BENCH_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ROOT="${AVDDESKTOP_HOME:-$HOME/Library/Application Support/AVDDesktop}"
SDK="$ROOT/sdk"
AVD_HOME="$ROOT/avd"
AVD_NAME="${AVD_NAME:-MyDevice}"
EMU_PORT="${EMU_PORT:-5556}"
SERIAL="emulator-$EMU_PORT"
GRPC_PORT="${GRPC_PORT:-8554}"
GATEWAY_PORT="${GATEWAY_PORT:-8080}"
WEB_PORT="${WEB_PORT:-5173}"
LOG_DIR="$ROOT/logs"
RESULT_DIR="$BENCH_DIR/results"
DISCOVERY_FILE="$RESULT_DIR/current-discovery.ini"
RUNNING_DIR="$HOME/Library/Caches/TemporaryItems/avd/running"
EMULATOR="${EMULATOR_BIN:-$BENCH_DIR/.tools/emulator-preview-37.1.8/emulator/emulator}"
ADB="$SDK/platform-tools/adb"

mkdir -p "$LOG_DIR" "$RESULT_DIR"

require_runtime() {
  [[ -x "$EMULATOR" ]] || { echo "Emulator not found: $EMULATOR" >&2; exit 1; }
  [[ -x "$ADB" ]] || { echo "adb not found: $ADB" >&2; exit 1; }
  [[ -f "$AVD_HOME/$AVD_NAME.ini" ]] || { echo "AVD not found: $AVD_HOME/$AVD_NAME.ini" >&2; exit 1; }
  [[ "$(uname -m)" == "arm64" ]] || echo "warning: AVD is arm64 but host is $(uname -m)" >&2
}
