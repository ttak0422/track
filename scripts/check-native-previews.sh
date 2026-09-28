#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
export CLANG_MODULE_CACHE_PATH="$PWD/native/.build/preview-module-cache"
sh scripts/apple-toolchain.sh native swift run --package-path native --disable-sandbox VerifyPreviews "$@"
