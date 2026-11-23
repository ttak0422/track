# Experimental ordinary read-only AppKit acceptance

This is a manual procedure, not a report of a passing run. It exercises the
experimental GPUI app with normal read-only admission, using **only generated
data**. It does not launch the shipping Track.app and is not an editing test.

Use a disposable macOS VM with its own interactive desktop, or a dedicated
test Mac with an isolated test account and no real vaults or sync clients.
Do not run it on an active user's desktop. A temporary HOME does not isolate
WindowServer, focus, clipboard, input sources, or Dock state. Do not install
the binary or alter OS accessibility permissions, input settings, or profiles.

## Build and prepare

From a disposable checkout at the exact candidate revision, follow
[the build prerequisites](../README.md#run-macos). Use Go 1.26+ and the
candidate's locked Rust dependencies. Preparation uses the VM's Nix store;
application build outputs and build caches below are task-owned:

```sh
ROOT=$(mktemp -d /tmp/track-gpui-appkit.XXXXXXXX)
ROOT=$(cd "$ROOT" && pwd -P)
REPO=$(pwd -P)
git rev-parse HEAD HEAD^{tree}
./experiments/gpui/prepare.sh
export CARGO_HOME="$ROOT/cargo-home"
export CARGO_TARGET_DIR="$ROOT/cargo-target"
export CARGO_BUILD_BUILD_DIR="$ROOT/cargo-build"
GOENV=off GOMODCACHE="$ROOT/go-mod" GOCACHE="$ROOT/go-build" \
  go build -o "$ROOT/track" ./cmd/track
cargo build --locked --release --manifest-path experiments/gpui/Cargo.toml
GPUI="$CARGO_TARGET_DIR/release/track-gpui-poc"
"$GPUI" --version
shasum -a 256 "$GPUI" "$ROOT/track"
```

Generate a new two-vault fixture. The helper only creates files. It does not
launch Track or set process-wide configuration:

```sh
PYTHONPATH="$REPO/tests/canonical_read_only" python3 -B - "$ROOT" <<'PY'
from pathlib import Path
import sys
from process import Fixture

root = Path(sys.argv[1])
base = root / "fixture"
base.mkdir(mode=0o700)
f = Fixture(base)
for name, title in [("active", "ActiveOnly"), ("peer", "PeerOnly")]:
    (base / name / ".track/notes/100.yaml").write_text(
        f"version: 1\ntitle: {title}\ntags: []\n"
    )
(base / "active/note/101.md").write_text(
    "# Rendered heading\n\n**Rendered prose**\n\n[[ActiveOnly]]\n"
)
(base / "active/.track/notes/101.yaml").write_text(
    "version: 1\ntitle: RenderProbe\ntags: []\n"
)
PY
```

Use a free adjacent loopback port pair, such as 19932/19933 **only after verifying
both are free**. If either port is in use, choose another pair; never stop a
process by port/name. Use the fixture's manifest-disjoint 0700 private parent.
Take a baseline outside the protected roots with the helper below:

```sh
set -e
inventory() {
  PYTHONPATH="$REPO/tests/canonical_read_only" python3 -B - "$ROOT" <<'PY'
import json
from pathlib import Path
import sys
from process import inventory

b = Path(sys.argv[1]) / "fixture"
names = ["active", "peer", "machine.yml", "ordinary"]
print(json.dumps(sorted(inventory([b / name for name in names]).items())))
PY
}
inventory > "$ROOT/before.json"
BASE="$ROOT/fixture"
PORT=19932
# Preflight only: listeners are released here; the owned launch must still bind.
python3 -B - "$PORT" <<'PY'
import socket
import sys

port = int(sys.argv[1])
assert 1024 <= port < 65535
with socket.socket() as workspace, socket.socket() as static:
    workspace.bind(("127.0.0.1", port))
    static.bind(("127.0.0.1", port + 1))
PY
(
  cd "$BASE"
  exec env -i PATH=/usr/bin:/bin HOME="$BASE/home" PWD="$BASE" TMPDIR="$BASE/home" \
    XDG_CONFIG_HOME="$BASE/home" XDG_CACHE_HOME="$BASE/home" \
    "$GPUI" --read-only --track-bin "$ROOT/track" --vault main \
    --config "$BASE/machine.yml" --cache "$BASE/private" --server-port "$PORT"
) > "$ROOT/gpui.log" 2>&1 &
APP_PID=$!
```

## Checks

1. **Review and cancel.** The review lists exactly the generated active/peer
   paths. Confirm the private parent is still empty and neither listener is
   open during review. Cancel is focused first and Enter does not authorize
   reading. Cancel or Escape; no listener/private child appears. Close the
   exact fixture process, then repeat with explicit **Allow read-only access**.
   Record actual app/engine identities, and verify the engine belongs to this
   app before considering any process cleanup.
2. **Read and navigate.** Search ActiveOnly and PeerOnly (same numeric ID,
   different vaults). Verify each selects the right namespace and body.
   Open RenderProbe; compare Source and **Use rendered reading for this session**, including its heading/emphasis. Open **Full graph** in the active
   vault; verify the link to ActiveOnly and the correct note on open. Return to
   the reader and use **View > Reload**; wait for the expected body. Editing,
   creation, ordinary session persistence, Follow and automatic refresh remain
   unavailable. Separate pixel/input observations from engine response logs.
3. **Failure and ownership.** Repeat using a separate fresh fixture. Add an
   invalid Markdown-sidecar pair and a conflict-suffixed copy *before* the
   baseline. Confirm good notes remain readable and incomplete results are
   labeled, not represented as complete; do not require an invalid row to be
   navigable. A missing registered root is a separate preflight refusal case.
   For a deterministic late completion use an owned, test-only response
   barrier, not sleep or slow I/O on a real vault. Hold one body request, switch
   to Home or a healthy note, then release it; the old response must not
   replace the new destination. If no barrier is available, record this as
   **not run**. Do not infer live cloud hydration or waiting-banner coverage
   from the engine process tests.
4. **Quit and relaunch.** Use the native app's Quit. Verify exact app and owned
   engine exit, both listeners closed and no owned PID leases remain. Do not
   expect populated private cache children to be removed. Relaunch and verify
   a fresh review is required, with no previous tab/body auto-restore or
   writable fallback. After final exit, compare protected paths:

```sh
wait "$APP_PID"
inventory > "$ROOT/after.json"
cmp "$ROOT/before.json" "$ROOT/after.json"
```

Repeat the inventory check for the independent failure/cancel fixtures. It
compares entries, modes, sizes, modification times and bytes; reads may alter
atime. No canonical file is changed during a run. Keep test-owned logs and
private-cache artifacts until inspection is complete. Do not use this procedure
on a production or OneDrive vault, and do not interpret a pass as performance,
IME, accessibility, OS sandboxing, or distribution acceptance.

For separate layer-specific evidence see [the Go process matrices](../../../tests/canonical_read_only/README.md)
and [virtual Workspace tests](KEYBOARD-UI.md). Neither drives actual AppKit.
