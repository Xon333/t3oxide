#!/bin/sh
# Host-local capability-records owns vault location and Python dependencies.
set -eu
repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
if ! command -v capability-records >/dev/null 2>&1; then
  echo "capability-records is unavailable; reconcile the affected records on the canonical knowledge host." >&2
  exit 2
fi
exec capability-records check --owner t3oxide --repo "$repo" "$@"
