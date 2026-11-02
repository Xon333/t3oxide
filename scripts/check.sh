#!/bin/bash
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
node --test tests/*.test.mjs
for file in adapter/*.mjs scripts/*.mjs; do node --check "$file"; done
bash -n scripts/build-go.sh scripts/check-go.sh scripts/check-portable-core.sh
python3 -m py_compile scripts/measure.py
python3 -m unittest discover -s tests -p test_measure.py
