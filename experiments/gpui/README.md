# GPUI rendering experiment

An isolated, read-only-by-default native workspace using [Zed's GPUI](https://www.gpui.rs/).
It does not replace Track.app. Server mode is opt-in. Ordinary vault use remains read-only. Its purpose is to
reach existing-workspace compatibility while validating native rendering. No claims of improved frame rate are made yet.

## Validation status

Native text rendering, note selection, and virtualized scrolling are verified on
Apple Silicon/macOS 27.2. `font-kit` is explicitly enabled: disabling GPUI defaults
without it selects `NoopTextSystem` and silently removes all glyphs.
This is not yet a feature-compatible replacement for Track.app; see [PARITY.md](PARITY.md).

## Run (macOS)

Install Nix, a current Rust toolchain and Apple's Command Line Tools, then run:

```sh
./experiments/gpui/prepare.sh
cargo run --locked --release --manifest-path experiments/gpui/Cargo.toml
```

The default is a synthetic 10,000-note fixture. Use the floating search action to find and select notes in a virtualized overlay. GPUI uses native Metal rendering, not WKWebView.
Runtime shader compilation avoids requiring the full Xcode Metal compiler.
If a Nix-provided GCC shadows Apple’s toolchain, set `CC=/usr/bin/clang`,
`CXX=/usr/bin/clang++`, and
`CARGO_TARGET_AARCH64_APPLE_DARWIN_LINKER=/usr/bin/clang` when building.

To inspect actual Track titles/tags, explicitly export a vault snapshot:

```sh
TRACK_VAULT=/absolute/path/to/vault track notes > /tmp/track-notes.json
cargo run --release --manifest-path experiments/gpui/Cargo.toml -- /tmp/track-notes.json
```

The viewer only reads that JSON. `track notes` may refresh its derived index but does
not save note bodies; normal Go indexing/sidecar maintenance can still occur. Exported titles/tags are private: do not commit
snapshots or use them in public screenshots. Unknown JSON fields are ignored;
missing optional tags and an empty notes list are supported. Partial reads display
a warning. Malformed input fails before opening a window.

## Scope and evaluation

- One-sheet reader, floating rail, title strip, virtualized search overlay and context aside.
- Read failures and partial data remain visible; full diagnostics are a separate toggle.
- No production editor, full Markdown parity, live updates, full graph interaction parity, persistence,
  accessibility/IME parity, or integration into the shipping app yet.
- macOS is the initial target; Linux/Windows support is not configured or verified.
- GPUI is pinned and Cargo.lock is committed. Keep this experiment outside the main
  Go/Swift/Nix builds; do not add its large dependency graph to normal CI.

Before adopting this architecture, compare release builds with Track.app using the
same note count and window size. Measure frame times and memory with Instruments
while continuously scrolling, selecting, and resizing. Evaluate Markdown and
IME editing separately; list virtualization alone does not establish editor parity.

```sh
cargo fmt --manifest-path experiments/gpui/Cargo.toml --check
cargo test --manifest-path experiments/gpui/Cargo.toml
```

## Read an explicit vault

The first live integration is read-only and uses the existing CLI on background
workers. It does not yet own a web server or subscribe to live events:

```sh
cargo run --manifest-path experiments/gpui/Cargo.toml -- \
  --vault /absolute/path/to/vault --track-bin /absolute/path/to/track
```

Optional `--config /absolute/config.yml` and `--cache /absolute/cache` isolate
configuration and derived caches. Both the executable and vault must be explicit
absolute paths. Select a note to fetch portable Markdown, or use **Reload vault**.
Late responses from previous selections are ignored. CLI failures remain visible;
a 30-second deadline terminates only the owned CLI process. The adapter drains both
output pipes concurrently. The specified executable should be Track itself, not a
wrapper spawning descendants that inherit pipes.

The body supports native headings, paragraphs, lists, quotes and code blocks. Links
remain inert text; HTML, images and other unsupported blocks remain literal.
Inline emphasis, code styling and simple native tables are supported. Syntax
coloring and block virtualization are not implemented. This is not the fully rendered Track document. Export
normalizes links and other constructs: it must **never** be used as editable source.
Editing will use the existing raw-body/etag HTTP contract instead. Search, live
updates, and full Markdown rendering are still tracked as incomplete in `PARITY.md`.

## Native graph

The context aside shows the selected note’s local graph. The synthetic mode
uses a 100-node fixture. Pan by dragging the background or scrolling; hold Ctrl or
Shift while scrolling to zoom around the pointer. Click a node to select it, drag
to move it, and double-click to navigate. Hover/selection
shows metadata. Leaving the canvas ends dragging.

Placement starts circular, then settles a bounded deterministic force snapshot on
a background worker (up to 1,000 nodes). Larger graphs keep circular placement.
This is not existing force-layout parity: neighbors do not follow drags elastically. Full-graph preview pinning, persistent positions,
and label toggles remain incomplete. No renderer performance advantage is claimed.
See [GRAPH.md](GRAPH.md) for research and validation limits.

## Owned HTTP server and search

Add `--server-port 19022` to the explicit-vault command to start an owned loopback
Go server. Both that port and the next port must be available. The application
requires the child readiness token and exposes initialization/storage failures.
Only this child is shut down on the app's Quit command or window close. Abrupt
parent loss relies on Go's existing parent watchdog and is not yet validated here.

HTTP mode reads source Markdown (not portable export) and provides native search
across all served vaults. Search preserves vault-scoped note identities and ignores
outdated navigation/storage-generation responses. Production editing and SSE refresh are not
implemented; use Reload vault for manual refresh. Local graphs use the selected HTTP note’s own vault scope. Japanese IME uses the native input protocol but real composition is
unverified; cursor movement is Unicode-scalar, not grapheme-cluster aware.

## UI compatibility

The previous dashboard-style experiment has been replaced by the existing Track
shell composition and palette. See [UI.md](UI.md). The current Web reference was
captured with public help fixtures in an isolated headless browser. The revised
native shell has compile/unit coverage but still requires fresh matched native
captures; earlier screenshots do not validate this layout. Tab persistence, activity
Home activity and complete dock actions remain incomplete. Search supports Up/Down,
Enter (selected or first result), and Escape with focus restoration. These bindings
are disabled during composition; full native IME validation remains pending.

## Pinned native termination hook

`prepare.sh` uses the repository's locked nixpkgs to fetch the official GPUI crate
by its archive SHA-256 and apply a small [local patch](patches/README.md). The source
is generated in the Nix store and linked under ignored `.build-deps/`; no full
upstream source is vendored. Cargo requires that path and cannot silently fall back
to unpatched GPUI. Re-run preparation after changing the patch or project lock.

The patch exposes a native application-quit request before AppKit termination,
which the read-only workspace routes through owned-server cleanup. Ordinary
window-close interception alone does not cover Dock Quit or OS termination.
The patch is compile/unit-tested, not yet validated through real Dock/OS actions.
Ordinary editing remains disabled until native termination and dirty-document guards
are exercised through the real application. Model tests are not native-quit evidence.


## Disposable editor validation only

`--developer-edit-fixture /absolute/temporary/vault` enables a limited editor only
for that exact `--vault` in owned-server mode. This is not a normal-use feature.
The vault must be a disposable copy under the OS temporary directory or `/tmp`,
with a preexisting `.track-native-edit-fixture` file containing exactly
`disposable-fixture-only` followed by a newline. Explicit temporary `--config`
(file), `--cache` (directory), and a dedicated `--server-port` are required.
Never add the marker to personal data. Path checks are not a sandbox against
concurrent filesystem modification.

The rail provides Preview/Edit/Split and explicit Save. One active draft is
supported. Navigation and exit share Save/Discard/Cancel guards; saving locks
input and retains the original source revision. Ambiguous failures are not
retried automatically. Source comparison requires an exact body match before
explicit reconciliation. Drafts are memory-only: crashes or forced termination
can lose edits. Draft-preview links are inactive. Native IME, Dock Quit, window
close and failure-path UI testing remain required before release.

Heading links currently support ordinary wiki/Markdown fragments against verified
notes, including same-note hashes. Missing headings go to the note top. Block
anchors, encoded fragments and explicit `h-*` IDs remain unsupported and show a
notice rather than guessing a destination. Native fragment interaction is unverified.
