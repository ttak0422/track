#!/bin/sh
# Virtual GPUI windows and generated disposable v2 fixtures; no native app launch.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/../../.." && pwd)
cd "$root"
fixture=$(mktemp -d "${TMPDIR:-/tmp}/track-gpui-keyboard.XXXXXX")
trap 'rm -rf -- "$fixture"' EXIT HUP INT TERM
fixture=$(CDPATH= cd -- "$fixture" && pwd -P)
if [ "$(uname -s)" = Darwin ]; then
  export SDKROOT="$(env -u SDKROOT /usr/bin/xcrun --sdk macosx --show-sdk-path)"
  export CC=/usr/bin/clang CXX=/usr/bin/clang++
  export CARGO_TARGET_AARCH64_APPLE_DARWIN_LINKER=/usr/bin/clang
  export CARGO_TARGET_X86_64_APPLE_DARWIN_LINKER=/usr/bin/clang
fi
go test -c -o "$fixture/track-bound-bridge.test" ./internal/track/webui
TRACK_NATIVE_TEST_SYNTHETIC_ONLY=1 \
TRACK_NATIVE_BOUND_TEST_BINARY="$fixture/track-bound-bridge.test" \
cargo test --locked --manifest-path experiments/gpui/Cargo.toml --features ui-tests \
  shell::keyboard_ui:: -- --ignored --test-threads=1
