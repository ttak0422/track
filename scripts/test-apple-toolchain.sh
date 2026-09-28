#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/track-apple-toolchain.XXXXXX")
trap 'rm -rf "$TMP_DIR"' EXIT HUP INT TERM

mkdir -p "$TMP_DIR/bin" "$TMP_DIR/selected/SDKs/MacOSX.sdk" \
	"$TMP_DIR/native-override" "$TMP_DIR/native-sdk" \
	"$TMP_DIR/desktop-override" "$TMP_DIR/desktop-sdk"

cat > "$TMP_DIR/xcode-select" <<'EOF'
#!/bin/sh
set -eu
if [ "${DEVELOPER_DIR+x}" = x ] || [ "${SDKROOT+x}" = x ] || [ "${TOOLCHAINS+x}" = x ]; then
	printf '%s\n' 'xcode-select received a contaminated toolchain environment' >&2
	exit 1
fi
printf '%s\n' "$APPLE_TOOLCHAIN_TEST_SELECTED_DIR"
EOF

cat > "$TMP_DIR/xcrun" <<'EOF'
#!/bin/sh
set -eu
if [ "${DEVELOPER_DIR-}" != "$APPLE_TOOLCHAIN_TEST_EXPECTED_DIR" ]; then
	printf 'unexpected DEVELOPER_DIR: %s\n' "${DEVELOPER_DIR-}" >&2
	exit 1
fi
if [ "${SDKROOT+x}" = x ] || [ "${TOOLCHAINS+x}" = x ]; then
	printf '%s\n' 'xcrun received a contaminated SDK/toolchain environment' >&2
	exit 1
fi
printf '%s\n' "$*" >> "$APPLE_TOOLCHAIN_TEST_XCRUN_LOG"
case "$*" in
	'--sdk macosx --show-sdk-path')
		if [ "${APPLE_TOOLCHAIN_TEST_FAIL_SDK:-}" = 1 ]; then
			printf '%s\n' 'stub: no macOS SDK' >&2
			exit 1
		fi
		printf '%s\n' "$APPLE_TOOLCHAIN_TEST_DISCOVERED_SDK"
		;;
	'--find swift') printf '%s\n' "$APPLE_TOOLCHAIN_TEST_BIN/swift" ;;
	'--find swiftc') printf '%s\n' "$APPLE_TOOLCHAIN_TEST_BIN/swiftc" ;;
	'--find clang') printf '%s\n' "$APPLE_TOOLCHAIN_TEST_BIN/clang" ;;
	'--find clang++') printf '%s\n' "$APPLE_TOOLCHAIN_TEST_BIN/clang++" ;;
	*) printf 'unexpected xcrun arguments: %s\n' "$*" >&2; exit 1 ;;
esac
EOF

for tool in swift swiftc clang clang++; do
	cat > "$TMP_DIR/bin/$tool" <<'EOF'
#!/bin/sh
set -eu
printf '%s|%s|%s|%s|%s\n' "${0##*/}" "$DEVELOPER_DIR" "$SDKROOT" "$CC" "$CXX" >> "$APPLE_TOOLCHAIN_TEST_RUN_LOG"
printf '%s\n' "$*" >> "$APPLE_TOOLCHAIN_TEST_RUN_LOG"
EOF
done
chmod +x "$TMP_DIR/xcode-select" "$TMP_DIR/xcrun" "$TMP_DIR/bin/"*

export APPLE_TOOLCHAIN_XCODE_SELECT="$TMP_DIR/xcode-select"
export APPLE_TOOLCHAIN_XCRUN="$TMP_DIR/xcrun"
export APPLE_TOOLCHAIN_TEST_BIN="$TMP_DIR/bin"
export APPLE_TOOLCHAIN_TEST_SELECTED_DIR="$TMP_DIR/selected"
export APPLE_TOOLCHAIN_TEST_DISCOVERED_SDK="$TMP_DIR/selected/SDKs/MacOSX.sdk"
export APPLE_TOOLCHAIN_TEST_XCRUN_LOG="$TMP_DIR/xcrun.log"
export APPLE_TOOLCHAIN_TEST_RUN_LOG="$TMP_DIR/run.log"
export DEVELOPER_DIR=/nix/store/test-apple-sdk
export SDKROOT=/nix/store/test-apple-sdk/SDKs/MacOSX.sdk
export TOOLCHAINS=nix-test-toolchain

# Default discovery must ignore the ambient Nix paths and query the selected developer directory.
export NATIVE_DEVELOPER_DIR=
export NATIVE_SDKROOT=
export APPLE_TOOLCHAIN_TEST_EXPECTED_DIR="$APPLE_TOOLCHAIN_TEST_SELECTED_DIR"
sh "$SCRIPT_DIR/apple-toolchain.sh" native swift --version
if ! /usr/bin/grep -F "swift|$APPLE_TOOLCHAIN_TEST_SELECTED_DIR|$APPLE_TOOLCHAIN_TEST_DISCOVERED_SDK|$TMP_DIR/bin/clang|$TMP_DIR/bin/clang++" "$APPLE_TOOLCHAIN_TEST_RUN_LOG" >/dev/null; then
	printf '%s\n' 'default discovery did not invoke Swift with the selected Apple toolchain' >&2
	exit 1
fi
if ! /usr/bin/grep -Fx -- '--version' "$APPLE_TOOLCHAIN_TEST_RUN_LOG" >/dev/null; then
	printf '%s\n' 'default discovery did not forward compiler arguments' >&2
	exit 1
fi

# Both named override families are independent and bypass only SDK discovery when explicitly set.
: > "$APPLE_TOOLCHAIN_TEST_XCRUN_LOG"
export NATIVE_DEVELOPER_DIR="$TMP_DIR/native-override"
export NATIVE_SDKROOT="$TMP_DIR/native-sdk"
export APPLE_TOOLCHAIN_TEST_EXPECTED_DIR="$NATIVE_DEVELOPER_DIR"
sh "$SCRIPT_DIR/apple-toolchain.sh" native swiftc -version
if ! /usr/bin/grep -F "swiftc|$NATIVE_DEVELOPER_DIR|$NATIVE_SDKROOT|$TMP_DIR/bin/clang|$TMP_DIR/bin/clang++" "$APPLE_TOOLCHAIN_TEST_RUN_LOG" >/dev/null; then
	printf '%s\n' 'native overrides were not honored' >&2
	exit 1
fi
if /usr/bin/grep -Fx -- '--sdk macosx --show-sdk-path' "$APPLE_TOOLCHAIN_TEST_XCRUN_LOG" >/dev/null; then
	printf '%s\n' 'explicit NATIVE_SDKROOT was not used' >&2
	exit 1
fi

export DESKTOP_DEVELOPER_DIR="$TMP_DIR/desktop-override"
export DESKTOP_SDKROOT="$TMP_DIR/desktop-sdk"
export APPLE_TOOLCHAIN_TEST_EXPECTED_DIR="$DESKTOP_DEVELOPER_DIR"
sh "$SCRIPT_DIR/apple-toolchain.sh" desktop swift --version
if ! /usr/bin/grep -F "swift|$DESKTOP_DEVELOPER_DIR|$DESKTOP_SDKROOT|$TMP_DIR/bin/clang|$TMP_DIR/bin/clang++" "$APPLE_TOOLCHAIN_TEST_RUN_LOG" >/dev/null; then
	printf '%s\n' 'desktop overrides were not honored' >&2
	exit 1
fi

# A missing SDK must fail with the relevant override name, rather than silently using ambient SDKROOT.
export NATIVE_DEVELOPER_DIR=
export NATIVE_SDKROOT=
export APPLE_TOOLCHAIN_TEST_EXPECTED_DIR="$APPLE_TOOLCHAIN_TEST_SELECTED_DIR"
export APPLE_TOOLCHAIN_TEST_FAIL_SDK=1
if sh "$SCRIPT_DIR/apple-toolchain.sh" native swift --version 2>"$TMP_DIR/error.log"; then
	printf '%s\n' 'missing SDK was unexpectedly accepted' >&2
	exit 1
fi
if ! /usr/bin/grep -F 'set NATIVE_SDKROOT explicitly' "$TMP_DIR/error.log" >/dev/null; then
	printf '%s\n' 'missing SDK diagnostic did not explain the override' >&2
	exit 1
fi

printf '%s\n' 'Apple toolchain discovery/override tests passed'
