#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

GATEWAY_DIR="$BENCH_DIR/gateway"
PYTHON_BIN="${PYTHON_BIN:-$(command -v python3.13 || command -v python3)}"
VENV="$GATEWAY_DIR/.venv"

if [[ ! -x "$VENV/bin/python" ]]; then
  "$PYTHON_BIN" -m venv "$VENV"
fi

"$VENV/bin/python" -m pip install --disable-pip-version-check -q -r "$GATEWAY_DIR/requirements.txt"
"$VENV/bin/python" -m grpc_tools.protoc \
  -I"$GATEWAY_DIR/proto" \
  -I"$("$VENV/bin/python" -c 'import grpc_tools, pathlib; print(pathlib.Path(grpc_tools.__file__).parent / "_proto")')" \
  --python_out="$GATEWAY_DIR/proto" \
  --grpc_python_out="$GATEWAY_DIR/proto" \
  "$GATEWAY_DIR/proto/emulator_controller.proto" \
  "$GATEWAY_DIR/proto/rtc_service_v2.proto" \
  "$GATEWAY_DIR/proto/rtc_service_v2_new.proto" \
  "$GATEWAY_DIR/proto/ice_config.proto"

echo "Gateway environment ready: $VENV"
