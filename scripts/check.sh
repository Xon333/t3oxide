#!/bin/bash
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
node --test --test-concurrency=1 --test-reporter=tap tests/*.test.mjs
for file in adapter/*.mjs scripts/*.mjs; do node --check "$file"; done
bash -n scripts/build-rust.sh scripts/check-rust.sh scripts/check-security.sh scripts/check.sh
python3 -m py_compile scripts/measure.py scripts/update-rust-notices.py tests/rust_terminal.py tests/slt_terminal.py
python3 -m unittest discover -s tests -p test_measure.py
