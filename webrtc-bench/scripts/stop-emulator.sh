#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

require_runtime

latest_discovery() {
  local candidate
  for candidate in $(ls -t "$RUNNING_DIR"/pid_*.ini 2>/dev/null); do
    if grep -qx "port.serial=$EMU_PORT" "$candidate"; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done
}

DISCOVERY="$(latest_discovery || true)"
QEMU_PID=""
if [[ -n "$DISCOVERY" ]]; then
  QEMU_PID="$(basename "$DISCOVERY" .ini | sed 's/^pid_//')"
fi
LAUNCHER_PID=""
if [[ -f "$RESULT_DIR/current-emulator.pid" ]]; then
  LAUNCHER_PID="$(cat "$RESULT_DIR/current-emulator.pid" 2>/dev/null || true)"
fi

if "$ADB" -s "$SERIAL" get-state >/dev/null 2>&1; then
  "$ADB" -s "$SERIAL" emu kill >/dev/null 2>&1 || true
fi

for _ in $(seq 1 25); do
  alive=0
  if [[ -n "$QEMU_PID" ]] && kill -0 "$QEMU_PID" 2>/dev/null; then alive=1; fi
  if [[ -n "$LAUNCHER_PID" ]] && kill -0 "$LAUNCHER_PID" 2>/dev/null; then alive=1; fi
  if [[ "$alive" == 0 ]] && ! "$ADB" devices | awk '{print $1}' | grep -qx "$SERIAL"; then
    exit 0
  fi
  sleep 0.2
done

# Preview emulator ignores `adb emu kill` in some headless configurations.
for pid in "$QEMU_PID" "$LAUNCHER_PID"; do
  if [[ -n "$pid" ]]; then kill "$pid" 2>/dev/null || true; fi
done
for _ in $(seq 1 50); do
  alive=0
  if [[ -n "$QEMU_PID" ]] && kill -0 "$QEMU_PID" 2>/dev/null; then alive=1; fi
  if [[ -n "$LAUNCHER_PID" ]] && kill -0 "$LAUNCHER_PID" 2>/dev/null; then alive=1; fi
  if [[ "$alive" == 0 ]]; then
    exit 0
  fi
  sleep 0.2
done

for pid in "$QEMU_PID" "$LAUNCHER_PID"; do
  if [[ -n "$pid" ]]; then kill -9 "$pid" 2>/dev/null || true; fi
done
echo "warning: forced stop for $SERIAL" >&2
