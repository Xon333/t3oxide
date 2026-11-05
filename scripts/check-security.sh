#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
# Optional developer audit; no application dependency or lockfile mutation.
if ! cargo audit --version >/dev/null 2>&1; then
  echo 'Install cargo-audit with: cargo install cargo-audit --locked' >&2
  exit 2
fi
cargo audit --file Cargo.lock
