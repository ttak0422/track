#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
check_dir=$(mktemp -d "${TMPDIR:-/tmp}/track-anchor-check.XXXXXX")
trap 'rm -rf "$check_dir"' EXIT
sh scripts/apple-toolchain.sh native swiftc -parse-as-library -module-cache-path "$check_dir/cache" \
  native/Sources/TrackUI/MarkdownAnchors.swift native/Tools/VerifyAnchors/main.swift \
  -o "$check_dir/check"
"$check_dir/check"
