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

The body supports native headings, paragraphs, lists, quotes, thematic separators
and code blocks. Verified links use the guarded navigation described below; HTML
and unsupported media remain literal. CommonMark thematic rules render as dividers,
not searchable source punctuation; identical text inside code remains literal. A bounded local
PNG/JPEG subset can be displayed in the owned live reader (see below).
Inline emphasis, code styling and simple native tables are supported. Ordinary
fences have bounded lexical coloring for JS aliases (js/javascript/jsx/ts/typescript/tsx),
Lua, JSON, Python (python/py/python3) and shell (bash/sh/shell). Shell coloring
provides whole-word reserved-keyword hints, boundary-aware comments and quoted
strings/escapes. It does not parse substitutions or keyword positions; double-quoted
interpolation stays opaque, and an unquoted `<<` sequence (including here-strings or shifts) leaves the remainder
plain. Unquoted parameter/command substitution openers also leave the rest plain. Nested quotes inside double-quoted substitutions and ANSI-C strings may
be colored incorrectly; CRLF handling is display-oriented, not shell validation. Zsh is not classified as this shell subset. Python supports comment, keyword, numeric
and single/triple-quoted string hints; prefixes/interpolations are not parsed and
contextual keywords stay plain. This is lexical help, not complete language parsing: regex literals,
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

The Full graph canvas also accepts focused keyboard selection: arrows choose the
nearest node in that direction, Home/End choose the first/last note ID, Enter opens
the selected note and Escape clears selection/temporary preview. Selection stops
unfinished background settling before revealing the node. Click the canvas or
press F7 from reader/no focus first; Escape/Tab leaves canvas focus. These are local unmodified keys, not global shortcuts; native
focus, IME and accessibility validation remains pending.

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
outdated navigation/storage-generation responses. Production editing and automatic
SSE refresh are not implemented; use Reload vault for manual refresh. Owned HTTP
sessions observe note/data change notifications and stream interruptions; this is
change awareness, not a claim that every displayed view is current. Local graphs use the selected HTTP note’s own vault scope. Japanese IME uses the native input protocol but real composition is
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
explicit reconciliation. Drafts are memory-only unless the separate developer recovery opt-in described
below is enabled. Crashes can still lose checkpoints marked Pending. Draft-preview links are inactive. Native IME, Dock Quit and
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

### Disposable fixture draft recovery (developer opt-in)

Add `--developer-draft-recovery` only when both `--developer-edit-fixture` and
`--developer-session-fixture` authorize the same disposable fixture, with the
existing explicit protected temporary config/cache and owned-server settings.
The boolean accepts no path. Never mark a personal vault as a fixture. Ordinary
launches and launches without this additional opt-in perform no recovery-file I/O.

One draft is checkpointed asynchronously to `draft-recovery.json` in the protected
cache, separately from tabs/history. **Pending** is not durable. **Saved locally**
means that exact draft revision completed its local checkpoint, not that the note
was saved. A crash can lose newer pending text. Checkpoints share the session
storage owner and lock; errors retain the draft and do not retry blindly.
There is no in-session retry after a storage failure; retain the current text
and inspect the fixture/cache before restarting.

On restart, **Unsaved fixture draft available** does not fetch or open a note.
Explicitly open the same fixture note and obtain a fresh source, then use
**Recover draft (no save)** in diagnostics. Recovery installs a dirty draft only
when the source identity and base revision still match. Changed, missing or
unavailable sources retain the record without silently rebasing it. An unresolved
record blocks starting a different draft. **Discard saved fixture draft…** asks
for confirmation identifying the note; it does not delete the note or session.

Existing Save/Discard/Cancel prompts remain. Successful Save or explicit Discard
orders an exact recovery clear before a pending transition. A failed or uncertain
save retains recovery. If the local barrier fails or exceeds five seconds, the
transition is suppressed; Cancel keeps the editor. Timing out does not cancel an
in-flight filesystem operation or start a second writer. Closing alone does not
clear recovery. Ordinary editing remains disabled.

The store accepts one UTF-8 draft up to 2 MiB, with a 3 MiB artifact cap and bounded
metadata. Invalid, foreign or concurrently changed files are preserved, not reset.
Private regular-file checks and atomic synchronized replacement are not a sandbox
against a same-user process racing filesystem changes, and power-loss durability
is not established. Native Dock Quit, IME, pending-save and accessibility QA remain
required; headless tests are not evidence that those interactions are safe.

### Disposable fixture tab/history/scope snapshots (developer opt-in)

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
version, binding fingerprint, revision, tab kind, vault-qualified note ID, title
hint, bounded route history/cursor and working-vault scope. Limits: 100 history routes, 128 tabs,
2,048 title bytes each, 128 KiB serialized snapshot, 256
registry names of at most 1,024 bytes. Explicit reading checkpoints may additionally
store up to 128 note revisions, semantic block indices and layout/build identifiers.
No body, draft, server token, search query, preview or credential is persisted. Home/Graph labels are
fixed. Binding covers canonical fixture/cache/config identity, configuration bytes
and current available registry names. Replacing even an identical config inode
invalidates it; fingerprints are identity checks, not cryptographic authentication.

Startup reads on the background startup task before any tab write. If accepted
navigation occurs while startup is pending, the server is attached but restored
tabs/history/scope and automatic list loading are skipped; the newer route is retained until
an explicit reload/navigation. Restored note
order is retained, but Home is inserted/promoted as the safe active view. No note
body GET is issued by restoration or initial list loading in this mode. Selecting
a restored tab makes a fresh current owned-server request; unknown/unavailable
vaults never fall back to launch scope. Registry changes disable persistence.
Malformed, oversized, unsupported or mismatched snapshots remain untouched and a
nonfatal banner explains that tabs are now memory-only. There is no reset/delete
button or startup migration.

Fixture snapshots read legacy tabs-only schema 1, tabs/history schema 2 and
tabs/history/scope schema 3, and write schema 4 after an accepted metadata or
checkpoint change. Legacy snapshots
start with the launch scope. The saved working scope must still be available in
the same validated fixture registry; unknown scopes disable persistence rather
than falling back. Scope restoration does not reinterpret note identities or
make federated search single-vault. Restored history discards its saved Forward
branch and appends safe Home; Back revisits earlier routes only on explicit input.
Startup does not rewrite the snapshot (the lock file may be created). Malformed
history, duplicate/unknown fields, invalid cursors and unavailable vault identities
preserve the original artifact and disable persistence.

Accepted navigation, scope selection and fetched title changes enqueue a bounded latest metadata
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

## Save and close shortcuts

Cmd-S (File → Save) uses the existing guarded fixture Save and stays in the editor.
Cmd-W (File → Close Tab) closes the active tab, not the application; dirty notes
retain Save/Discard/Cancel protection. In About, Cmd-W dismisses only About.
Modal surfaces, composition and pending work block underlying workspace actions.
These shortcuts do not enable ordinary-vault editing.

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

The application menu’s About Track Native and the diagnostics drawer show the package
version, full source commit, tracked-source state and target captured at compile time.
Copy Build Info remains a separate explicit clipboard action. About does not access
the vault, and closing it leaves the workspace open. The same information is available without
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

Children and backlinks use 100-row pages in engine order. Lists over 4096 entries
show a limit notice instead of truncating results. Partial or cached reads are
labeled incomplete rather than implying that missing relations do not exist.
Pagination resets when the
accepted source changes. Relation and breadcrumb navigation reject stale sources
and destinations no longer present in the accepted response. No extra fetch or
write is triggered by paging.

Headless tests cover source preference, empty values, clearing/identity changes,
and relation labels. Native layout, long labels, accessibility and decorative
stamp parity remain unverified. Read-only metadata parity remains partial: existing hierarchy and
backlinks plus these labels do not establish full navigation or UX parity.

## Read-only live change awareness

Owned HTTP sessions subscribe to `/api/events` on their own loopback origin.
`change` and `data` notifications produce a notice with **Reload**. `follow`
signals are ignored unless **Follow editor** is explicitly enabled (see below). A disconnected stream stays visibly warned after an
observed gap. Reconnection does not erase the gap because the server does not
replay missed events. **Dismiss notice** acknowledges only the observed counters;
a newer notification remains visible. Dismiss does not refresh or certify content.

Reload uses the existing dirty-navigation gate; Cancel, a pending Save or failed
Save never authorizes replacement. Change/data events and reconnects never replace
a body, navigate, save, or discard a draft. Reload does not automatically dismiss
a notice, and pinned previews are not globally refreshed. The server primarily
watches the launch vault; other served vaults refresh on reads. Absence of a
notice is not proof of freshness.

The observer uses bounded SSE frames, a bounded reconnect delay and cancelable
async transport, with one worker for an owned session. It accepts only the owned
loopback endpoint, refuses redirects/proxies and never stores event payloads.
Header/connection and heartbeat inactivity deadlines do not impose a recurring
whole-stream lifetime timeout. CLI/snapshot modes do not create an observer.
Headless loopback/model tests are separate from native notification/Reload/IME
validation; full automatic live refresh remains unimplemented. Explicit
external-editor follow is described below.

### Session-local reader positions

Reader offsets are kept per vault-qualified note during tab/history navigation.
Changed accepted content or owned-source identity resets the old position; a
loading placeholder alone does not. Find and explicit fragment jumps retain
their own behavior. These automatic offsets are not saved to disk and do not mark
notes seen/read. Explicit fixture-only block checkpoints are a separate opt-in.
Native scroll behavior after source changes, resize and reload remains unverified.

### Read-only daily agenda

With a day selected, **Previous day** and **Next day** move exactly one civil date,
including empty days. Notes, dated tasks and calendar events switch together;
the working scope and accepted display zone stay unchanged. Navigation stops at
the displayed activity-calendar range, with disabled boundary controls. These
controls only read data; they never create a journal.


Select an activity-calendar day on Home to list notes recorded as active that day
in the working vault. This sends an explicit-date `GET /api/agenda`; it never calls
the journal-creation endpoint. Selecting a result uses the existing vault-qualified
note navigation and dirty-state guard. At most 100 rows are shown (the transport
rejects responses over 4096 notes); truncation and incomplete/cached results are
labelled. Failures retain the selected date and offer an explicit retry. There is
no background polling, automatic navigation, pagination or event/task mutation.
Late responses from another date, scope, source or retry are discarded. Date
semantics come from the existing Go engine, not a new client timezone conversion.
Native calendar picking, row layout, keyboard and accessibility remain unverified.

Agenda reads allow at most two in-flight requests across experimental windows.
If earlier reads are still finishing, retry the selected day after they finish.

The selected day also lists tasks whose recorded scheduled or due date equals
that day. The engine's dated-task GET includes all states, including DONE and
CANCELLED; this is not an overdue or recurrence-expanded view. A task matching
both fields is shown once. The state, scheduled/due dates and source note/line are
labels only. Selecting a task opens its note, not a line jump or editor. No task
mutation endpoint is called. Each response is bounded to 4096 dated tasks, and
at most 100 matches are shown with a truncation notice. Task and active-note
failures are shown separately; Retry refreshes the day lists under the same identity and
two-job concurrency guard. Native layout and accessibility remain unverified.

Task dates remain authored date-shaped labels: an impossible calendar date is
not repaired or used to hide other tasks. Only exact matches to the selected
valid calendar day are included.

### Bounded local raster images

The current owned live reader can display up to four standalone Markdown image
paragraphs referring literally to `assets/<name>.png|jpg|jpeg`. Source captions
remain visible and searchable. Inline/table/code images, external URLs, data/file
URLs, SVG, GIF and other formats remain literal; no external image fetch is sent.
Only the owned engine's scoped `GET /api/asset` is used. Paths reject traversal,
percent-encoding ambiguity, query/fragment syntax and backslashes. This is lexical
validation, not physical containment: the engine owns filesystem/symlink policy.

Encoded payloads are limited to 4 MiB, dimensions to 2048 per axis and four million
pixels. Decoder allocation accounting is capped at 16 MiB, not a hard process-RSS
guarantee. Decoded BGRA frames are passed directly to GPUI; its URL/encoded-image
loader and SVG fallback are never invoked. One image worker runs at a time across
experimental windows, with only the current reader cache retained. Launch,
storage, requested note, request and ETag changes reject old results. Loading is
read-only, with no disk cache or automatic retries; Reload retries failed/busy
loads. Pinned previews and offline snapshots do not load images. Images are scaled
within reader bounds; orientation/color-profile and native GPU/layout/scroll/AX
parity remain unverified. Ordinary editing stays disabled.

### Native calendar display zones

Select a Home activity day to list calendar events in the working vault. The
initial display zone is explicitly **UTC**, not the device time zone. Enter a
named zone such as `Asia/Tokyo` or `America/New_York` and choose **Apply** to
request that date in the selected zone. Typing alone does not fetch. A failed
Apply keeps the previous accepted zone and results, with a separate error;
it never silently falls back to UTC or relabels old results.

The engine determines the actual interval for that civil date, including
23-hour and 25-hour daylight-saving days and midnight transitions. Nonexistent
or unsupported historical dates fail explicitly. Timed events use precise
instant comparisons, including offsets and fractional seconds. Their original
start/end timestamps and event zone remain visible. All-day events keep their
independent civil-date range, including the first date and excluding the end
date. Cancelled events remain visible.

The applied zone is session-local. Device-zone following, persistent calendar
preferences, recurrence expansion, event editing and event-link opening remain
unsupported. Existing calendar clients retain their original list API.

Up to 4096 source events are accepted; larger or unreadable snapshots fail
explicitly rather than appearing complete. Results use pages of 100 rows, so
later matches remain reachable. Active notes, tasks and events report failures
independently. Stale responses cannot replace a newer date, zone or scope.
Successful results are snapshots, not a guarantee of vault-wide freshness.
Native layout, keyboard, IME and accessibility QA remains pending.

### Recorded reading milestones

The accepted live note may show **Recorded read** or **Recorded seen** from the
engine's positive integer milestone fields. Read takes precedence. Missing, zero,
negative or malformed values show no label because absence can also mean metadata
was unavailable; no NEW/unread state is inferred. Snapshot/list hints are not used.
This is display-only: no viewing timer, milestone POST, persistence or automatic
refresh is added. Native layout/accessibility validation remains pending.

### Read-only footnote display

The native reader displays footnote references as `[^label]` and definitions with
that same label followed by their formatted content. Definitions stay at their
source position, including unused definitions. Their paragraphs, lists, quotes
and ordinary links remain readable and searchable.

In the current live reader, click a reference to jump to its unique definition.
Undefined or duplicate labels stay inactive. The jump is local: it does not reload
the note, open another tab or write anything. Changed source and blocked reader
interactions cancel stale clicks. After a jump, the small **Return to reference**
control restores the position where you clicked, including repeated and table
references. It is one-use and session-only, with no history or saved state.
Changing the note, reader mode, layout or competing reader action clears it.

This is not full web footnote parity: references are not renumbered, definitions
are not collected at the end, and automatic definition backlinks are not added. Unresolved
references remain visible. No source is rewritten.
Native layout, selection and accessibility validation remains pending.

### Read-only vault hierarchy

On Home, open **Hierarchy** to load the working vault's existing parent/child tree.
Expand or collapse branches and select a note title to open it through the normal
guarded reader navigation. Refresh explicitly to request an updated tree.

The engine determines membership and ordering. This is not an all-notes list:
isolated notes and cyclic parent structures may be absent. An empty tree does
not mean the vault is empty. Flags remain visible and incomplete/cached results
are identified instead of being presented as complete.

This browser only reads the hierarchy; it does not edit parent links or reorder
notes. Scope, server, storage and newer requests invalidate stale results/actions.
Responses are limited to 4096 nodes and 32 levels. Larger/deeper forests are
rejected rather than silently cut. The visible tree is paged in groups of 200
rows; paging and branch expansion do not issue network requests. Closing the
section discards its loaded tree; reopening explicitly reloads it.
Native layout, keyboard, scrolling and accessibility validation remains pending.

### Read-only note tasks

In the live reader, open **Note tasks** to inspect the current note's parsed tasks,
including tasks without dates. The list shows engine-provided state, priority,
scheduled/due/completed dates and one-based source line numbers. It preserves
source order and includes completed and cancelled items.

The inspector uses the task data already delivered with the accepted note body;
opening it or paging does not issue another request. Reload the note to obtain
updated tasks. After a fixture save or accepted comparison replaces the local
body, task metadata is unavailable until Reload obtains a fresh note response.
It is read-only: there are no state changes, date edits, drag
operations or source-line jumps. Line numbers describe that loaded version only.

Up to 4096 tasks are accepted, with 100 rows per page. Malformed or larger task
sets show an error rather than being interpreted as no tasks. Note/source changes
reset the inspector and stale controls cannot operate on a different note.
Native layout, keyboard, scrolling and accessibility validation remains pending.

### Read-only rendered-content previews

Open **Rendered content (read-only)** in the live reader to ask the owned engine to
resolve includes in the accepted note body. **Refresh** requests another snapshot;
closing the section discards it. Rendering a closed section makes no request.
The inspector displays engine-selected excerpt text, origin and resolution errors.

These are inert text previews, not inline transclusion parity. Nested includes,
links, images and task controls inside excerpts are not executed. Host Find does
not search the inspector, and the host Markdown, headings and source positions
remain unchanged. Target revisions describe that response only; the endpoint does
not provide per-target completeness diagnostics or guarantee they remain current.

Results are limited to 32 includes and 256 KiB of excerpt text. Malformed or larger
responses show an error, not a silently incomplete list. Note, request, server and
storage changes invalidate old results; loading, editing and closing gates remain
in effect. The operation renders only, without saving a note or reading milestone.
Native layout, keyboard and accessibility validation remains pending.

After a successful snapshot, choose **Rendered preview** for a separate read-only
view with supported includes displayed in place. Only a directive occupying its
own top-level paragraph is embedded. Adjacent directive lines, lists, quotes,
tables and other ambiguous placements remain visible with an unsupported-location
notice rather than being inserted at guessed positions. The returned engine
Markdown is used for placement, never line numbers applied to the raw source.

Preview links, images, nested includes and task actions are inert. Host Find,
heading jumps and reading checkpoints are unavailable in this mode; preview text
must not become a saved host position. Close returns to the unchanged original
reader. The preview is bounded and fails explicitly if its block/text limits are
exceeded. This is not automatic transclusion or full navigation/selection parity;
native scrolling, focus and accessibility still require fixture QA.

### Copy code in rendered previews

In **Rendered preview**, use a tagged code block's **Copy code** control to copy
its displayed body as plain text. Retained diagram/chart input has **Copy source**;
this copies the visible input, never generated geometry or an original query
that the engine has replaced. Generated query snapshot data is labeled separately.
Included excerpts copy only their displayed code, without fetching another note.
Untagged blocks have no Copy control.

Fence delimiters, language captions and surrounding prose are excluded. The
preview removes one conventional final newline from the parsed body and preserves
other whitespace and Unicode; it is not a byte-for-byte file export. Payloads
above 256 KiB cannot be copied and are never silently truncated. Existing raw-reader
Copy behavior is unchanged.

Copy requires explicit activation of a current control. Closing, refreshing,
replacing the source or entering an unavailable/editing state invalidates old
controls and feedback. **Copy requested** acknowledges submission to the native
clipboard API, not verified clipboard contents. Nothing reads the clipboard or
copies automatically. Preview text selection, rich-text export, and link/task
interactions remain unavailable. Native clipboard delivery, keyboard focus and
accessibility still require fixture QA.

### Embedded task boards in rendered previews

An empty `taskboard` fence in the host note appears as a **Task board (read-only)**
in Rendered preview. It uses the task metadata accepted with that exact note
snapshot, without another request or a second Markdown task parser. Five state
lanes preserve source order and show counts, literal task text, priority and date
labels. Dates are not interpreted as overdue or normalized. There are no state
controls, drag-and-drop, line jumps or writes.

Three lanes and 50 cards per lane are shown at a time; paging exposes the rest.
An empty task set is explicit. Invalid or unavailable metadata is not treated as
an empty set. Included excerpts cannot borrow the host's tasks: their fence stays
visible with an unavailable notice. Nonempty fence content or extra options also
retain source rather than silently losing content. Query layouts, task boards and charts
share the eight-visual-block preview limit. Native layout/input/accessibility
validation and interactive task-board parity remain pending.

### Static native charts in rendered previews

A supported `echarts` JSON fence in **Rendered preview** draws a static categorical
line chart or vertical grouped-bar chart. Category order, series colors and
legend names are preserved. Missing (`null`) observations break lines or omit
bars; they are never converted to zero. Bars use a zero baseline. Axis labels
show the plotted numeric domain, which may differ from ECharts' tick styling.

For example, place this fence in a fixture note and open Rendered preview:

````markdown
```echarts
{"xAxis":{"type":"category","data":["A","B","C"]},"yAxis":{"type":"value"},"series":[{"name":"Score","type":"line","data":[1,null,3]}]}
```
````

This is a deliberately limited native renderer, not an ECharts runtime. It does
not execute JavaScript, load a browser or fetch chart assets. Limited inline
ViewSpec resolution is described below; metrics dashboards remain unsupported. Zoom, hover details, animation and chart actions are unavailable.
Unsupported semantic options, mixed chart types, bars without category-edge
gaps, invalid values or clipped explicit ranges retain the entire source with a
reason instead of drawing a
misleading partial chart. Source remains available for supported charts too.

Charts share the preview's eight-visual-block limit with query layouts and task
boards. Each chart is bounded to four series and 256 categories. Very narrow or
unusable geometry shows an unavailable state rather than invalid drawing.
Native appearance, high-DPI layout and accessibility acceptance remain pending.

### Inline ViewSpec charts in rendered previews

In an owned-server session, **Rendered preview** can resolve a limited `viewspec`
fence through the existing engine and draw its supported native chart. This
requires ViewSpec version 2, a line or bar mark, inline metric records and a
primary category/value encoding. The original specification stays visible with an
explicit source Copy control. Preview text selection remains unavailable.

A small fixture example:

````markdown
```viewspec
{
  "version": 2,
  "mark": "line",
  "data": {
    "kind": "metric",
    "records": [
      {"version": 1, "name": "score", "time": "A", "value": 1},
      {"version": 1, "name": "score", "time": "B", "value": 3}
    ]
  },
  "encoding": {"x": {"field": "time"}, "y": [{"field": "value"}]}
}
```
````

File-backed `data.source` is refused before any request, including relative
paths. Empty records, duplicate JSON keys, overlays, filters, sorting, rolling
windows, secondary axes and other unsupported fields also retain source with a
reason. This is not full ViewSpec or metrics-dashboard support. The client does
not rewrite unsupported specifications or read local chart files.

Resolution starts only when the accepted rendered preview is opened. Up to eight
visual blocks share one bounded request queue; duplicate specifications in the
same verified vault share a result. Closing or replacing the preview invalidates
queued results. Failures are not retried on every redraw; request fresh rendered
content to retry. Included specifications require an explicit verified source
vault and never fall back to the host vault.

The engine remains responsible for evaluating records. Its returned chart must
also pass the native renderer's strict subset; unsupported output keeps the
original specification rather than losing data or options. No note writes,
external fetches, JavaScript or ordinary editing are enabled. Native interaction
and visual acceptance remain pending.

### Native mindmaps in rendered previews

A `mindmap` fence in **Rendered preview** draws a static left-to-right tree of
rounded text nodes. Headings create branches; unordered bullet items create
leaves or nested children beneath the most recent heading. Source order and
duplicate labels are preserved. Simple link labels and wiki aliases are shown
as text only, without opening or resolving their targets.

````markdown
```mindmap
# Vault
## Notes
- [[Syntax]]
- [[Diagrams|Visuals]]
## Resources
- [Graphviz](https://graphviz.org/)
```
````

A whitespace-only fence uses ATX headings from the same captured host Markdown,
excluding fenced code. It never uses the window title or another note's headings.
An empty included mindmap stays unavailable when its complete surrounding source
is unknown; explicit outlines within included excerpts remain self-contained.

This subset supports one to six hash heading levels and unordered bullets with
zero to eight spaces of indentation in two-space steps. Tabs count as two spaces.
Ordered lists, Setext headings and arbitrary indented prose are not outline
nodes. Omitted prose is reported rather than silently presented as mapped content.
Unsupported indentation, oversized trees or unusable layout retain the whole
source with a reason. A forest uses an unlabeled connecting root, not an invented
title. Maps are bounded to 128 nodes, depth 16 and 32 KiB per fence, and share the
preview's eight-visual-block limit.

The original fence stays visible. No scripts, images, file references or extra
requests are involved. Native layout, text measurement, keyboard and accessibility
acceptance remain pending; interactive mindmap parity is not yet provided.

### Query layouts in rendered previews

The same **Rendered content (read-only)** action works for notes containing
`track-query` fences without includes. The engine resolves the query; **Rendered
preview** displays supported returned list, board, calendar and gallery results at their Markdown
positions. Lists preserve engine order. Boards use side-by-side lanes with lane
counts and literal card metadata. Empty results and empty lanes remain visible.

Lists use 100-row pages. Boards use four lanes per page and 50 cards per lane;
page controls expose later rows without fetching again. Preview replacement or
close resets those controls. Titles, metadata and target strings are inert:
there is no note opening, drag-and-drop or task mutation.

Calendar results use Sunday-first month grids, one supplied month per page. Each
date shows up to three literal titles and a count of remaining rows. The engine
determines civil-date groups; this is not a time-zone calendar-event projection.
Undated rows are not reconstructed. Empty dated results are explicit, and invalid
or ambiguous date groups retain the original source rather than being normalized
or silently dropped. Month paging does not fetch or write. **Show source (all supplied rows)**
displays the returned query block, including rows summarized by the daily count.

Gallery results use four-card pages with literal titles and metadata. Visible
covers may load only bounded local PNG/JPEG assets through the owned engine,
scoped to the accepted note or verified included-note origin. Missing include
origin never falls back to the host vault. No-cover cards show their literal icon
or a neutral face; unsupported references and failed loads show a visible state.
Remote URLs and unsupported image formats never trigger another loader. Hidden
pages do not fetch covers. Thumbnail work and memory are bounded, and stale
results cannot update a replaced preview. Thumbnails are at most 320 × 180, with
a 32-entry/8 MiB cache shared across pages of that preview. Busy-worker and evicted
cover states require a fresh rendered-content refresh to retry; they are not
automatically retried. Close/source changes discard the cache. Native cover/layout
QA remains pending.

Unknown layouts, malformed payloads and over-limit blocks retain their
source fence with a notice rather than pretending to be a supported layout.
At most eight query blocks are visualized per preview. Each accepts up to
256 KiB of JSON, 32 columns, 128 groups and 4096 rows, with bounded text fields.
Existing whole-preview limits still apply. This is snapshot reading, not live
query refresh or complete query-view navigation. Native layout, keyboard and
accessibility validation remain pending.

### Fixture-only reading checkpoints

With disposable-fixture session persistence enabled, **Save reading position**
records the first visible Markdown block. **Resume saved position** explicitly
returns to that block after you manually reopen the note. Long paragraphs and
code blocks resume at their beginning, not the exact line previously visible.

This requires a known clean build, an unchanged accepted note revision and matching
measured reader/viewport geometry. Unknown, changed or unmeasured conditions refuse
resume rather than guessing. Only the main Markdown article is eligible, not
metadata, task/include inspectors or graph previews. Resolved-title projections
that change the displayed host blocks also make checkpoints unavailable. At least
one host block must be visible, including when requesting Resume. Source changes, newer manual
scrolling and other navigation invalidate a pending resume.

Checkpoints share the protected fixture session snapshot and background writer.
They contain note identity, revision, block index and layout/build identifiers,
not note bodies or drafts. Saving is queued; a queued update is not a guarantee
that it survived an abrupt exit. Normal launches collect no checkpoint geometry
and perform no session I/O. Nothing automatically saves on scroll, resumes at
startup, fetches a note or changes seen/read milestones. Native interaction and
layout validation remain pending.

### External-editor follow

In an owned live session, explicitly enable **Follow editor** to follow the editor's
saved/indexed note. It is off at launch and is not persisted. This is separate
from one-time Markdown handoffs: it never opens publisher filesystem paths or
reads unsaved buffers. The raw reader can follow the editor viewport approximately
by its source-line proportion, using current measured reader geometry. This is
not exact source-line alignment or cursor highlighting. Missing or changed layout
measurements leave scrolling unavailable rather than guessing an offset.

Follow reads the owned server's current editor state on enable and subsequent
follow signals. Signals are coalesced into bounded latest-state requests, not
replayed as navigation commands. Missing or expired publisher state leaves the
current note unchanged and shows a waiting state. Unknown vaults, errors and
connection gaps pause follow rather than guessing a target or retrying forever.

Stop is immediate. Manual scrolling, resizing, navigation, editing, pending
actions and competing surfaces take precedence; follow cannot discard a draft or
repeatedly open save
prompts. Resume requires explicit action after a pause. No publisher POST, note
write, automatic body refresh or reading checkpoint is sent. Native editor/IME,
keyboard and lifecycle acceptance remains pending; ordinary editing stays disabled.

### External Markdown handoff snapshots

In an owned live session, use the server address shown by the native viewer with
`track markdown --addr 127.0.0.1:PORT -- "file.md"`, replacing `PORT` with that
address's port. The CLI snapshots the file's containing folder and prints a
one-use viewer URL; it neither starts the server nor opens a browser. Copy that
URL, choose **Paste handoff**, then explicitly confirm **Open one-time snapshot**.
Only the current owned server's URL is accepted. The token is not shown in the
confirmation or saved in preferences.

Opening consumes the handoff before its response arrives. It expires after one
minute, and a failed or interrupted open may already have consumed it. Generate
a new handoff instead of retrying the old URL. Pasting or canceling before Open
does not consume it. A failed replacement keeps the previously opened snapshot.

The external viewer is a separate, ephemeral read-only snapshot. Choose among its
Markdown files using paged filenames; selection makes no filesystem or network
request. Close returns to the unchanged vault reader. Links, images, nested
includes, tasks, Find, reading checkpoints and editing are unavailable here.
The snapshot is not imported, saved, automatically refreshed or restored after
restart. Generate a fresh handoff to reflect source-folder changes.

Malformed paths, duplicate names, invalid UTF-8/base64 and excessive snapshots
are rejected rather than opened from the filesystem. The limits are 5000 files,
24 MiB wire JSON, 16 MiB decoded content and 2 MiB per Markdown file, with further
native parsing bounds. Image bytes are validated for size but not rendered.
Native clipboard, focus, keyboard/IME and accessibility QA remains pending.

## Read-only tag pages

Click an accepted live note tag to open a dedicated page, not a search. It lists
launch-vault notes with that exact tag or descendants, case-insensitively, in
engine order. Partial/cached responses remain qualified; results are virtualized
without a search-result cap. The page participates in Back/Forward and Reload
but is not a tab. Existing tabs remain available; Cmd-W on the tag page does
nothing. Fixture sessions may retain bounded tag history while still restoring
to safe Home. Ordinary persistence and editing remain disabled. Offline tag
listing and native layout/interaction acceptance remain incomplete.


## Fixture-only title creation (experimental)

With the existing explicit `--developer-edit-fixture` owned-server launch, **New**
opens a title-only dialog, including in an empty fixture. The launch fixture's
**default template** supplies the body; no blank-body promise or follow-up PUT.
Dirty drafts use Save/Discard/Cancel. Success shows a receipt; **Open created note**
verifies fresh source before opening Preview. Ordinary/named vaults and snapshots
cannot create. Root/config/cache/marker changes or symlinks in the controlled
fixture refuse the request.

A bounded receipt in the isolated cache is synced before POST. Uncertain results
never retry. After the request finishes, **Keep unresolved receipt and close**
verifies durable retention before allowing normal guarded quit. The receipt
reappears after restart and still blocks further creation; acknowledging it is
not success, rollback or permission to retry. No extra session/draft-persistence
flag is needed, and ordinary launches never touch this receipt. See
[PARITY.md](PARITY.md) for scope and [NATIVE-QA.md](NATIVE-QA.md) for required checks.
A bounded native creation smoke passed; actual IME and interrupted creation QA
remain open. Normal-vault editing stays disabled.

The durable-receipt increment passes 642 native tests (7 ignored) and formatting;
its final build and receipt-specific GUI checks are not yet confirmed.

### Native typed property links

Properties with the exact `link` type can open a verified internal note, including
a supported heading fragment. Values remain literal labels; repeated values keep
their order. Only `up` properties of type `link` are hidden; plain `up` stays visible.
Pending, missing, ambiguous or failed targets remain readable but inactive. URI-like
values are internal lookups, never browser/file opens. Property-only targets share
the reader's existing 128-distinct-target lookup limit. Unsupported fragments stay
inactive. No hover preview, automatic title replacement, full metadata styling
parity or block-fragment navigation is provided. No note writes are added.

### Native journal: On this day

An accepted daily journal shows **On this day** in the reader aside: notes worked
on that exact journal date in the journal's own vault. Year/month journals and
ordinary notes do not show this section. It preserves the engine's order, omits
the source journal, and opens results through normal guarded navigation; Back
returns to the journal. It does not change Home's selected day or display zone.

Empty results, incomplete/cached results and unavailable reads are distinct.
Use Retry after a failed or busy read. At most 100 notes are shown with an explicit
count if more exist; there are no additional pages. This is not an anniversary,
task/event view or NEW/read-status inference. It only reads existing data and
never creates or edits a journal. Native GUI/IME/keyboard validation is pending.

### Native reader j/k scrolling

Single-click a plain, noninteractive paragraph in the preview-only reader, then
use lowercase **j** to scroll down or **k** to scroll up by three body lines. Key
repeat is supported and movement stops at the edges. **Escape** releases this
ownership; click eligible prose again to resume. Modified/uppercase keys keep
their existing behavior. Links, code, tables, images, selection-style drag or
multiple clicks do not acquire these keys. Editor/split, Find/search, composition,
menus/dialogs, graph/preview/handoff and other input owners block this shortcut.
The current plain text elements have no selectable-text/input-handler model; no
global keyboard or IME handler is added. Native GUI/IME verification is pending.

### Native View history controls

Use **View → Back / Forward** or **⌘[ / ⌘]** in the active workspace, including
Home, tag pages and full graph. Unavailable history directions are disabled.
Existing dialogs, composition and save/recovery guards still apply; a dirty note
uses the normal navigation decision. About and background windows do not route
these commands to another workspace. Reader focus is not required. Reload is not
part of this change. Native pointer menu checks passed; keyboard/IME coverage remains incomplete.

## Enlarge decoded reader images

Successfully decoded standalone local PNG/JPEG images have an explicit Enlarge control. The window-local overlay reuses the same decoded pixels, fits without upscaling, and closes with Escape, backdrop or Close. It adds no read, decode, history, clipboard or write operation. Competing input/recovery owners block admission; stale sources close the overlay. Native PNG fit and pointer dismissal, bottom-position retention and Quit were checked. JPEG, resize, keyboard/accessibility and IME validation remain incomplete.

## Copy current title

The accepted live reader offers Copy title beside its wrapped title. It submits the exact plain-text title, without wiki syntax or note identifiers. “Copy submitted” lasts about 1.5 seconds and renews on repeat; this reports clipboard submission, not readback verification. Empty or over-64-KiB titles and pending/error/competing-owner states have no active control. Preview copying and a Cmd-C binding are not added.

## Authored reader block links

Accepted live paragraph/list prose supports a terminal whitespace-delimited `^ID` (ASCII letter/digit first, then letters/digits/hyphen/underscore). The marker is hidden and `[[Title#^ID]]` or verified metadata links reveal the first exact, case-sensitive match. Missing targets report no jump. Bare markers, quotes, nonterminal/invalid markers, code, tables and other unsupported shapes retain their source. Snapshot and inert preview parsing are unchanged; no transclusion or writes.

This subset reveals at the reader top within available scroll extent. A successful
block jump briefly paints the destination background (about 1.5 seconds), separate
from Find; source changes, newer jumps and competing owners clear the cue.
Arrival-cue native validation is pending. Centered placement remains unavailable. Markers whose removal
would truncate protected link/code text remain literal and are not anchors.

## Native Find menu

Edit → Find provides Find… (Cmd-F), Find Next (Cmd-G), and Find Previous (Shift-Cmd-G) for the accepted reader. Find selects the retained query without advancing when already open; Next/Previous reuse the existing wrapping behavior and need a nonblank query, including after closing the bar. Commands are inactive for other panes, inputs, composition, modal/recovery/quit owners and other windows. There is no global text-search or Cmd-C change. Disposable native menu/accelerator and query-retention smoke is recorded in NATIVE-QA.md; full IME and interruption coverage remains pending. Closing the bar resets the next search step to the first/last result through the existing reader behavior.
