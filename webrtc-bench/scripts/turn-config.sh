#!/usr/bin/env bash
set -euo pipefail
python3 - <<'PY'
import json
import os

url = os.environ.get("TURN_URL", "").strip()
if not url:
    raise SystemExit("TURN_URL is required")
print(json.dumps({
    "lifetimeDuration": "86400s",
    "iceServers": [{
        "urls": [url],
        "username": os.environ.get("TURN_USERNAME", ""),
        "credential": os.environ.get("TURN_CREDENTIAL", ""),
    }],
    "iceTransportPolicy": "all",
}, separators=(",", ":")))
PY
