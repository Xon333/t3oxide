#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
GO_BIN=${T3OXIDE_GO:-go}
OUTPUT=${T3OXIDE_OUTPUT:-dist/t3oxide}
mkdir -p "$(dirname "$OUTPUT")"
CGO_ENABLED=0 "$GO_BIN" build -trimpath -buildvcs=false -ldflags '-s -w' -o "$OUTPUT" ./cmd/t3oxide
"$GO_BIN" version
printf 'Built %s\n' "$OUTPUT"
