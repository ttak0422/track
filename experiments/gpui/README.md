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
Inline emphasis, code styling and simple native tables are supported. Ordinary
fences have bounded lexical coloring for JS aliases (js/javascript/jsx/ts/typescript/tsx),
Lua and JSON. This is lexical help, not complete language parsing: regex literals,
template interpolation, JSX and Lua long strings/comments are not modeled. Unknown
or absent languages, language labels above 128 bytes, bodies above 64 KiB, or more
than 8192 token runs stay plain
and lossless. Immutable token ranges are cached on parsed blocks at ingestion/change
and shared by title projections; theme/Find/pointer rendering never reclassifies.
Copy and Find use the unchanged parsed body, and rich fence formats remain inert.
Native contrast, overflow, selection and accessibility still need GUI QA. Block
virtualization is not implemented. This is not the fully rendered Track document. Export
normalizes links and other constructs: it must **never** be used as editable source.
Editing will use the existing raw-body/etag HTTP contract instead. Search, live
updates, and full Markdown rendering are still tracked as incomplete in `PARITY.md`.

## Native graph

The context aside shows the selected note’s local graph. The synthetic mode
uses a 100-node fixture. Pan by dragging the background; unmodified scrolling remains available to the enclosing reader. Hold Ctrl or
Shift while scrolling to zoom around the pointer. Click a node to select it, drag
to move it, and double-click to navigate. Hover/selection
shows metadata. Leaving the canvas ends dragging.

Placement starts circular, then settles a bounded deterministic force snapshot on
a background worker (up to 1,000 nodes). Larger graphs keep circular placement.
This is not existing force-layout parity: neighbors do not follow drags elastically.
Persistent positions remain incomplete. No renderer performance advantage is claimed.
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
captures; earlier screenshots do not validate this layout. Tab persistence and
complete dock actions remain incomplete. Home now requests scoped activity from
the owned server and displays a bounded, read-only heatmap. Partial/cache states
remain explicit; cells do not create or open journals. Native heatmap appearance
and accessibility remain unverified. Search supports Up/Down,
Enter (selected or first result), and Escape with focus restoration. These bindings
are disabled during composition; full native IME validation remains pending.

## Reader Find subset

Reader Find searches rendered note body and table text locally, independently of
workspace search. Use the reader rail’s Find action, or Cmd-F while the reader
or Find field has focus, to open its floating field. Enter/Shift-Enter and
Cmd-G/Shift-Cmd-G move between matches. Done/Escape clear highlights while retaining
the query. It is unavailable in the developer editor and never saves or changes a
note. Matching is literal, case-insensitive, non-overlapping and whitespace-flexible;
highlights use original UTF-8 ranges and retain inline styles and link behavior.

This is not full desktop Find parity: title/notices and embedded content are not
searched. Navigation reveals the active match’s first shaped line, not an entire
multiline range; horizontal reveal is not provided. Queries and match counts are
bounded with visible limit states.
Native keyboard/IME, focus, selection and light/dark visual validation remain
pending; see the acceptance checklist in `NATIVE-QA.md`.

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
can lose edits. Draft-preview links are inactive. Native IME, Dock Quit and
complete pending/failure-path UI testing remain required before release. `NATIVE-QA.md` records the exercised dirty-close and lost-save-ack
subset. Use `qa/SAVE-FAULTS.md` only with a fresh disposable fixture to reproduce
held or dropped acknowledgements; it never enables ordinary editing.

Heading links currently support ordinary wiki/Markdown fragments against verified
notes, including same-note hashes. Missing headings go to the note top. Block
anchors, encoded fragments and explicit `h-*` IDs remain unsupported and show a
notice rather than guessing a destination. A plain same-note fragment was
exercised; cross-note and dirty-navigation paths remain unverified.

The rail’s **Full graph** opens a scoped Graph tab using the owned server, with
overview counts and title/reset controls. A [bounded read-only hover preview and
one explicit pin](GRAPH-PREVIEW-FOUNDATION.md) are implemented with synthetic
lifecycle/transport tests; GUI behavior and broader parity remain unverified.
See [GRAPH.md](GRAPH.md#full-vault-graph-prerequisite-2026-11-14).

### Disposable fixture tab snapshots (developer opt-in)

`--developer-session-fixture /canonical/temporary/vault` is independent of
`--developer-edit-fixture`. It requires an owned `--server-port`, matching `--vault`,
an exact `.track-native-edit-fixture` marker (`disposable-fixture-only` plus LF),
and explicit existing canonical temporary `--config` and `--cache`. Cache and
fixture trees must not overlap, and config must be outside cache. Cache and
fixture directories must be owned and not group/other writable; session files are
owner-only regular files with no hardlinks. No arbitrary session path is accepted.
Normal launches and editing-only launches perform no session filesystem I/O.

The only persistent entries are `track-native-tabs-v1.json` and the retained
`track-native-tabs-v1.lock` inside that cache. The JSON contains a format marker,
version, binding fingerprint, revision, tab kind, vault-qualified note ID and title
hint. Limits: 128 tabs, 2,048 title bytes each, 128 KiB serialized snapshot, 256
registry names of at most 1,024 bytes. No body, draft, etag, server token, search
query, preview, scroll position or credential is persisted. Home/Graph labels are
fixed. Binding covers canonical fixture/cache/config identity, configuration bytes
and current available registry names. Replacing even an identical config inode
invalidates it; fingerprints are identity checks, not cryptographic authentication.

Startup reads on the background startup task before any tab write. If accepted
navigation occurs while startup is pending, the server is attached but restored
tabs and automatic list loading are skipped; the newer route is retained until
an explicit reload/navigation. Restored note
order is retained, but Home is inserted/promoted as the safe active view. No note
body GET is issued by restoration or initial list loading in this mode. Selecting
a restored tab makes a fresh current owned-server request; unknown/unavailable
vaults never fall back to launch scope. Registry changes disable persistence.
Malformed, oversized, unsupported or mismatched snapshots remain untouched and a
nonfatal banner explains that tabs are now memory-only. There is no reset/delete
button or automatic migration.

Accepted navigation and fetched title changes enqueue a bounded latest metadata
snapshot to one background writer. Dirty Cancel, pending Save and failed Save do
not enqueue rejected navigation. This is UI-state persistence, not note autosave.
Writes use a held exclusive advisory lock, expected original bytes/revision,
no-follow descriptor-relative access, private same-directory temporary files and
atomic rename with file/directory fsync. Another cooperating instance is disabled,
not allowed to overwrite. Failures stop retries and preserve a complete old/new
snapshot, with a nonfatal UI warning. Do not remove a live instance's lock file.
These protections are not a sandbox against malicious same-user filesystem races.

The UI and quit guard never wait for disk. Coalesced, queued snapshots may be lost
on immediate quit/crash; restart uses the last completed atomic snapshot. No
power-loss durability guarantee is made. Note Save and the dirty/quit gate are
unchanged. Native relaunch/quit interaction remains manual QA pending; automated
tests create only fresh synthetic temporary fixtures.

### Opt-in real-engine save races (headless)

`live::save_races::owned_engine_post_preflight_conflict_and_lost_ack` is ignored
by default. Set `TRACK_NATIVE_TEST_BINARY` to an explicit absolute Track executable
and `TRACK_NATIVE_TEST_SYNTHETIC_ONLY=1`, then run:

```sh
cargo test --offline --locked --manifest-path experiments/gpui/Cargo.toml \
  owned_engine_post_preflight_conflict_and_lost_ack -- --ignored --nocapture
```
It creates an empty temporary vault and two uniquely titled synthetic notes through the CLI,
and writes only those notes after owned-server and canonical fixture checks.
A test-only loopback gate exercises a real post-preflight conflict and a real
commit whose acknowledgment is dropped. It does not launch GPUI, change the
production save path, or establish GUI/IME/Dock behavior. See [NATIVE-QA.md](NATIVE-QA.md).

## Session page history

Use the Back and Forward rail controls on Home, Graph or a note. Unavailable
directions are dimmed. With reader focus, Cmd-[ and Cmd-] also request Back and
Forward through the last 100
accepted Home, Graph and note routes. This is chronological page history, not tab
cycling. Notes retain their vault and ID; visiting a closed note reopens its tab.
Closing an active tab records its neighbor (or empty Home); a background close
leaves the active draft and route alone. Empty Home replays as literal Home, never
as the configured Home note. Graph uses the current working-vault scope.

A new route after Back drops Forward; title changes and repeated navigation to the
same route do not add entries. Note reads may fail after the route is accepted;
the destination remains selected with its read error, and Back remains available.
Fixture-only editing uses the existing Save/Discard/Cancel gate before moving the
history cursor. Cancel retains the draft and route; repeated requests while the
prompt is open do not skip destinations. Ordinary editing remains disabled.

History is in-memory only, with no heading positions or unsupported route kinds.
Model and disposable-fixture tests do not establish native shortcut/IME delivery;
GUI validation remains deferred until explicitly authorized. Mouse side-button
behavior is unchanged.

### Identify a running build

The diagnostics drawer shows the package version, full source commit, tracked-source
state and target captured at compile time. The same information is available without
opening a window, vault or server:

```sh
./experiments/gpui/target/debug/track-gpui-poc --version
```

`modified` means tracked repository files differed from the commit during compilation.
Untracked files are not covered; this is diagnostic provenance, not a reproducible-build
attestation. `unknown` means Git metadata was unavailable, not that the build is current.
Changes to the checkout after compilation do not change the executable's identity.

### Read-only note metadata slice

Live tags now come from the accepted current note source, not the list snapshot.
Current-note flags and hierarchy/backlink rows (including cross-vault references)
show plain text labels such as `[DEPRECATED] [CONFIDENTIAL]`; empty flags add no
label. Unknown string flags remain literal text. Tags and flags clear with the
accepted source and require the active vault-qualified note identity; a retained
body after server failure retains its matching labels and existing error warning.
Snapshot/synthetic mode keeps its snapshot tags. `CONFIDENTIAL` is a display label,
not access control. No extra requests or metadata writes are introduced.

Headless tests cover source preference, empty values, clearing/identity changes,
and relation labels. Native layout, long labels, accessibility and decorative
stamp parity remain unverified. Read-only metadata parity remains partial: existing hierarchy and
backlinks plus these labels do not establish full navigation or UX parity.
