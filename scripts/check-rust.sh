#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
cargo fmt --all --check
cargo fmt --manifest-path vendor/superlighttui/Cargo.toml --check
cargo clippy --locked --all-targets --all-features -- -D warnings
cargo clippy --locked -p superlighttui --lib -- -D warnings
cargo test --locked --all-targets
