#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
export DEVELOPER_DIR="${NATIVE_DEVELOPER_DIR:-/Library/Developer/CommandLineTools}"
export SDKROOT="${NATIVE_SDKROOT:-$(/usr/bin/xcrun --sdk macosx --show-sdk-path)}"
export CC="$DEVELOPER_DIR/usr/bin/clang"
export CLANG_MODULE_CACHE_PATH="$PWD/native/.build/preview-module-cache"
"$DEVELOPER_DIR/usr/bin/swift" run --package-path native --disable-sandbox VerifyPreviews "$@"
