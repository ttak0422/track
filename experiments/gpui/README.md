# GPUI rendering experiment

An isolated, read-only native workspace using [Zed's GPUI](https://www.gpui.rs/).
It does not replace Track.app, start a server, or change the vault. Its purpose is to
validate GPU-rendered interaction and virtualized lists before choosing a larger
native architecture. No claims of improved frame rate are made yet.

## Validation status

Native text rendering, note selection, and virtualized scrolling are verified on
Apple Silicon/macOS 27.2. `font-kit` is explicitly enabled: disabling GPUI defaults
without it selects `NoopTextSystem` and silently removes all glyphs.
This is not yet a feature-compatible replacement for Track.app; see [PARITY.md](PARITY.md).

## Run (macOS)

Install a current Rust toolchain and Apple's Command Line Tools, then run:

```sh
cargo run --release --manifest-path experiments/gpui/Cargo.toml
```

The default is a synthetic 10,000-note fixture. Scroll and select notes to exercise
the virtualized list and inspector. GPUI uses native Metal rendering, not WKWebView.
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
not edit note bodies or metadata. Exported titles/tags are private: do not commit
snapshots or use them in public screenshots. Unknown JSON fields are ignored;
missing optional tags and an empty notes list are supported. Partial reads display
a warning. Malformed input fails before opening a window.

## Scope and evaluation

- Virtualized, fixed-height note list; selection; native inspector; partial-read warning.
- The footer reports the last virtualized range, not a frame-rate benchmark. Click a
  row to refresh the displayed count after scrolling.
- No editor, full Markdown parity, live updates, force graph layout, search, persistence,
  accessibility/IME parity, or integration into the shipping app yet.
- macOS is the initial target; Linux/Windows support is not configured or verified.
- GPUI is pinned and Cargo.lock is committed. Keep this experiment outside the main
  Go/Swift/Nix builds; do not add its large dependency graph to normal CI.

Before adopting this architecture, compare release builds with Track.app using the
same note count and window size. Measure frame times and memory with Instruments
while continuously scrolling, selecting, and resizing. Then prototype Markdown and
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
remain inert text; tables, HTML, images and other unsupported blocks remain literal.
Inline emphasis and syntax coloring are not implemented, and blocks are not yet
virtualized. This is not the fully rendered Track document. Export
normalizes links and other constructs: it must **never** be used as editable source.
Editing will use the existing raw-body/etag HTTP contract instead. Search, live
updates, and full Markdown rendering are still tracked as incomplete in `PARITY.md`.

## Native graph

Select **Local graph** to read the selected note's CLI graph. The synthetic mode
uses a 100-node fixture. Pan by dragging the background or scrolling; hold Ctrl or
Shift while scrolling to zoom around the pointer. Click a node to select it, drag
to move it, and double-click or choose **Open note** to navigate. Hover/selection
shows metadata. Leaving the canvas ends dragging.

Placement is deterministic and circular, not the existing force layout: neighboring
nodes do not follow elastically. Full-graph preview pinning, persistent positions,
and label toggles remain incomplete. No renderer performance advantage is claimed.
See [GRAPH.md](GRAPH.md) for research and validation limits.
