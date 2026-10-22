#!/usr/bin/env bash
# Restricted-environment verification only. Not a substitute for check-go.sh:
# this tests actual stdlib-only production packages and the real Node adapter,
# but cannot compile or validate the pinned Charm UI/CLI executable.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
GO_BIN=${T3OXIDE_GO:-go}
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
printf 'module github.com/Xon333/t3oxide\n\ngo 1.23.2\n' > "$TMP/core.mod"
export GOTOOLCHAIN=local GOWORK=off
printf 'CORE/ADAPTER ONLY: %s; pinned UI build is NOT checked here.\n' "$("$GO_BIN" version)"
GROUP=${T3OXIDE_CHECK_GROUP:-all}
case "$GROUP" in all|core|original|features) ;; *) echo 'T3OXIDE_CHECK_GROUP must be all, core, original or features' >&2; exit 2 ;; esac
FLAGS=(-modfile="$TMP/core.mod" -count=1 -timeout=2m)
case "${1:-}" in
  '') ;;
  --race) FLAGS+=(-race) ;;
  --vet)
    "$GO_BIN" vet -modfile="$TMP/core.mod" ./internal/config ./internal/wire ./internal/client
    "$GO_BIN" vet -modfile="$TMP/core.mod" internal/integration/fixture_test.go internal/integration/adapter_test.go internal/integration/client_test.go internal/integration/features_test.go
    exit 0 ;;
  *) echo 'Usage: bash scripts/check-portable-core.sh [--race|--vet]' >&2; exit 2 ;;
esac
if [[ "$GROUP" == all || "$GROUP" == core ]]; then
  "$GO_BIN" test "${FLAGS[@]}" ./internal/config ./internal/wire ./internal/client
fi
if [[ "$GROUP" == all || "$GROUP" == original ]]; then
  "$GO_BIN" test "${FLAGS[@]}" internal/integration/fixture_test.go internal/integration/adapter_test.go internal/integration/client_test.go
fi
if [[ "$GROUP" == all || "$GROUP" == features ]]; then
  "$GO_BIN" test "${FLAGS[@]}" -run '^TestFeatures' internal/integration/fixture_test.go internal/integration/adapter_test.go internal/integration/client_test.go internal/integration/features_test.go
fi
