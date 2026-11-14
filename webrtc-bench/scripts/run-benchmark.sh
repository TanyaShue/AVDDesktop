#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

RESOLUTIONS="${RESOLUTIONS:-720x1280 1080x1920 1440x2560 2160x3840}"
DURATION="${DURATION:-20}"
WARMUP="${WARMUP:-5}"
CODEC="${CODEC:-vp8}"
VSYNC="${VSYNC:-60}"
RESULT_TAG="${RESULT_TAG:-}"
PT="${PT:-0}"
BITRATE_KBPS="${BITRATE_KBPS:-0}"
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

require_runtime
if [[ ! -x "$EMULATOR" ]]; then
  "$BENCH_DIR/scripts/install-preview-emulator.sh"
fi
if [[ ! -f "$BENCH_DIR/motion-app/build/webrtc-motion.apk" ]]; then
  "$BENCH_DIR/scripts/build-motion-apk.sh" >/dev/null
fi
"$BENCH_DIR/scripts/setup-web.sh"

cd "$BENCH_DIR"
npm run preview >"$LOG_DIR/webrtc-bench-web.log" 2>&1 &
WEB_PID=$!
for _ in $(seq 1 100); do
  if curl -fsS "http://127.0.0.1:$WEB_PORT" >/dev/null 2>&1; then break; fi
  sleep 0.2
done
curl -fsS "http://127.0.0.1:$WEB_PORT" >/dev/null

for resolution in $RESOLUTIONS; do
  width="${resolution%x*}"
  height="${resolution#*x}"
  echo "=== Benchmark ${width}x${height} ==="

  "$BENCH_DIR/scripts/start-emulator.sh" "$width" "$height"

  GATEWAY_LOG="$LOG_DIR/webrtc-bench-gateway-${resolution}-${CODEC}-${VSYNC}hz.log"
  "$BENCH_DIR/scripts/start-gateway.sh" >"$GATEWAY_LOG" 2>&1 &
  GATEWAY_PID=$!

  READY=0
  for _ in $(seq 1 100); do
    if curl -fsS "http://127.0.0.1:$GATEWAY_PORT/api/v1/emulator/status" >/dev/null 2>&1; then
      READY=1
      break
    fi
    if ! kill -0 "$GATEWAY_PID" 2>/dev/null; then
      echo "Gateway exited. Log:" >&2
      tail -120 "$GATEWAY_LOG" >&2
      exit 1
    fi
    sleep 0.2
  done
  if [[ "$READY" != 1 ]]; then
    echo "Gateway did not become ready. Log:" >&2
    tail -120 "$GATEWAY_LOG" >&2
    exit 1
  fi

  node "$BENCH_DIR/scripts/benchmark.mjs" \
    "--resolution=$resolution" \
    "--codec=$CODEC" \
    "--hz=$VSYNC" \
    "--tag=$RESULT_TAG" \
    "--pt=$PT" \
    "--bitrate=$BITRATE_KBPS" \
    "--duration=$DURATION" \
    "--warmup=$WARMUP" \
    "--url=http://127.0.0.1:$WEB_PORT" \
    "--gateway=http://127.0.0.1:$GATEWAY_PORT" \
    "--outputDir=$RESULT_DIR"

  kill "$GATEWAY_PID" 2>/dev/null || true
  wait "$GATEWAY_PID" 2>/dev/null || true
  GATEWAY_PID=""
  "$BENCH_DIR/scripts/stop-emulator.sh"
done

node "$BENCH_DIR/scripts/summarize-results.mjs"
