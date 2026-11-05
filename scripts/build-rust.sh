#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
OUTPUT=${T3OXIDE_OUTPUT:-dist/t3oxide}
TARGET_DIR=${CARGO_TARGET_DIR:-target}
cargo build --release --locked --target-dir "$TARGET_DIR" --bin t3oxide
mkdir -p "$(dirname "$OUTPUT")"
cp "$TARGET_DIR/release/t3oxide" "$OUTPUT"
chmod 755 "$OUTPUT"
mkdir -p dist
cp NOTICE dist/NOTICE
cp -R LICENSES dist/
"$OUTPUT" --version
printf 'Built %s (no installation performed)\n' "$OUTPUT"
