#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT=$(dirname "$SCRIPT_DIR")
cd "$ROOT"

MODULE_CACHE="$ROOT/build/desktop-clang-module-cache"

CLANG_MODULE_CACHE_PATH="$MODULE_CACHE" sh "$SCRIPT_DIR/apple-toolchain.sh" desktop swift \
	run --package-path "$ROOT/desktop" -c debug TrackWebTests
go test ./...

if command -v shellcheck >/dev/null 2>&1; then
	shellcheck "$SCRIPT_DIR/apple-toolchain.sh" "$SCRIPT_DIR/test-apple-toolchain.sh" "$SCRIPT_DIR/desktop-build.sh" "$SCRIPT_DIR/desktop-verify.sh" "$SCRIPT_DIR/desktop-smoke.sh"
	printf '%s\n' "shellcheck passed for desktop scripts"
else
	printf '%s\n' "shellcheck not installed; shell script static analysis was not run"
fi
