#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

DISCOVERY="${1:-$DISCOVERY_FILE}"
[[ -f "$DISCOVERY" ]] || { echo "Discovery file not found: $DISCOVERY" >&2; exit 1; }

"$BENCH_DIR/scripts/setup-gateway.sh" >/dev/null
if [[ "${TARGET_API:-}" == 37* && -z "${RTC_API:-}" ]]; then
  export RTC_API=v2
fi
export PYTHONUNBUFFERED=1
exec "$BENCH_DIR/gateway/.venv/bin/python" "$BENCH_DIR/gateway/gateway_server.py" \
  --port "$GATEWAY_PORT" \
  --discovery_file "$DISCOVERY"
