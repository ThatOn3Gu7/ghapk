#!/bin/sh
# Runs ./ghapk against a pretend GitHub (acme/nazo) and a pretend phone, so you
# can look around the interface without touching anything real.
set -e
here=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
python3 "$here/scripts/fakegh/gen.py" "$tmp/fake" >/dev/null
export PATH="$here/scripts/fakegh:$here/scripts/fakegh/bin:$PATH"
export FAKEGH_DIR="$tmp/fake" FAKEGH_SPEED=2500000 FAKEPHONE_DIR="$tmp/phone" FAKE_INSTALL_DELAY=3
export TERMUX_VERSION=1 GHAPK_RISH=off GHAPK_CONFIG_DIR="$tmp/config" GHAPK_CACHE_DIR="$tmp/cache" GHAPK_HISTORY="$tmp/history.json"
mkdir -p "$tmp/phone"
"$here/ghapk" -r acme/nazo "$@"
