#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
# Pinned scanner; does not modify the application's dependency selections.
"${T3OXIDE_GO:-go}" run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
