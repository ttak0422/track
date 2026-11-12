# GPUI rendering experiment

An isolated, read-only native workspace using [Zed's GPUI](https://www.gpui.rs/).
It does not replace Track.app, start a server, or change the vault. Its purpose is to
validate GPU-rendered interaction and virtualized lists before choosing a larger
native architecture. No claims of improved frame rate are made yet.

## Current validation blocker

The prototype builds and its snapshot tests pass, but native QA on macOS 27.2
showed backgrounds and list rows without visible text. This reproduces with the
system font and Helvetica. GPUI 0.2.2 / runtime Metal shaders are the tested
combination; the cause is not established. This is a **rendering experiment, not a
usable replacement for Track.app**. Resolve glyph rendering and repeat interaction
QA before drawing performance conclusions or extending it into an editor.

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
- No editor, Markdown rendering, live updates, graph layout, search, persistence,
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
