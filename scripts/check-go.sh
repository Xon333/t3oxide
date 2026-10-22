#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
GO_BIN=${T3OXIDE_GO:-go}
GOFMT_BIN="$(dirname "$(command -v "$GO_BIN")")/gofmt"
UNFORMATTED=$("$GOFMT_BIN" -l cmd/t3oxide internal)
if [[ -n "$UNFORMATTED" ]]; then
  printf 'Run gofmt on:\n%s\n' "$UNFORMATTED" >&2
  exit 1
fi
"$GO_BIN" test -count=1 ./...
"$GO_BIN" vet ./...
"$GO_BIN" test -race -count=1 ./...
