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

Install Nix, Rust 1.99 or newer and Apple's Command Line Tools, then run:

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

## Pinned dependency preparation

`prepare.sh` uses the repository's locked nixpkgs to fetch the official GPUI
and xattr crates by their archive SHA-256 and apply small
[local patches](patches/README.md). The source
is generated in the Nix store and linked under ignored `.build-deps/`; no full
upstream source is vendored. Cargo requires that path and cannot silently fall back
to unpatched GPUI or xattr. Re-run preparation after changing a patch or project lock.
The xattr patch preserves Linux/Android missing-attribute semantics with current
libc; it does not disable tar attributes or downgrade libc.

The GPUI patch exposes a native application-quit request before AppKit termination,
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

Fixture snapshots read schemas 1–5 (tabs, history, scope, reading positions and
tag history as available), with an empty recent-note list for those versions.
The fixture writer saves schema 6 after an accepted metadata or checkpoint change;
startup alone never migrates a file. Schema 6 adds up to 100 distinct vault-qualified
recent note identities and cached labels, independent of open tabs. Legacy snapshots
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

The live reader displays references to unique footnote definitions as `[1]`,
`[2]`, and so on, in first-reference order. Repeated references share a number;
the matching definition label uses that number too. Numbering follows displayed
block order and then table row/cell order, including references inside definitions.
Definitions stay at their source position. Unused, missing or duplicate definitions
retain their authored labels. Their formatted content and ordinary links remain
readable and searchable. Find uses the displayed labels; Copy MD keeps the original
source. Snapshots and included-note previews retain their original labels.

Numbering is limited to 256 definitions and 256 reference occurrences per note.
Larger or malformed display snapshots retain all original labels rather than
showing a partially numbered result.

In the current live reader, click a reference to jump to its unique definition.
Undefined or duplicate labels stay inactive. The jump is local: it does not reload
the note, open another tab or write anything. Changed source and blocked reader
interactions cancel stale clicks. After a jump, the small **Return to reference**
control restores the position where you clicked, including repeated and table
references. It is one-use and session-only, with no history or saved state.
Changing the note, reader mode, layout or competing reader action clears it.

This is not full web footnote parity: definitions are not collected at the end,
and automatic definition backlinks are not added. Unresolved
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
explicit source Copy control. Rendered content additionally supports the bounded single-paragraph displayed-text selection described below; code/source Copy remains separate.

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
request. Close returns to the unchanged vault reader. Bounded same-snapshot Markdown file links are supported; external/wiki links, unsupported images, nested
includes, tasks, reading checkpoints and editing are unavailable here.
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

## Reader Hierarchy panel

The reader rail has a click-only Hierarchy panel for the explicitly labeled working-vault scope, not an implicit switch to a foreign note’s vault. Roots and immediate children appear first; roots cannot collapse, nonroot branches toggle, and titles open verified scoped notes. Close, Escape or backdrop dismisses without navigation. The panel shares Home’s bounded read, diagnostics and 200-row paging, but keeps separate state. Hover/grace interaction and retained expansion after closing are not implemented. Disposable native navigation and position-retention smoke is recorded in NATIVE-QA.md; full IME, interruption and accessibility validation remains pending.

## Copy MD

Copy MD beside the reader title submits portable Markdown from the exact accepted body, not the rendered view or draft. Wiki links become their nonempty alias or target title before `#`; wholly valid include lines become labels. Supported include options are `:only-contents` and `:lines` with positive single/range/comma-separated line numbers. Code bytes, ordinary Markdown links/images/HTML, reference definitions and conservative nonblank definition continuations, unknown include tails and midline includes remain literal, an intentional conservative difference from reference text-level conversion. Input/output over 256 KiB is refused without truncation. “Copy submitted” acknowledges API submission only, renewing for about 1.5 seconds. Native validation is pending; no Cmd-C override, clipboard readback, network/export or vault writes are added.

### Reader link previews (bounded candidate)
Verified internal body links can show one inert note preview after a 260 ms hover rest; leaving has a 220 ms grace period and entering the card holds it. Close dismisses it. Clicking the original link still uses normal guarded navigation, including fragments. External/unresolved links, local heading jumps, metadata, snapshots and included-note projections do not preview. Pinned graph and reader cards share the existing four-card limit and can coexist with a reader hover card. No focus, reader position, history, clipboard or vault changes are made by hovering. Graph and reader previews share two HTTP worker slots and bounded content; busy, failed and partial reads remain explicit. Cloud model checks are added; native hover/IME behavior is not yet verified.

### Native Reload
View → Reload (`Cmd-R`) reuses the rail's guarded reload for the active ready live reader or Home. It is unavailable in other routes, inputs, previews, competing owners or while the current list/body read is pending. A retained previously accepted reader can retry a recoverable read failure without adding a history visit. Snapshot/startup, Tag and Graph menu reload are outside this bounded slice; no automatic refresh or vault writes are added.

Native Reload validation: 730 Rust tests passed (7 ignored), formatting/native build passed. Disposable note/Home/history/search-menu smoke is recorded in NATIVE-QA.md; pending/failure/IME native cases remain unverified. Home requires a recorded Home reload route or safe-session Home; route-less initial Home without session opt-in remains disabled to avoid the existing first-note fallback.

### Paragraph selection
Drag across unstyled plain paragraphs, then use **Copy selection** beside the title. Crossing a paragraph boundary requires adjacent, fully visible measured paragraphs with exact source correspondence; otherwise the original single-paragraph behavior remains available. Forward/backward ranges use Unicode grapheme boundaries and do not invent line breaks at visual wraps. **Copy selected Markdown lines** separately copies touched original source lines, including interior blank lines and CRLF. Clipboard submission is explicit and capped at 256 KiB.

Escape/outside click clears selection. Unsupported or offscreen crossings, navigation, reflow and another owner cancel rather than guess. Non-link paragraphs with bold, italic or inline code also support single-paragraph displayed-text Copy, but not cross-paragraph extension or Markdown-line Copy. Link-containing paragraphs, headings, lists, tables, fenced code, media and previews are excluded; auto-scroll, keyboard extension, rich copy and accessibility selection remain unsupported. Earlier isolated native smoke covered only single-paragraph Japanese selection, reverse soft-wrapped highlighting, fixture clipboard, cancellation and link navigation. The adjacent-paragraph extension and styled single-paragraph selection still need native pointer/clipboard acceptance.

Live search-result `#tag` buttons refine the current query without opening a note or switching vaults. Exact duplicate terms are no-ops; appended queries normalize whitespace. Invalid tags (empty, whitespace/control-containing, over 4096 bytes) and queries over 64 KiB are refused without truncation. Snapshot tags remain inert because local substring filtering is not engine tag search. Native refinement validation is pending.

Result tags containing `%` or `_`, or ending in `/`, remain inert because the engine does not preserve literal tag matching for those forms.

Fixture Recent metadata uses the same protected snapshot, registry binding and
serialized worker, without a new path or ordinary-profile disk I/O. Restore does
not open/fetch notes or add visits; newer accepted navigation wins over delayed
startup restoration. Duplicate, malformed, unknown-vault or unknown-version data
disables persistence and preserves the original artifact rather than pruning it.
Labels are bounded to 512 characters / 2048 UTF-8 bytes. The unchanged 128 KiB
total snapshot cap can reject an otherwise individually valid list; a warning
is shown, with no truncation or replacement. Queued metadata is not guaranteed
durable on immediate/abrupt quit; no forced writer join has been added.

### Dedicated read-only Calendar

Choose **Calendar** in the rail for a Sunday-first month grid. Previous month,
Next month and Current month use civil dates; Current month recomputes the
current device-local date. Cell counts are **note activity only**, from one
working-vault month-range request, not task/event counts or a federated total.
Select any day, including a zero-count day, for bounded active-note, dated-task
and engine-projected event sections. This deliberate subset allows empty-activity
day selection because tasks/events can exist there. Missing counts in a partial
response remain unknown. Read-only note actions keep their exact source vault.

The page reuses the existing day-details display-zone control (UTC initially),
request limits and stale-response checks. It does not add month-wide note/task
snippets, journal creation, calendar editing, recurrence expansion or
external event links. Native layout/route QA for this addition remains pending.

Calendar is a normal non-note tab with Back/Forward, Close and guarded Reload.
Its displayed month, selected day and responses are memory-only. Protected
fixture snapshots containing the Calendar tab/history route use schema 7;
versions 1–6 remain readable, and older versions cannot contain Calendar routes.
Startup still restores Home without fetching Calendar. Ordinary launches add no
session writes. Existing metadata limits and abrupt-quit durability limitations
remain unchanged.

### Native Calendar month event previews

The Calendar grid reads one bounded engine-projected event range for the visible
month, independently of note activity. Each date shows its event count, the first
three event titles in engine order, a **Cancelled:** label when applicable, and
**+N events** for additional records. Titles are shortened in cells; select the
day for full metadata. Preview rows do not open links or edit events; the whole
cell continues to select that date. Deleted records are excluded by the engine.
Task and note previews are described below.

The month display-zone input starts at UTC and is session-only. Editing it hides
old previews as unknown; choose **Apply / retry month summaries** for a new range.
A selected day's existing details retain their own response zone until their
separate Apply action or a new day selection. Loading, invalid zones, missing API,
read failures and incomplete responses are unknown, never zero events. Zero is
shown only for an accepted complete date bucket. Note-activity failure does not
prevent event previews or day selection; its counts remain unknown.

The native adapter requires all dates in order, at most 42 inclusive days,
maximum three previews per day, bounded identities/titles and at most 4096 source
events. It shares the existing two-request permit with day/weekly work, never
makes one request per month cell and does not compute event timezone membership
locally. Month, zone revision, scope, registry, request, launch and storage changes
retire prior results. No vault writes, new session fields or external calls are
added. Isolated native QA verified host event counts, cancelled labels, overflow,
zone-change invalidation and day details. Cold foreign-scope loading and switching back to the launch vault were
verified after separating canonical reads from peer index startup. Real IME and
native DST transitions remain unverified.

### Native Calendar month tasks and notes

Each month cell now shows events, dated tasks and active notes as separate
sections, each with up to three previews and its own **+N** count. Select the
cell for the existing read-only day details; snippets and overflow counts do not
open notes independently, and clicking a journal date does not auto-open or
create a journal. Task previews include completed states and use the engine's
scheduled/due membership (one entry when both dates match). Note previews follow
authoritative activity dates and engine ordering, not a guessed creation date.

Task due bars use **today returned by the engine in the selected display zone**,
not the selected cell or the computer's local date. Due today is a full bar;
seven days away is half, fourteen or more is empty, and overdue is full in the
warning color. Tasks without a due date have no bar. Completed tasks use the
same text and bar treatment. A syntactically valid but impossible due date is
preserved in the task record but does not draw a misleading bar. All labels are
read-only, shortened to fit cells;
the engine additionally bounds each preview label to 256 UTF-8 bytes. Day details
remain the route to fuller information.

Choose **Apply / retry month summaries** after a display-zone edit or an unavailable
index. The task/note summary only reads an already verified index: cold, partial,
changed or failed index reads show **unknown**, not zero, and never silently retry.
Event and task/note errors are independent. Initial loading runs note activity
before the summary; Apply runs the event range before the summary, within the
same shared two-request bound. Month, source, registry, storage and zone changes
reject older results. Civil task/note date membership is not timezone-rebucketed;
the zone supplies the due-bar's current date.

All sections deliberately use the selected working vault (empty scope means the
launch vault), rather than reproducing inconsistent cross-vault filtering in the
web Calendar. This does not change the web implementation. Month summaries,
selection and zones are not persisted; ordinary-vault write and session gates
remain unchanged. Isolated native validation covered dense month layout, independent overflow,
scope switching, zone-edit invalidation, retry and day details. Interrupted
requests, native deadline-boundary timing and real IME remain unverified; this
does not establish full Calendar parity.

## Math renderer notices

The optional display-math action uses pinned `latex-rust` 3.0.3 (MIT OR Apache-2.0)
with its embedded STIX Two Math font. The font is licensed under SIL OFL 1.1;
redistributions must retain [the bundled font license](licenses/STIX-Two-Math-OFL.txt).
No system font lookup, TeX installation or external rendering service is used.

#### Selected source-reference Copy (experimental GPUI)

For an exact released plain-paragraph selection, **Copy source reference**
uses the accepted note response's `copy_path` and the same original touched
lines as **Copy selected Markdown lines**: `path:N` or `path:N-M`.
Forward/reverse selections, CRLF, Unicode and intervening blank lines share the
existing source-map rules. An endpoint at the next line's start excludes that
line. No path guessing, new request or filesystem write is performed.
Missing/invalid paths and styled or stale selections disable the action.
This is exact selected-source-line copying, not whole-block reference parity.
Cloud model tests do not establish native clipboard/pointer validation; see the
[experimental native workspace guide](../../docs/help/note/1785024017000.md).

#### Explicit Open tasks (read-only)

Home's **Open tasks** disclosure uses one explicit scoped `GET /api/tasks?open=1`
and preserves engine ordering/eligibility and duplicate rows. An empty scope
means launch vault. Native acceptance is limited to 16 MiB / 4096 rows, with local
100-row paging, shared agenda concurrency and a separate single-inflight permit.
No per-row fetch or paging-request loop is introduced. Source-note opening uses
the existing guarded vault-qualified route. Scope, storage, launch, request,
reload and page revisions protect asynchronous completion and row callbacks.

Loading, complete-empty, partial/cached and failure states stay distinct; Retry is
explicit. Unknown task states and malformed/oversize responses fail closed.
The existing server still materializes all matching indexed tasks and may refresh
derived index/cache; client bounds do not imply bounded server work. No task/note
mutation API or ordinary-vault write feature is added. Native GUI remains
unverified; cloud model/adapter tests cover only the stated contracts.

### Calendar period agendas (experimental)

Calendar now has Month / Today / Tomorrow / This week controls. Day/week navigation is civil ±1/±7, Sunday-first, with local All/Events/Tasks filters and 100-display-row pages. Month and its selected-day/journal behavior are unchanged. Task source navigation stays read-only and guarded; no event/task editing is introduced.

The engine-selected display zone determines today via at most three existing day-boundary reads of UTC today and its neighbors, with `start <= captured_now < end`. At most seven visible day reads follow, reusing a matching today response; tasks are fetched once. One serial period batch shares the existing agenda permit limit. Cancellation stops subsequent requests, never releases a running worker's permit early. A 40-second between-request deadline supplements the existing HTTP timeout; it is not a hard wall-clock kill. The existing 16 MiB per-response limit remains, with separate accepted 4 MiB/4096-record event and task aggregates. No new engine API or dependency is added, and existing server-side materialization/index refresh is not claimed bounded or write-free.

Events retain engine day membership and original timestamp/zone/all-day labels. Tasks reuse weekly civil-date placement, stable ordering and overdue rules, with invalid-date diagnostics. Failures/partial results stay independent after the today anchor is verified; anchor failure makes the whole period unavailable. Relative view refreshes re-anchor today, while explicit historical navigation remains fixed. An owned timer checks midnight/clock changes approximately every 30 seconds; focus checks also invalidate expired data. Unapplied zone changes and route/source changes discard results. Sunday is fixed, and returning to a retained Calendar may require Reload. Ordinary vault write gates are unchanged. Full GPUI compilation and native GUI/IME behavior are not verified by the isolated cloud model/adapter harness.

### Search match details

Live result rows project the existing engine `match` values (`title`, `body`, `path`) into reason labels and show the optional `snippet` as inert plain text. No Markdown/HTML/link parser, extra request, grouping or reorder is introduced. The same accepted result supplies title, tags and excerpt; there is no independent metadata cache or navigation authority. Existing query-generation guards and keyboard result indices are unchanged. Missing/unknown optional details retain the result. Display excerpts are limited to 160 Unicode scalar values plus ellipsis, with controls replaced by spaces; live rows reserve separate fixed-height single lines for title, tags, reason and excerpt; each may clip horizontally on narrow windows. Snapshot search remains unchanged. Cloud source-model tests are not a full GPUI/native layout validation.

### Home Recently created notes

An explicit Home disclosure reads `/api/notes?sort=created&limit=100` for the selected working vault. It preserves engine order (regular notes, creation ID descending), unlike the independent app Recent visits list. Missing titles display the numeric ID; no updated/read-state inference or eager source fetch occurs. Raw row namespaces are verified against the requested scope/known launch alias before canonicalization. Responses and row callbacks are qualified by launch/storage/request/scope/registry and disclosure serial; Close, Reload, navigation and scope changes retire old authority. One in-flight listing shares the existing agenda read permit, retained until HTTP finishes. Loading, complete-empty, partial/cached and error remain distinct, with explicit Reload/Retry. Existing engine index refresh is unchanged; no mutation API, dependency or ordinary-vault editing is introduced. Cloud model/adapter tests do not constitute full GPUI/native validation.

### External snapshot local Markdown navigation

The consumed snapshot viewer can follow relative Markdown file links to exact existing `.md`/`.markdown` snapshot entries, plus viewer-local Back/Forward (100 successful file-and-heading selections). The host vault/history is untouched. Strict single percent-decoding and root-bounded path normalization reject absolute/scheme/backslash/control/query targets and malformed UTF-8; missing files are inert, never resolved through the filesystem, network, vault or OS. Link admission is computed once on accepted parse against at most 5000 snapshot paths, considering only the first 256 parsed link occurrences. Cached entries grant no independent authority: activation rechecks consume serial, selected-file revision, current file and exact parsed link; close/replacement invalidates them. Snapshot limits and one-consume/one-selection guards remain. Cloud tests do not establish full GPUI compilation or native interaction behavior.

### External snapshot PNG/JPEG images

Opening a consumed snapshot or selecting one of its Markdown files prepares standalone PNG/JPEG image references from that document's directory. For example, `![Diagram](../images/diagram.png)` can display only the exact image entry already contained in the accepted snapshot. No file, vault, HTTP, OS or external URL fallback is used. The original image text/alt stays visible, including when the image is missing, corrupt, unsupported or over a limit. Images fit the reader width without distortion; they do not offer Enlarge, animation, inline/table placement or linked-image interaction. SVG, HTML, data/file URLs, query strings, fragments and root-escaping paths are unsupported.

Only the first 16 standalone occurrences and at most 8 unique paths are considered per selected document. Duplicate paths share the same decoded resource. The existing 16 MiB total decoded snapshot-file payload limit remains; PNG/JPEG encoded bytes are now retained in memory. Rendering additionally limits each image to 4 MiB encoded bytes, 2048 pixels per edge and 4 million pixels, with a 16 MiB unique retained-pixel budget per prepared document. Previous displayed resources can coexist with a replacement job; decoder temporaries and GPU resources are not a hard process-memory bound.

A single process-wide image worker permit remains held until decoding ends. Decode happens off the UI thread; cancellation is checked between images, not a hard interruption of one decode. Busy preparation preserves the text and reports images unavailable; select the file again to retry. Completion is accepted only for the current snapshot, selected-file revision and viewer lifetime. Close releases displayed image resources and cancels pending presentation. No new request, dependency or mutation API is added. Cloud source/decoder tests do not establish full GPUI compilation or native layout behavior.

### External snapshot heading navigation

Snapshot Markdown links can target `#heading` in the current file or `../guide.md#heading` in another existing snapshot Markdown file. Japanese fragments such as `#%E6%97%A5%E6%9C%AC%E8%AA%9E` are decoded once. Path and fragment are separated before decoding; encoded `#` is never treated as a second delimiter. Empty fragments, multiple `#`, malformed UTF-8/escapes and overlong fragments are inert. Links without a fragment open the document at its top.

The viewer uses the existing **native** heading anchors, not a claim of browser slug compatibility: parsed ATX/Setext heading text is lowercased, letters/digits/underscore are retained, whitespace/hyphens become hyphens, and punctuation is omitted. Empty slugs use `section`. Duplicates and collisions receive the next available `-2`, `-3`, etc. suffix in document order; for example repeated `# Details` uses `details`, `details-2`. Code blocks and raw HTML do not create heading targets. Fragment matching is exact after decoding, with no fuzzy title search or host-note resolver.

A missing target heading reports **Heading not reached** and leaves the previous file, images and local history unchanged rather than silently opening the top. Repeating the same valid heading link jumps again. Viewer Back/Forward remembers file plus heading, bounded to 100 entries; selecting a file in the list means its top. Parsing, bounded image preparation and target validation finish before the fresh scroll handle is published. Old completions after file selection, replacement or Close cannot move a new document. No filesystem/network lookup, vault navigation or mutation is added. Full GPUI compilation and native scrolling remain unverified; cloud tests cover the stated model and lifecycle boundaries.

### Pin reader link previews

A successfully loaded internal reader-link preview offers **Pin**. Pin retains that exact read-only body, title, partial status and diagnostics without moving reader focus, selection, scroll or history. Loading and failed reader cards cannot be pinned. Pins survive pointer departure and ordinary note navigation/Back; Open as page uses normal guarded note navigation, and Close removes only that card. Existing card placement, selector, move/resize and explicit Unpin controls are reused; preview links, edits, media and persistence remain unavailable.

Graph and reader pins share a four-card ceiling. New pins at capacity report the limit and never evict another card. Explicit duplicate pin intents retain the existing vault-qualified identity and raise it; different vaults with the same numeric ID remain distinct. Hovering an already-pinned target retains the existing graph behavior: no duplicate card or fetch is created. Use that card's selector/title control to bring it forward; merely hovering does not change its stacking order.

Reader pins are frozen accepted snapshots: hover timers and late completions cannot replace their content. Launch/storage, working-scope revision, verified available registry and session permission changes retire their authority, with checks at presentation and action time. They are not live-synchronized or restored across restart. Cloud model tests cover lifecycle and ownership, not full GPUI compilation or native pointer/keyboard/IME behavior.

### Find inside an external snapshot

Choose **Find** in the external snapshot viewer to search only the current displayed document. Matching is literal, case-insensitive and whitespace-flexible, across inline decoration boundaries but never across independent paragraphs or table cells. Headings, code and table cell text are included. The file list, viewer chrome, image pixels/OCR and undisplayed source are not searched. Original image captions and any literal fallback text actually displayed remain searchable.

**Next / Previous** wrap through matches; Enter / Shift-Enter work when the dedicated Find input has focus. Highlights use the same rendered block/cell text, and a guarded after-layout reveal brings the active match's first laid-out line into view. Close Find or Escape removes highlights; Escape closes Find before the viewer. Input composition blocks step/close, and viewer Find actions do not operate the host reader's query or history. Host Cmd-F/G support is unchanged; use the explicit viewer Find control.

The query is retained across file changes, but matches and native geometry are recomputed for the newly accepted file. Passive re-evaluation does not override file/heading navigation's scroll intent. Pending file selection or snapshot replacement pauses Find and invalidates old reveal tickets; failed replacement can resume searching the retained document with fresh ownership. Queries above 4096 bytes are rejected for evaluation, and at most 10,000 matches are retained with an explicit limit indicator. Search does not add filesystem/network requests or vault writes. Full GPUI compilation and native Find/scroll/input/IME behavior remain unverified; isolated cloud tests cover the model and adapter boundaries only.

### Rendered-preview Find

The explicit **Find in preview** control searches only ordinary displayed host text and successfully expanded include bodies. It reuses the rendered-text literal/case-insensitive/whitespace-flexible matcher, with separate block/cell and include-occurrence addresses. Query cards, task boards, diagram/chart internals and their visible source disclosures, metadata/chrome, image pixels and hidden/unexpanded source are excluded. Displayed image alt text remains searchable. Existing partial warnings remain visible; no matches is not a completeness claim.

Next/Previous wrap, Enter/Shift-Enter act in the dedicated input, and Escape closes Find before the preview. Composition blocks step/close. Highlights and guarded after-layout reveal use per-slice geometry, never host source indices, Find state, reading positions or history. Source/refresh/close/scope/page changes invalidate old callbacks and geometry. Host Cmd-F/G behavior is unchanged.

Bounds: 4 KiB query, first 8,192 eligible blocks / 2 MiB displayed text and 10,000 matches, with a visible limited indicator. Text bounds do not claim hard RSS bounds. There are no additional requests, dependencies or vault mutations. Cloud actual-source model/guard tests use lightweight Markdown data types; full GPUI/native input/layout/scroll remain unverified.

### Session-only rendered reading

**Use rendered reading for this session** opts accepted live notes into the existing read-only rendered preview. A memory-only preference survives note navigation; an exact accepted-owner ticket allows one automatic engine render attempt per acceptance, not one request per frame. Busy/error requires explicit **Reload / Retry rendered content**. This re-resolves accepted host text, not a fresh host-note GET; ordinary Reload is unchanged. There is no SSE retry loop or new API/dependency.

The preview header provides guarded Back/Forward and Source/off controls; existing query/include Open actions retain the mode for the next note. Closing the view turns it off. Other navigation remains available after Source. This is a limited preview-based reading path, not general-link/editing/selection parity. Home/Calendar/Graph are unchanged, and no mode or rendered scroll checkpoint is persisted.

Same-source refresh can retain accepted content with an explicit warning only when body/request/task state/scope/registry/storage match. Source changes and off-on retire old workers/controls. Dirty/recovery history vetoes precede preview retirement; temporary modal/IME ownership never authorizes automatic focus theft. Existing single-worker and resource bounds remain. Actual-source lifecycle tests use lightweight identity stubs; full GPUI/native focus, dialog and navigation verification remains unavailable in this cloud-only workflow.

In this first slice, rendered-header Back/Forward supports adjacent note and Home history entries only. For a Tag, Calendar or Graph destination it displays “return to Source” instead of activating an unsupported transition. Source mode retains the ordinary navigation guards and full existing history controls.

### Open ordinary host links in Rendered content

In **Rendered content (read-only)**, ordinary host prose offers explicit **Open host link** controls. These resolve the original wiki key or a supported Markdown destination in the host note's vault, not the displayed label or currently selected working vault. Existing included-excerpt controls continue to use the verified included note's vault. Query-row Open controls are separate. Opening succeeds through the ordinary protected note-navigation path; it does not edit a note or create a missing target.

Host controls support wiki links without a heading/block fragment and the existing conservative Markdown subset: simple relative note titles, optional literal `.md`/`.MD` suffix, and valid percent-encoded Unicode. Paths, external URLs, assets, actions, malformed encoding and Markdown query/fragment destinations are not admitted. Except for the explicit same-host Markdown heading controls described below, host heading/block fragments remain explicitly unavailable rather than being silently dropped; included wiki heading/block navigation retains its existing behavior. A literal wiki title ending in `#`, such as `C#`, is still a title, not an empty fragment.

Only ordinary host paragraph links are eligible, not table cells, images, query/diagram internals or metadata controls. The preview offers at most 16 controls/statuses from the first 256 parsed links and 8,192 host blocks. This is a bounded prefix, not a complete link inventory; return to **Source** to inspect the remaining links. Inline text itself is not made clickable. Existing Find, source text and source-copy behavior are unchanged.

Lookups run only on explicit activation and share the existing bounded preview navigation worker. A newer click while that worker is busy cancels the older navigation intent and asks you to retry after the worker finishes; it does not start another worker. Missing targets, engine-reported ambiguity/errors, uncertain results and storage changes retain the current preview with a notice. Ordinary duplicate-title resolution still follows the existing engine resolver; this feature does not add a title-collision scan. Refresh, source/request changes, mode off/on, Close, scope/registry or storage changes reject old controls and completions. No new API, dependency, external opener or canonical note mutation is added; existing engine read/index behavior remains.

Cloud tests exercise extracted production admission, wiki parsing, normalization, resolver namespace and stale-ticket logic with lightweight model/fake-client types. Full GPUI compilation and native focus, layout and navigation remain unverified.

### Copy selected text in Rendered content

In **Rendered content (read-only)**, drag within one fully visible, non-link prose paragraph and choose **Copy selected text**. This copies only the selected displayed characters: no Markdown decoration, destination URL, vault path or citation is added. Bold, emphasis and inline code can be crossed within the paragraph. Forward and reverse selections use Unicode grapheme boundaries; soft wrapping does not insert extra line breaks.

This first slice is single-paragraph only. It does not extend across paragraphs or include boundaries, and excludes link-containing paragraphs, headings, lists, quotes, tables, images, query/diagram internals and source disclosures. Repeated included excerpts have independent selection identities. At most 256 rendered slices admit selection. A selected payload above 256 KiB is rejected, never silently truncated. Existing code/source Copy controls, source-reader selection and clipboard shortcuts are unchanged; use the explicit preview button rather than relying on Cmd-C.

Scrolling, reflow, source/refresh/navigation changes, mode off/on, scope/storage changes, Find or a competing preview control invalidate the selection or reject its old Copy action. Dragging outside the measured paragraph cancels rather than guessing a nearest character; there is no auto-scroll or keyboard extension. An unchanged layout can repaint without canceling a drag. Selection and copying never alter host reading positions, history or note contents, and perform no extra read request. **Copy submitted** means the GPUI clipboard API was called, not that pasting into another application was verified.

Cloud tests cover extracted production eligibility, grapheme selection, owner/occurrence retirement, geometry checks and payload limits using lightweight UI/model stubs. Full GPUI compilation, native soft-wrap/pointer event ordering and clipboard paste behavior remain unverified.

### Host Contents and local heading links in Rendered content

Choose **Show Contents** in **Rendered content (read-only)** to see accepted rendered host headings in document order, indented by heading level. Included excerpts do not contribute entries or consume duplicate-heading anchors. Choose an entry to reveal that exact host occurrence inside the preview. **Hide Contents** collapses the list; opening or closing it cancels an unfinished jump. The list is initially closed and is capped at a 144-pixel scrollable area when open.

These are native anchors for the accepted rendered host, not a claim of identical Web or authored-source anchors. Letter/digit/underscore characters are retained, punctuation is omitted, and whitespace/hyphens become separators. Collisions use the existing native `-2`, `-3` suffix rules across all host sections: headings `A-2`, `A`, `A` produce `a-2`, `a`, `a-3`. Includes do not change that sequence. Contents still works when rendered host headings differ from the original note.

Ordinary host prose also offers explicit **Go to host heading** for Markdown destinations beginning with `#`, such as `[日本語](#%E6%97%A5%E6%9C%AC%E8%AA%9E)`. The fragment is decoded once and must exactly match a native anchor. This extra link path is enabled only when the original host and accepted rendered host have matching complete heading title/level/anchor sequences. Missing/invalid fragments or a different sequence show **Host heading not reached**, not a successful jump to the top. Empty fragments, other-file fragments and named wiki heading targets remain unsupported here; existing included wiki controls are unchanged. No lookup request is added to prove a same-title note is the current note.

Reselecting an entry can jump again. Empty headings or unavailable current measurements produce a not-reached notice. Contents and preview Find do not compete for a reveal; close Find before using Contents. A heading jump clears preview selection, and scroll, reflow, another control, refresh, source/scope/storage changes or mode off/on cancel stale pending reveals. The fixed-height notice row does not resize the reader when a jump starts or finishes. Only the preview scroll offset changes: host source checkpoints and note/Back/Forward history are untouched.

At most 8,192 host blocks, 256 headings and 2 MiB of heading text are admitted. Exceeding a bound disables the whole Contents model rather than assigning anchors to an incomplete prefix. Existing host-link controls retain their separate bounded-prefix limits. No extra fetching, filesystem access, dependency or note mutation is introduced. Cloud regression tests exercise extracted production identity/anchor models and the real pulldown-cmark heading events through a small harness adapter; full GPUI parser integration, native layout/reveal and pointer behavior remain unverified.

### Conservative current-note auto refresh

The session-only **Auto refresh current note** control checks the accepted vault-qualified note on engine change/gap hints, not event payload identities. It uses the existing 250 ms lifecycle poll, a 500 ms quiet debounce, a two-second start interval, and one physical worker across disable/re-enable. Reads use existing byte/time limits and storage checks before/after, not an atomic snapshot. A new accepted note gets one check while enabled; errors wait for a new event or explicit Retry.

Full response equality preserves content/request/caches/scroll. Source metadata updates preserve scroll when body/tasks are identical; body/tasks changes require a measured top. Any changed Rendered snapshot also requires its measured top, otherwise the old accepted content stays visible with a notice to return to Source and use View > Reload to fetch the updated note. Rendered mode survives but changed source requires a fresh render. Dirty/edit/recovery/IME/Find/selection/modal/navigation/Editor Follow activity blocks replacement; interrupted requests cannot publish after the interaction. No navigation, history, recent-visit, or session checkpoint write is performed. Data/include/query-only dependencies are outside this limited refresh contract. Existing engine read-side index refresh remains unchanged; no canonical mutation endpoint is added.

Actual-source cloud model/adapter tests are separate from full GPUI/native UI verification, which remains unverified. See the English help section “Session-only current-note auto refresh” for the source Reload versus Rendered Reload distinction and failure behavior.

### Explicit browser links in accepted Rendered prose

Rendered host and successfully expanded include paragraphs offer separate **Open in browser** controls for explicit HTTP(S) Markdown links. The label and normalized destination are shown independently. Existing `external_links` validation and opener dispatch are reused unchanged; no inline activation, hover fetch, guessed relative URL, file/custom scheme, credential URL, query/diagram/code/raw-HTML/image link or source-disclosure interpretation is added. Browser dispatch itself does not change note navigation, history, scroll or canonical vault content.

The combined prefix is bounded to 16 controls / 256 parsed links / 8192 blocks; authored and normalized URLs must each be at most 4096 UTF-8 bytes. Exact preview/source/occurrence, scope/storage/registry, mode, page/source disclosure and Find/selection revisions are checked at activation, alongside current modal/editor/recovery/IME gates. Labels are bounded to 80 characters, while the normalized destination is shown. Source view remains available for links outside the prefix. HTTP(S) local destinations, query strings and fragments follow the existing ordinary-reader parser.

Cloud actual-source model tests use a fake opener and verify no unintended calls. Full native browser delivery and pointer interaction remain unverified; harness parser/model adapters are not a substitute for the full GPUI renderer.
