#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT=$(dirname "$SCRIPT_DIR")
cd "$ROOT"

DEVELOPER_DIR=${DESKTOP_DEVELOPER_DIR:-/Library/Developer/CommandLineTools}
SDKROOT=${DESKTOP_SDKROOT:-/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk}
MODULE_CACHE="$ROOT/build/desktop-clang-module-cache"

env DEVELOPER_DIR="$DEVELOPER_DIR" SDKROOT="$SDKROOT" CLANG_MODULE_CACHE_PATH="$MODULE_CACHE" \
	swift run --package-path "$ROOT/desktop" -c debug TrackWebTests
go test ./...

if command -v shellcheck >/dev/null 2>&1; then
	shellcheck "$SCRIPT_DIR/desktop-build.sh" "$SCRIPT_DIR/desktop-verify.sh" "$SCRIPT_DIR/desktop-smoke.sh"
	printf '%s\n' "shellcheck passed for desktop scripts"
else
	printf '%s\n' "shellcheck not installed; shell script static analysis was not run"
fi
