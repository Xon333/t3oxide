#!/bin/bash
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
node --test tests/*.test.mjs
for file in adapter/*.mjs scripts/*.mjs; do node --check "$file"; done
bash -n scripts/build-mac.sh
if command -v swiftc >/dev/null; then
  swiftc -swift-version 5 -typecheck mac/Model.swift
  swiftc -frontend -parse mac/App.swift
  BIN=$(mktemp -t t3oxide-model.XXXXXX)
  trap 'rm -f "$BIN"' EXIT
  swiftc -swift-version 5 mac/Model.swift tests/ModelTests.swift -o "$BIN"
  "$BIN"
  if [[ $(uname -s) == Darwin ]]; then
    swiftc -swift-version 5 mac/Model.swift mac/Transport.swift tests/TransportTests.swift -o "$BIN"
    "$BIN"
  fi
fi
python3 -m py_compile scripts/measure.py
