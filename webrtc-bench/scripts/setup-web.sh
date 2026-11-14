#!/usr/bin/env bash
set -euo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

cd "$BENCH_DIR"
npm install --no-audit --no-fund
npx playwright install chromium
npm run build
