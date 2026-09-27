#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT=$(dirname "$SCRIPT_DIR")
cd "$ROOT"

APP_BUNDLE=${DESKTOP_APP:-"build/desktop/Track.app"}
case "$APP_BUNDLE" in
	/*) ;;
	*) APP_BUNDLE="$ROOT/$APP_BUNDLE" ;;
esac
APP_EXECUTABLE="$APP_BUNDLE/Contents/MacOS/Track"
if [ ! -x "$APP_EXECUTABLE" ]; then
	printf 'Track.app executable not found: %s\n' "$APP_EXECUTABLE" >&2
	exit 1
fi

python3 "$SCRIPT_DIR/desktop-smoke.py" "$APP_EXECUTABLE"
