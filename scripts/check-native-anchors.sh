#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
export DEVELOPER_DIR="${NATIVE_DEVELOPER_DIR:-/Library/Developer/CommandLineTools}"
export SDKROOT="${NATIVE_SDKROOT:-$(/usr/bin/xcrun --sdk macosx --show-sdk-path)}"
check_dir=$(mktemp -d "${TMPDIR:-/tmp}/track-anchor-check.XXXXXX")
trap 'rm -rf "$check_dir"' EXIT
swiftc -parse-as-library -module-cache-path "$check_dir/cache" \
  native/Sources/TrackUI/MarkdownAnchors.swift native/Tools/VerifyAnchors/main.swift \
  -o "$check_dir/check"
"$check_dir/check"
