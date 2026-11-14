#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

RESOLUTION="${1:-1080x1920}"
CODEC="${CODEC:-h264}"
VSYNC="${VSYNC:-60}"
WIDTH="${RESOLUTION%x*}"
HEIGHT="${RESOLUTION#*x}"
GATEWAY_PID=""
WEB_PID=""

cleanup() {
  local code=$?
  if [[ -n "$GATEWAY_PID" ]] && kill -0 "$GATEWAY_PID" 2>/dev/null; then
    kill "$GATEWAY_PID" 2>/dev/null || true
    wait "$GATEWAY_PID" 2>/dev/null || true
  fi
  if [[ -n "$WEB_PID" ]] && kill -0 "$WEB_PID" 2>/dev/null; then
    kill "$WEB_PID" 2>/dev/null || true
    wait "$WEB_PID" 2>/dev/null || true
  fi
  "$BENCH_DIR/scripts/stop-emulator.sh" >/dev/null 2>&1 || true
  exit "$code"
}
trap cleanup EXIT INT TERM

[[ -x "$EMULATOR" ]] || "$BENCH_DIR/scripts/install-preview-emulator.sh"
[[ -f "$BENCH_DIR/motion-app/build/webrtc-motion.apk" ]] || "$BENCH_DIR/scripts/build-motion-apk.sh" >/dev/null
"$BENCH_DIR/scripts/setup-gateway.sh" >/dev/null
"$BENCH_DIR/scripts/start-emulator.sh" "$WIDTH" "$HEIGHT"

"$BENCH_DIR/scripts/start-gateway.sh" >"$LOG_DIR/webrtc-bench-dev-gateway.log" 2>&1 &
GATEWAY_PID=$!
for _ in $(seq 1 100); do
  if curl -fsS "http://127.0.0.1:$GATEWAY_PORT/api/v1/emulator/status" >/dev/null 2>&1; then break; fi
  sleep 0.2
done

cd "$BENCH_DIR"
npm run dev -- --port "$WEB_PORT" >"$LOG_DIR/webrtc-bench-dev-web.log" 2>&1 &
WEB_PID=$!
for _ in $(seq 1 100); do
  if curl -fsS "http://127.0.0.1:$WEB_PORT" >/dev/null 2>&1; then break; fi
  sleep 0.2
done

URL="http://127.0.0.1:$WEB_PORT/?autoconnect=1&codec=$CODEC&hz=$VSYNC"
echo "WebRTC FPS Lab: $URL"
echo "Press Ctrl-C to stop emulator, gateway and web server."
wait "$WEB_PID"
