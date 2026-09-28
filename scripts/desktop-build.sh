#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT=$(dirname "$SCRIPT_DIR")
cd "$ROOT"

if [ "$(uname -s)" != "Darwin" ]; then
	printf '%s\n' "desktop-app requires macOS" >&2
	exit 1
fi

for tool in go npm node python3; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		printf 'required tool is not on PATH: %s\n' "$tool" >&2
		exit 1
	fi
done

if [ ! -x web/node_modules/.bin/vite ] || [ ! -x web/node_modules/.bin/tsc ]; then
	npm ci --prefix web
fi

APP_BUNDLE=${DESKTOP_APP:-"build/desktop/Track.app"}
case "$APP_BUNDLE" in
	/*) ;;
	*) APP_BUNDLE="$ROOT/$APP_BUNDLE" ;;
esac
case "$APP_BUNDLE" in
	"$ROOT"/build/*.app) ;;
	*) printf 'DESKTOP_APP must name an app bundle under %s/build\n' "$ROOT" >&2; exit 1 ;;
esac
case "$APP_BUNDLE" in
	*"/../"*|*"/.."|*"/./"*) printf 'DESKTOP_APP must not contain dot path segments: %s\n' "$APP_BUNDLE" >&2; exit 1 ;;
esac
FRONTEND_DIST="$ROOT/build/desktop-web-dist"
OVERLAY="$ROOT/build/desktop-embed-overlay.json"
MODULE_CACHE="$ROOT/build/desktop-clang-module-cache"

mkdir -p "$ROOT/build"
(
	cd "$ROOT/web"
	./node_modules/.bin/tsc -b
	# A shell used for site development may export static mode. Desktop always needs the live API UI.
	VITE_TRACK_STATIC=0 ./node_modules/.bin/vite build --base / --outDir "$FRONTEND_DIST" --emptyOutDir
)

python3 "$SCRIPT_DIR/desktop-embed-overlay.py" "$ROOT" "$FRONTEND_DIST" "$OVERLAY"

CLANG_MODULE_CACHE_PATH="$MODULE_CACHE" sh "$SCRIPT_DIR/apple-toolchain.sh" desktop swift \
	build --package-path "$ROOT/desktop" -c release --product TrackWeb

rm -rf "$APP_BUNDLE"
mkdir -p "$APP_BUNDLE/Contents/MacOS" "$APP_BUNDLE/Contents/Resources"
cp "$ROOT/desktop/.build/release/TrackWeb" "$APP_BUNDLE/Contents/MacOS/Track"
go build -trimpath -overlay="$OVERLAY" \
	-o "$APP_BUNDLE/Contents/Resources/track" ./cmd/track
cp "$ROOT/desktop/Resources/Info.plist" "$APP_BUNDLE/Contents/Info.plist"
cp "$ROOT/desktop/Resources/PkgInfo" "$APP_BUNDLE/Contents/PkgInfo"
chmod 755 "$APP_BUNDLE/Contents/MacOS/Track" "$APP_BUNDLE/Contents/Resources/track"

if command -v plutil >/dev/null 2>&1; then
	plutil -lint "$APP_BUNDLE/Contents/Info.plist"
fi

printf 'Built unsigned app: %s\n' "$APP_BUNDLE"
