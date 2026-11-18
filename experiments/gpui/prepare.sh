#!/bin/sh
# Materialize the two explicit, hash-pinned source patches, not the application.
set -eu
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
# Preflight every destination before changing either link, including dangling links.
for package in gpui xattr; do
  destination="$here/.build-deps/$package"
  if [ -e "$destination" ] && [ ! -L "$destination" ]; then
    printf '%s\n' "Refusing to replace a non-symlink: $destination" >&2
    exit 1
  fi
done
mkdir -p "$here/.build-deps"
for package in gpui xattr; do
  nix-build "$here/patched-source.nix" --argstr package "$package" --out-link "$here/.build-deps/$package"
done
