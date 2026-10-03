#!/bin/sh
set -eu

usage() {
	printf '%s\n' "usage: sh scripts/apple-toolchain.sh (native|desktop) (swift|swiftc|clang|clang++) [arguments...]" >&2
}

if [ "$#" -lt 2 ]; then
	usage
	exit 2
fi

kind=$1
command_name=$2
shift 2

case "$kind" in
	native)
		developer_override=${NATIVE_DEVELOPER_DIR:-}
		sdk_override=${NATIVE_SDKROOT:-}
		developer_var=NATIVE_DEVELOPER_DIR
		sdk_var=NATIVE_SDKROOT
		;;
	desktop)
		developer_override=${DESKTOP_DEVELOPER_DIR:-}
		sdk_override=${DESKTOP_SDKROOT:-}
		developer_var=DESKTOP_DEVELOPER_DIR
		sdk_var=DESKTOP_SDKROOT
		;;
	*)
		usage
		printf 'unknown toolchain group: %s\n' "$kind" >&2
		exit 2
		;;
esac

xcode_select=${APPLE_TOOLCHAIN_XCODE_SELECT:-/usr/bin/xcode-select}
xcrun=${APPLE_TOOLCHAIN_XCRUN:-/usr/bin/xcrun}

if [ ! -x "$xcrun" ]; then
	printf 'Apple xcrun is unavailable at %s. Install Xcode Command Line Tools with `xcode-select --install`.\n' "$xcrun" >&2
	exit 1
fi

if [ -n "$developer_override" ]; then
	developer_dir=$developer_override
else
	if [ ! -x "$xcode_select" ]; then
		printf 'Apple xcode-select is unavailable at %s. Install Xcode Command Line Tools or set %s explicitly.\n' "$xcode_select" "$developer_var" >&2
		exit 1
	fi
	if ! developer_dir=$(/usr/bin/env -u DEVELOPER_DIR -u SDKROOT -u TOOLCHAINS "$xcode_select" -p); then
		printf 'No selected Apple developer directory. Install/select Xcode or Command Line Tools (`xcode-select --install`, then `sudo xcode-select --switch ...`) or set %s.\n' "$developer_var" >&2
		exit 1
	fi
fi

case "$developer_dir" in
	/*) ;;
	*) printf '%s must be an absolute path: %s\n' "$developer_var" "$developer_dir" >&2; exit 1 ;;
esac
if [ ! -d "$developer_dir" ]; then
	printf '%s does not exist: %s. Install/select an Apple toolchain or set a valid %s.\n' "$developer_var" "$developer_dir" "$developer_var" >&2
	exit 1
fi

if [ -n "$sdk_override" ]; then
	sdkroot=$sdk_override
else
	if ! sdkroot=$(/usr/bin/env -u SDKROOT -u TOOLCHAINS DEVELOPER_DIR="$developer_dir" "$xcrun" --sdk macosx --show-sdk-path); then
		printf 'Could not discover a macOS SDK under %s. Install a matching Xcode/Command Line Tools SDK or set %s explicitly.\n' "$developer_dir" "$sdk_var" >&2
		exit 1
	fi
fi

case "$sdkroot" in
	/*) ;;
	*) printf '%s must be an absolute path: %s\n' "$sdk_var" "$sdkroot" >&2; exit 1 ;;
esac
if [ ! -d "$sdkroot" ]; then
	printf 'macOS SDK does not exist: %s. Install a macOS SDK or set a valid %s.\n' "$sdkroot" "$sdk_var" >&2
	exit 1
fi

find_apple_tool() {
	tool_name=$1
	if ! tool_path=$(/usr/bin/env -u SDKROOT -u TOOLCHAINS DEVELOPER_DIR="$developer_dir" "$xcrun" --find "$tool_name"); then
		printf 'Could not find Apple %s in %s. Install/select a complete Xcode or Command Line Tools toolchain.\n' "$tool_name" "$developer_dir" >&2
		exit 1
	fi
	if [ ! -x "$tool_path" ]; then
		printf 'Apple %s resolved to a missing or non-executable path: %s\n' "$tool_name" "$tool_path" >&2
		exit 1
	fi
	printf '%s\n' "$tool_path"
}

swift=$(find_apple_tool swift)
swiftc=$(find_apple_tool swiftc)
clang=$(find_apple_tool clang)
clangxx=$(find_apple_tool clang++)

case "$command_name" in
	swift) tool=$swift ;;
	swiftc) tool=$swiftc ;;
	clang) tool=$clang ;;
	clang++) tool=$clangxx ;;
	*) usage; printf 'unsupported Apple tool: %s\n' "$command_name" >&2; exit 2 ;;
esac

# Assign the complete selected toolchain environment on every invocation so a Nix shell's
# DEVELOPER_DIR, SDKROOT, TOOLCHAINS, CC, or CXX cannot split the compiler from its SDK.
exec /usr/bin/env -u TOOLCHAINS \
	DEVELOPER_DIR="$developer_dir" SDKROOT="$sdkroot" CC="$clang" CXX="$clangxx" \
	"$tool" "$@"
