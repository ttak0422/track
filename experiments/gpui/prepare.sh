#!/bin/sh
# Materialize only the pinned GPUI source patch, not the native application.
set -eu
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
destination="$here/.build-deps/gpui"
if [ -e "$destination" ] && [ ! -L "$destination" ]; then
  printf '%s\n' "Refusing to replace a non-symlink: $destination" >&2
  exit 1
fi
mkdir -p "$here/.build-deps"
nix-build "$here/patched-source.nix" --out-link "$destination"
