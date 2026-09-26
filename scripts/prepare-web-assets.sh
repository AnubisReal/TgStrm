#!/bin/sh
# Build output is copied immediately before compiling the Go executable. The
# source tree only keeps dist/.keep, so stale browser assets cannot leak into a
# release.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
assets="$root/internal/webui/dist"

if [ ! -f "$root/web/dist/index.html" ]; then
  echo "web/dist is missing; run npm ci && npm run build from web first" >&2
  exit 1
fi

find "$assets" -mindepth 1 ! -name .keep -exec rm -rf {} +
cp -R "$root/web/dist/." "$assets/"
