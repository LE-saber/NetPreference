#!/bin/bash
# Build the real released r6 source for upgrade-format testing, never relabel new code.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
BASE=38a69cbb529eb8fc1b820fa00d7cc439306112f4
OUT=${1:-"$ROOT/dist-legacy"}
WORK=$(mktemp -d /tmp/netpreference-legacy-XXXXXX)
trap 'rm -rf "$WORK"' EXIT
cd "$ROOT"
git cat-file -e "$BASE:go.mod"
git archive "$BASE" | tar -xf - -C "$WORK"
grep -q '^PKG_VERSION:=0.1.0$' "$WORK/openwrt/luci-app-netpreference/Makefile"
grep -q '^PKG_RELEASE:=6$' "$WORK/openwrt/luci-app-netpreference/Makefile"
python3 "$WORK/scripts/build_ipk.py" --out "$OUT"
test -s "$OUT/luci-app-netpreference_0.1.0-r6_x86_64.ipk"
