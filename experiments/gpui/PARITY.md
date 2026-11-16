# Native workspace parity

Target: the existing live Track.app workspace, not just the snapshot prototype.
The Go engine remains authoritative. Static-site generation and encrypted publishing
remain existing CLI/web capabilities; displaying embedded apps is still a desktop requirement.

Implementation and validation are separate gates. An unchecked cell is not a claim
of support. Use disposable fixture vaults, never a configured personal vault, for tests.

## Capability checklist

| Capability | Implemented | Parity validated | Reference |
| --- | :---: | :---: | --- |
| Synthetic/JSON snapshot list and metadata selection | [x] | [ ] | `experiments/gpui/src/main.rs` |
| Explicit read-only vault list and portable Markdown | [x] | [ ] | `src/backend.rs`; native QA with public help fixture |
| Owned HTTP read-only notes and federated search | [x] | [ ] | `live.rs`, `workspace_live.rs`; public-help search/source/quit smoke. Live result-tag refinement candidate reuses input/debounce; snapshots inert, native validation pending |
| Session-only working-vault scope selection | [x] | [ ] | `shell.rs`; existing tabs/Home/federated search remain unchanged |
| Persistent vault scope and workspace preferences | [ ] | [ ] | Fixture-only working scope implemented; ordinary persistence and other preferences remain open |
| Note body, tags, flags, hierarchy and backlinks | [ ] | [ ] | Accepted-source tags and typed read-only DEPRECATED/CONFIDENTIAL badges implemented; unknown flags remain literal neutral labels. Full visual/hierarchy parity and native narrow-label QA remain open. |
| Dedicated read-only launch-vault tag page | [x] | [ ] | `workspace_tags.rs`; literal hierarchical matching, virtualized full listing, page-only history and schema 5 fixture support; native navigation/history checked, narrow/theme layout pending |
| In-memory vault-qualified tabs and close/overflow navigation | [x] | [ ] | `tabs.rs`, `shell.rs`; model tests, native QA pending |
| Bounded in-memory Home/Graph/note Back/Forward | [x] | [ ] | `navigation.rs`, `shell.rs`; 100 routes, dirty-gated tickets and fixture lifecycle tests; native shortcuts/IME pending |
| Persistent tabs, history and floating previews | [ ] | [ ] | `components/tabs`, `components/preview` |
| Read-only rendered body/table Find | [x] | [ ] | `reader_find.rs`, `workspace_find.rs`; local matches/highlights; shaped active-match line reveal added, native line-reveal/IME checks pending |
| Display of existing read/seen milestones | [x] | [ ] | `note_metadata.rs`; accepted live source only, no milestone writes or NEW inference; native QA pending |
| Explicit disposable-fixture reading checkpoints | [x] | [ ] | `session_store.rs`, `shell.rs`, `markdown.rs`; protected block checkpoints and fresh prepaint guards, no automatic or ordinary persistence; native QA pending |
| Reading positions, seen/read milestones and exact-range Find parity | [ ] | [ ] | `reading.ts`, `ReadingPositionBoundary`, `WebViewFindController` |
| Create, edit, save, delete and metadata editing | [ ] | [ ] | `NoteEditor`, `NoteMetaDialog`, `NoteActionsMenu` |
| Conflict detection, dirty-state protection and draft recovery | [ ] | [ ] | `NoteEditor.recovery`, `recovery.ts`, desktop termination tests |
| Attachments and draw.io editing | [ ] | [ ] | `DrawioEditorDialog`, `NoteEditor.drawio`, `/api/asset` |
| Basic Markdown blocks | [x] | [ ] | `src/markdown.rs`; headings/wrapping/scroll smoke, native thematic-rule contrast/spacing/AX pending |
| Simple tables and inline emphasis/code/link styling | [x] | [ ] | `markdown.rs`; code/emphasis native smoke, tables tests |
| Read-only plain task indicators and inline strike | [x] | [ ] | `markdown.rs`; parser/style/Find/link tests; native glyph/AX pending; rich task notation excluded |
| Ordinary code-fence caption and explicit Copy | [x] | [ ] | `code_copy.rs`, `markdown.rs`; parsed-body/lifecycle tests, native clipboard and accessibility pending |
| Explicit rendered-preview code/source Copy | [x] | [ ] | Snapshot-qualified, bounded plain-text clipboard submission; included snippets and retained input only; Unicode code clipboard round trip verified, general focus/AX pending |
| Bounded JS-family/Lua/JSON/Python/shell fenced-code lexical styles | [x] | [ ] | `code_highlight.rs`, `markdown.rs`; bounded lossless ranges, Find/Copy and ingestion cache tests; native GUI unvalidated |
| Full code syntax highlighting and math | [ ] | [ ] | `MarkdownView`, `components/markdown` |
| Verified internal wiki/Markdown links and outline jumps | [x] | [ ] | `markdown.rs`, `shell.rs`; scoped resolver and heading-fragment subset; same-note plain fragment native smoke; cross-note/dirty transitions pending |
| Unaliased explicit-ID wiki link title display | [x] | [ ] | `markdown.rs`, `shell.rs`; bounded verified resolver titles, Unicode/Find/anchor tests; native interaction pending |
| Explicit read-only included-note inspector and rendered preview | [x] | [ ] | `included_notes.rs`, `workspace_includes.rs`; owned render API, inert bounded snapshots and explicit in-reader standalone projection with verified host-source correspondence; bounded native display smoke passed, full transclusion and layout-race QA pending |
| Transclusion and line-copy links | [ ] | [ ] | `wikiLinks`, `toc`, `copyRange`, `NoteInclude` |
| Static native heading/list mindmaps | [x] | [ ] | Bounded node/edge diagram in rendered preview, same-source outline context; native visual/AX and interactive links pending |
| Diagram fences: Mermaid, D2, Graphviz, LikeC4, draw.io, Pele, mindmap | [ ] | [ ] | `components/markdown/*Diagram*` |
| Engine-resolved query layout snapshot reading | [x] | [ ] | Bounded inert blocks in explicit rendered preview; other layouts/navigation/native QA pending |
| Embedded host-note read-only task boards | [x] | [ ] | Exact accepted task metadata, bounded lanes/cards; included sources and mutation unavailable, native QA pending |
| Static categorical line and grouped-bar chart subset | [x] | [ ] | Bounded native geometry in rendered previews; unsupported semantics retain source, native visual/AX QA pending |
| Inline metric ViewSpec chart resolution | [x] | [ ] | Owned bounded read-only resolution, strict inline admission and chart fallback; file-backed specs and native QA pending |
| Charts, ViewSpec, metrics, query views and task boards | [ ] | [ ] | `EChartsBlock`, `ViewSpecChart`, `MetricsDashboard`, `QueryView`, `TaskBoard` |
| Images, PDF, audio/video, maps, Grafana, OGP and embedded apps | [ ] | [ ] | `PdfDeck`, `MediaFrame`, `Embed`, `MapFence`, `GrafanaFrame` |
| Tasks, states, scheduled/due dates and calendar integration | [ ] | [ ] | `TasksView`, `TaskControls`, `calendarTasks` |
| Read-only named-zone day event listing in selected Home day | [x] | [ ] | `live.rs`, `workspace_activity.rs`; engine civil-day projection and independent all-day dates; explicit session-local IANA zone, device-following/persistence/native QA pending |
| Calendar, agenda, journals, event editing and time zones | [ ] | [ ] | `Calendar*`, `DayView`, `calendarEvents.ts` |
| CLI graph canvas, camera, selection and node dragging | [x] | [ ] | `src/graph*.rs`; help graph native smoke test |
| Full-vault Graph tab, scoped read, overview and title/reset controls | [x] | [ ] | `workspace_graph.rs`; synthetic wire/lifecycle tests; native initial/reload fit, pan/reset, title/open/Back checks; interrupted/large-graph QA pending |
| Read-only fullgraph hover and up to four pinned previews | [x] | [ ] | `graph_preview.rs`, `workspace_preview.rs`; synthetic lifecycle/read-boundary tests; one native Pin/Close checked, capacity/interrupted GUI pending |
| Focused full-graph keyboard selection and opening | [x] | [ ] | `graph_view.rs`; bounded deterministic selection, scoped focus and guarded navigation; native QA pending |
| Full graph layout, previews and navigation parity | [ ] | [ ] | `GraphCanvas`, `GraphFullView`, `graphWheel` |
| Themes, keyboard navigation, selection, clipboard and IME | [ ] | [ ] | `themeState`, `keybindings`, desktop reading shortcuts |
| Explicit one-use external Markdown snapshot viewer | [x] | [ ] | Owned-origin consume, bounded inert snapshots; native clipboard/focus QA pending |
| Explicit editor-follow note navigation and proportional scrolling | [x] | [ ] | Owned GET and event invalidation, verified scope, measured raw-reader geometry and user-action precedence; exact source alignment and native QA pending |
| External Markdown handoff and editor-follow behavior | [ ] | [ ] | `MarkdownViewer`, `markdownHandoff`, `/api/follow` |
| Read-only scoped Home activity heatmap | [x] | [ ] | `workspace_activity.rs`, `live.rs`; calendar/transport/identity tests; light/dark native rendering checked |
| Read-only owned SSE change/gap notices | [x] | [ ] | `live_events.rs`, `event_awareness.rs`; bounded synthetic transport/model tests; native startup-delay notice/dismiss checked, interrupted-stream GUI pending |
| Activity journal navigation, live refresh, partial reads and recovery | [ ] | [ ] | `ActivityPanel`, `reading.ts`, `useLiveEvents` |
| Explicit absolute HTTP(S) browser links | [x] | [ ] | `external_links.rs`; fake-opener/provenance tests, actual browser/keyboard/AX pending |
| Compile-time build identity and headless version display | [x] | [ ] | `build_info.rs`, `build.rs`; native diagnostics visual QA pending |
| Native menus and build information parity | [ ] | [ ] | `AppDelegate`, `BuildInfo`, `WebURLPolicy`; explicit Copy Build Info and bounded informational About implemented; native acceptance recorded separately |
| Native notifications and speech input | [ ] | [ ] | `NativeNotificationBridge`, `NativeSpeechBridge`, `VoiceView` |
| Owned server startup, restart and bounded shutdown | [ ] | [ ] | `TrackServerProcess`, `WebServerSupervisor` |

References without a path are under `web/src` or `desktop/Sources`. For every
completed row, record the fixture/test and native QA result in the corresponding PR.
Native text, selection and virtualized scrolling were verified on Apple Silicon /
macOS 27.2 after explicitly enabling `font-kit`. Full accessibility and IME parity
remain open; the original missing glyphs were a disabled font backend, not an OS fault.

## Developer-only editing status

A guarded disposable-fixture editor now connects the raw-body/etag transport to
Preview/Edit/Split and unified navigation/exit prompts. Ordinary use remains
read-only. It supports one draft, with separately gated fixture checkpoints, not full editor
parity. Independent source review and unit tests cover stale callbacks, save
locking, and transition guards. Disposable native checks cover app-menu quit,
Cancel, explicit Save, stale-source refusal, dirty Window Close/repeated Cmd-Q and Discard, with child-port cleanup.
Native uncertain acknowledgement and held-success Save-and-continue checks are also recorded.
Dock Quit and active IME remain open.
See `NATIVE-QA.md` for evidence and the README for the fixture-only opt-in.

Fixture-only recovery is separately opt-in. It connects protected checkpoints,
explicit same-source recovery and ordered save/discard clears to the existing
editor gates. Ordinary editing remains disabled. Filesystem/model and headless
integration tests do not establish native Dock Quit, IME or power-loss durability.
Separate native evidence covers crash recovery, Discard, lost acknowledgement and
held-success Save-and-continue; these do not imply complete editor acceptance.

## Reuse the engine boundary

- The initial read-only adapter executes explicit CLI commands on background workers;
  it is a transitional integration, not the editing/source contract.
- Use the existing HTTP API for live interaction; keep filesystem parsing, indexing,
  link resolution, metadata serialization and writes in Go. Snapshot import remains
  useful for isolated rendering tests, not as the live architecture.
- Read endpoints: `/api/vaults`, `/api/search`, `/api/notes`, `/api/note`,
  `/api/note/meta`, `/api/hierarchy`, `/api/resolve`, `/api/activity`.
- Mutation and planning endpoints: `/api/note`, `/api/note/meta`, `/api/asset`,
  `/api/tasks`, `/api/task`, `/api/agenda`, `/api/journal`, `/api/calendar-events`,
  `/api/calendar-event`.
- Rendering/interaction endpoints: `/api/render`, `/api/viewspec`,
  `/api/metrics-dashboard`, `/api/ogp`, `/api/graph`, `/api/graph/local`,
  `/api/follow`, `/api/markdown`, `/api/markdown/consume`, `/api/events`.
- **`/api/render` returns Markdown and resolved include/fence information, not HTML.**
  Native rendering still needs implementations of the web renderer's formats.
  Decide native rendering versus an isolated web surface per format; embedding the
  entire existing workspace is not proof of native feature parity.
- Treat note identifiers as opaque and scope them by vault. Preserve partial-read,
  unavailable-vault and stale-cache information rather than presenting an empty result.
- Keep HTTP and parsing off the UI thread. Reject stale responses after navigation
  or search changes. Reconnect SSE by refreshing affected data: missed events are
  not replayed. Existing events distinguish `change` from `data`.

## Safety gates

- [ ] Save with the server-provided `etag`; test conflicting external edits and keep
  the unsaved draft on failure. Send typed metadata fields rather than assembling YAML.
- [ ] Verify save/discard/cancel behavior for tab close, navigation, reload, restart,
  window close and application quit. Do not silently replace drafts after refresh.
- [ ] Own the Go child explicitly. Preserve the desktop parent PID/readiness-token
  protocol, wait for `TRACK_DESKTOP_READY:<token>` plus health, and reject port
  collisions. Never adopt or terminate a process merely because it owns a port/name.
- [ ] Stop only the owned child, with bounded escalation and verified reaping,
  including stopped or unresponsive children.
- [ ] Preserve workspace/static-app origin isolation. External links require an
  explicit user gesture; unsupported schemes and unrelated loopback embeds stay blocked.
- [ ] Check native text, Japanese IME composition, selection, clipboard, focus and
  accessibility before treating the editor as a replacement.
- [ ] Exercise malformed input, empty data, unavailable storage, partial reads,
  network interruption and reconnect without data loss or misleading success states.

## Delivery stages

1. **Rendering baseline:** fix font setup, test English/Japanese/emoji, selection,
   scrolling and resizing. Add a repeatable native smoke fixture.
2. **Live read-only slice:** explicit disposable vault → owned Go server → vault
   list/search → note body/backlinks. Validate request cancellation and partial states.
3. **Safe editing slice:** input → save → etag update → reload, plus concurrent edits,
   disconnected saves and all dirty-close paths. Add create/metadata/assets next.
4. **Reading workspace:** basic Markdown, links/includes, tabs, history, reading
   position and find. Track unsupported rich formats visibly rather than dropping them.
5. **Planning and visualization:** tasks, calendar/journals/events and graph interaction;
   then rich diagrams, charts and media with explicit sandbox boundaries.
6. **Native completion:** menus, shortcuts, notifications, speech, handoff, packaging
   and lifecycle behavior. Resolve remaining checklist gaps before replacement claims.

## Evidence required for completion

- Run the same disposable scenarios against Track.app and GPUI; compare navigation,
  visible content, saved bodies/sidecars, conflicts, recovery and shutdown outcomes.
- Reuse `docs/testing/desktop-clean-termination.md`, `desktop-editor-recovery.md`,
  `desktop-restart.md`, desktop smoke tests and existing web/Go regression fixtures.
- Record release-build frame time, memory and input latency for equal note counts,
  document content and window sizes. Continuous scroll, selection, resize and long-note
  editing are separate workloads. A virtualized list alone proves no editor speedup.
- Keep normal Track.app working throughout the migration. Publish coherent increments
  with explicit missing/unverified features; do not mark parity complete on compilation alone.

## Rich-content rendering boundary

Keep the workspace and graph native. For static diagrams, a constrained renderer
can produce sanitized SVG for GPUI images; HTML labels/`foreignObject` need explicit
handling. PDF page images can use PDFKit/Core Graphics, but text selection, search,
forms and accessibility are separate capabilities, not implied by displaying pages.

Interactive HTML tools may require a narrowly isolated WKWebView. GPUI 0.2.2 exposes
a native window handle, and Wry documents child-view hosting, but this pairing has
not been exercised. A child native view does not inherit GPUI clipping or scene
ordering. Before adoption, validate a disposable surface for scroll clipping,
Retina resizing, search-overlay occlusion, focus/IME restoration and teardown.
Use a nonpersistent WebKit data store, restricted origins/navigation/resources,
and minimal validated IPC; ephemeral storage alone is not a security boundary.
Do not embed the entire existing workspace and call that native parity.

References:
- [Wry child hosting](https://docs.rs/wry/latest/wry/struct.WebViewBuilder.html)
  (0.57.2 documentation inspected; not a validated GPUI dependency pairing).
- [WebKit website data stores](https://developer.apple.com/documentation/webkit/wkwebviewconfiguration/websitedatastore).
- [PDFKit page drawing](https://developer.apple.com/documentation/pdfkit/pdfpage/draw(with:to:)).

#### Fixture-only tab-session persistence checkpoint

Separate `--developer-session-fixture` opt-in now stores bounded vault-qualified
MRU tab metadata, title hints and up to 100 history routes under the isolated
temporary cache. Startup reads
before writes, restores the strip with safe Home rather than the previous active
route, and does not auto-fetch a restored note. Lock/revision/identity validation,
atomic writes, corruption-preserving memory-only degradation, and accepted versus
rejected dirty-navigation snapshots have synthetic coverage. Startup and writes
run off the UI thread; queued latest metadata is not guaranteed flushed on quit.
This is not durable drafts, note autosave, scroll restoration, Web storage
migration, or release-ready desktop recovery. Exact active-route parity and native
GUI relaunch/quit validation remain deferred. See README for opt-in and limits.

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

### Bounded graph-note comparison

The native graph supports four explicit pins plus one transient preview. A compact
vault/ID-qualified selector raises or closes one captured card without refetching.
Pin capacity retains all existing cards. Navigation and graph scope changes retain
pins; session or storage invalidation clears them. Headless tests cover multi-pin
identity, capacity, cache pruning and clamped placement. Native pointer delivery,
selector wrapping, z-order and dirty-dialog behavior remain unverified. Pin drag/resize
is implemented with model tests, but native, keyboard/AX and rich-preview parity
are not claimed.

### Graph paint submission bounds

Conservative viewport culling avoids building definitely invisible circle/edge
paint primitives. Crossing edges and padded border strokes remain; labels, picking,
camera and layout do not change. Pure geometry tests and CPU-only predicate counts
are verified. Native visual comparison and frame/GPU performance remain pending;
this does not complete full graph layout or navigation parity.

### Superseded graph calculations

Background settling now cooperatively cancels when graph data is replaced, nodes
are manually moved or the graph view is destroyed. No partial result is published,
and stale-generation rejection remains. Headless tests cover cancellation before,
during and after solving and independent replacement/drop tokens. Native
rapid-navigation and drag responsiveness remain unverified.

### Pinned preview geometry

Pinned titles start movement and a bottom frame strip starts resizing. Captured
read identities reject stale gesture callbacks; session-only rectangles are
clamped below the selector and inside the viewport. Raising/closing other pins
and navigation retain geometry; unpin resets it and invalidation clears it.
Mouse release, capture exit and window deactivation end gestures. Model tests
cover bounds and lifecycle, not native pointer delivery or keyboard/AX parity.

### Active Find match reveal

Find now records the active match's first line from the same prepainted native
text used for paragraphs, code and table cells. UTF-8 offsets include generated
list prefixes, with per-render geometry invalidation and existing request/revision
guards. One bounded retry covers unavailable geometry. This is vertical first-line
reveal, not whole-range or horizontal scrolling. Model tests pass; actual shaping,
resize-during-Find and native visibility checks remain pending.

### Fixture-only history restoration

Schema 2 stores validated route history and cursor atomically with tab metadata.
Legacy tabs-only snapshots remain readable; startup does not rewrite the snapshot.
Restoration always adds safe Home and truncates saved Forward, without an automatic
note fetch. Newer accepted navigation rejects both restored tabs and history.
History-only cursor changes persist even when tabs are unchanged. Ordinary mode
performs no session-file I/O. Native restart/Back/Forward and quit durability are
not established by the model/temporary-directory tests.

### Experimental pinned-preview keyboard controls

The pin selector has scoped keyboard entry and explicit title/Unpin/Open/Close
controls. F6/Shift-F6 is a native prototype convenience, not an established Track
shortcut or a claim of keyboard parity. The visible selector hint documents entry,
Tab traversal, arrow movement and Shift-arrow resizing. Reader Tab navigation is
unchanged; editing, IME composition, dialogs, search, menus and pointer geometry
block these actions. Captured read identity protects stale controls. Model tests
do not validate native focus delivery, macOS function-key handling or accessibility.

### Explicit build-information copy

The application menu and diagnostics expose Copy Build Info. Its plain-text payload
is exactly the same compile-time version, full revision, tracked-source state and
target as `--version`; it accepts no workspace state. No global shortcut or automatic
clipboard update is added. Payload tests pass without accessing the pasteboard.
Actual native clipboard delivery, accessibility and About-panel parity remain open.

### Python fence lexical hints

Python/py/python3 join the existing ingestion-time bounded code-token cache.
Comments, hard keywords and single/triple-quoted strings are shielded, while
prefixes, interpolations and soft/contextual keywords are not parsed. The source
remains lossless; existing Find/Copy/cache/size limits apply. Headless tests do not
establish native rendering, font contrast or accessibility parity.

### Fixture-only working-vault restoration

The bounded atomic tab/history snapshot also retains the selected working scope.
Restoration requires the same available registry and retains safe Home, leaving
note identities and federated search unchanged. Newer accepted startup navigation
or scope selection discards restoration. Legacy snapshots default to launch scope;
invalid scope metadata preserves the original artifact and disables persistence.
Normal invocation performs no session-file I/O. Model and disposable-file tests
are not native restart, IME, dirty-dialog or quit-durability validation.

### Owned event-stream awareness

The owned loopback observer reports change/data notifications and interruptions.
It does not replay event payloads or refresh any body automatically. Explicit
editor follow uses separate bounded current-state reads.
Notice dismissal acknowledges only captured launch-qualified counters; events
arriving before the click but after paint remain unacknowledged. Gaps stay sticky
across reconnect, and disconnection remains visible. Explicit Reload retains
existing dirty-navigation guards. Native notice layout/input and interruption
flows are unverified; this does not complete live synchronization or recovery.

### Shell fence lexical hints

Bash/sh/shell use bounded, source-preserving lexical hints, not a shell grammar.
Whole-word keywords are context-free; comments require a word boundary, quoted
text remains opaque, and heredoc content falls back to plain text. Zsh is not
implicitly treated as Bash. Copy, Find and token-cache bounds are unchanged.
Native contrast, selection, wrapping and accessibility remain unverified.

### Session-local reader position identity

Per-note scroll offsets survive tab/history navigation only while the accepted
content and source identity remain unchanged. A changed body, owned-server launch
or storage identity resets the retained offset instead of applying an old reading
position to new content. Loading placeholders do not overwrite the last accepted
identity. Explicit Find and heading navigation still control their own jumps.
This is not persisted reading position, seen/read tracking or native scroll QA.

### Read-only daily agenda

Selected-day Previous/Next controls use civil ±1 arithmetic within the current
activity range. They reuse guarded three-section reads and bounded concurrency;
old results cannot acquire a new date label. No empty-day skipping or new
out-of-range fetching is added. Date/range/stale/reset models are covered;
native controls, keyboard and IME remain unverified.


Home activity day selection fetches the existing engine's active-note agenda for
an explicit date and working-vault scope. Bounded results and partial diagnostics
are displayed; links use guarded note navigation. Request identity rejects stale
date/scope/launch/storage and superseded retry results. This adds no journal
creation, task-state/calendar-event editing or timezone reinterpretation. Native
interaction and visual parity remain pending, so G09 is not complete.

### Read-only dated tasks

The daily agenda now reads engine task states and displays exact scheduled/due
date matches, including completed/cancelled tasks, without client-side state
inference. Duplicate due/scheduled matches appear once. This adds no overdue,
recurrence, mutation or task-line navigation semantics. Transport bounds,
partial diagnostics and selection/source guards remain in force. Native task
list interaction and visual parity remain pending.

### Bounded local raster subset

The owned live current-note reader displays at most four standalone local
PNG/JPEG image paragraphs while preserving source captions. Lexical asset path
validation, scoped bounded reads, explicit raster-only decoding and source
identity guards precede GPUI rendering. Inline/table images, remote resources,
other formats and pinned previews remain unsupported. Asset filesystem symlink
policy remains the engine's responsibility, not a client containment guarantee.
Native rendering, color/orientation, reflow/scroll and accessibility remain open.

### Read-only footnote display

Parser-backed references and labeled definitions preserve readable nested content
and Find ranges. Definitions remain in source order, not web-style reference
numbering/end collection. Current-reader jumps to unique definitions
use dedicated parser targets and accepted-source guards. An explicit one-use
Return control retains the clicked scroll position only for the same source, tab,
reader identity and geometry; no route history, fetch or persistence is added.
Repeated/table-reference origins and stale/geometry rejection have model tests;
native middle/table Return, ordinary scrolling and note-navigation invalidation
were checked in an isolated fixture. Native reflow/keyboard QA remains pending.
Automatic backlinks are unsupported;
ordinary links inside definitions retain existing resolution rules. Native
layout, selection and accessibility validation remains pending.

### Read-only whole-vault hierarchy

An explicit Home disclosure reads the engine's scoped hierarchy, with local
branch expansion and guarded note navigation. Engine ordering and flags are
preserved; isolated/cyclic structures are not reconstructed. Bounds and
partial/error notices prevent an unavailable tree being treated as an empty
vault. No hierarchy mutation or drag reordering is implemented. Native
keyboard/layout/scroll/accessibility parity remains unverified.

### Current-note task inspection

The live reader displays a bounded, paged projection of task data from its accepted
note response, including undated and completed tasks in source order. No extra
request, parser reimplementation or task mutation is introduced. Source-qualified
controls reset across note revisions. This is not taskboard editing, line jumps
or native task interaction/accessibility validation.


## Fixture creation slice: bounded, not general creation parity

Implemented: explicit developer-fixture launch-only title dialog, empty-fixture
admission, default-template POST, strict receipt and fresh source verification,
explicit reader opening, single-flight pending ownership, dirty/recovery/IME
and competing-owner guards. No automatic retry, body override, template picker,
named-vault creation, voice-selection/pinned-preview parity or ordinary-vault writes.

Creation now durably records a bounded intent before POST using a dedicated
receipt lock/artifact and existing protected atomic-write primitives. Pending
requests block quit; after completion, explicit exact receipt verification/sync
can allow closing without deleting evidence or enabling another create. Restart
loads unresolved evidence before enabling New. Recorded Created identities remain
visible without automatically opening a note. A title match or absent result is
never used to infer success/noncreation. Actual uncertainty reconciliation and
safe permission for a later fresh create remain deferred.
Success with GET failure remains “created, unable to open/verify,” never a second
POST. Fixture bindings and linked output directories are checked before dispatch,
but this is a controlled disposable-fixture gate, not a filesystem sandbox against
concurrent hostile changes.

Model/transport tests and a bounded native first-note/duplicate/navigation smoke
passed, including real-engine default-template checks. Actual IME and interrupted
mutation GUI flows remain unverified; see [NATIVE-QA.md](NATIVE-QA.md).
No general new-note parity claim.

Durable-receipt increment: native macOS tests pass (642 passed, 7 ignored), and
formatting and native debug build pass. A seeded post-intent process-exit fixture
verified native receipt restore, blocked dismissal/quit, retained close, New
reopening without retry, normal quit and second-launch restore. This is not a
live lost-response POST or dirty/IME combination smoke; see NATIVE-QA.md.

### Typed property internal links

Typed projection preserves duplicate order and literal labels; only exact `link`
properties use the source-qualified bounded resolver, including metadata-only
targets. Only exact `up` + `link` is suppressed. Verified clicks recheck source,
revision and row, then use existing guarded navigation/heading flow. Malformed
metadata is reported, not turned into targets. URI-like values never externally
open. The 128-target bound remains shared with body links. Hover previews, full
metadata styling and block fragments remain unsupported. Native pointer checks cover metadata-only navigation, Back, duplicate ordering,
plain versus typed up and inert missing/plain URL values. Exact heading scrolling,
interrupted flows, layout, IME and keyboard/accessibility validation remain pending;
projection/lookup/stale-callback models are covered.

### Daily journal On this day

Accepted daily journal IDs supply the exact civil date; the journal's own vault
scopes the existing agenda GET. Independent reader state preserves engine order,
excludes qualified self, and rejects stale source/tab/revision/epoch/retry reads
and clicks. Home/day/zone state is untouched. Existing two-read concurrency and
4096-note transport bounds apply; the first 100 rows have explicit truncation.
Success-empty, partial/cache and errors differ, with bounded explicit retry.
No mutations, NEW inference, anniversary expansion, hover previews or pagination.
Eligibility/identity/order/empty/stale models are covered. Native daily list
result navigation/Back and ordinary/month/year exclusion were verified; named
vault, empty/partial failure, layout and interruption cases remain unverified.

### Focused plain-reader j/k

Explicit stationary unmodified single-click on noninteractive prose acquires
source/tab/content/geometry-bound reader keys. Lowercase unmodified j/k scroll
synchronously by three rendered body lines (24px/line fallback), with repeat and
edge clamps. Escape/other owners/source or layout changes revoke ownership;
interactive content never acquires it. No new requests, writes or persistence.
Selection-style gestures and marked-text owners are excluded rather than adding
a reader selection/input framework. Models cover commands, clamp/fallback and
click/focus/source rejection. Native j/k steps, edge clamps, Escape/reacquire
and Shift rejection were verified. OS-generated key repeat and actual IME remain
unverified.

### Native View history controls

Conditional workspace-root Back/Forward actions drive native menu availability
and bracket accelerators, with fresh boundary/owner checks before the existing
guarded history path. A separate root focus fills only an active, unowned focus
vacancy or a recognized unmounted surface; it never grants reader j/k ownership. No app-global history listener or
reader-local duplicate bracket handler remains. About/background dispatch is
excluded. Home/Tag/Graph boundary and key/focus models are covered. Native menu
Home/reader return, full-graph Back, boundaries and search exclusion were verified;
bracket delivery with the active IME, Tag/About and interrupted flows remain
unverified. Reload is excluded.

## Raster enlargement candidate

Explicit read-only enlargement of already decoded current-reader standalone PNG/JPEG images is implemented with exact source/block/allocation guards and one modal owner. Aspect-fit and identity/admission models pass on macOS. Native PNG landscape/portrait fit, content click, wheel isolation, dismissal, bottom-position retention and open-overlay Quit were verified. JPEG, resize, full keyboard/AX, source changes and IME combinations remain unverified. No new formats, fetches, zoom/pan, clipboard, persistence or vault writes.

## Current reader title copy

Explicit exact-title clipboard submission is source/tab/revision/launch/vault qualified, bounded to 64 KiB, with generation-safe 1.5-second feedback. Reuses the accepted reader and existing owner guards; no focus acquisition, clipboard reads, mutation or persistence. Shipped in `8b256fb`; the macOS suite passed 690 tests with 7 ignored, and an isolated native clipboard roundtrip verified exact title submission (see NATIVE-QA.md). General IME/accessibility and the other listed scenarios remain unverified; preview copy is deferred.

## Authored block links candidate

Current accepted reader paragraph/list markers and internal block-fragment links are implemented through existing resolver/navigation with separate heading/footnote namespaces. Duplicate IDs select the first occurrence; missing/invalid IDs never use a heading/top fallback. Source/tab/reader/interaction/viewport guards reject stale deferred jumps. Six model tests added; Rust/build/native validation pending. No standalone-marker attachment, transclusion, preview/snapshot pipeline change or vault writes.

Block-link model/native evidence is recorded in NATIVE-QA.md (697 Rust tests;
paragraph/tight-list/duplicate/missing/same-note pointer smoke). Centered placement,
block transclusion and the remaining native scenarios are not implemented or
validated by that increment. Normal vault writes stay disabled.

Arrival-cue follow-up: successful guarded block application now paints a transient,
source-qualified background without changing geometry, text ranges or Find.
Three model tests cover invalidation and old/new timers. Cloud diff-check passed;
Rust/build/native appearance and timeout validation remain pending.

Arrival-cue native pointer smoke passed for dark paragraph and light list rows,
including expiry without layout/position change. See NATIVE-QA.md for limits and
700-test evidence; this does not establish full keyboard/IME/block parity.

## Native Find routing

Conditional workspace-root listeners plus action-time checks route native Edit → Find commands to the accepted reader only. Old raw Cmd-F/Cmd-G handling is removed; Find input Enter/Shift-Enter stays local. Existing root focus recovery does not grant reader j/k ownership or replace an existing input focus. Four model/keymap tests added; 704 Rust tests passed (7 ignored), formatting and build passed. Disposable native menu/accelerator, query retention and Home/Search/About exclusion smoke passed; IME and interruption coverage remain pending. No requests, writes, clipboard or persistence changes.

## Reader Hierarchy panel

A source/scope-qualified, click-only reader panel reuses Home hierarchy transport, concurrency, diagnostics and bounded row rendering. Reader roots stay expanded; Home root behavior is unchanged. Close invalidates pending requests and stale controls; modal focus gates background navigation/Find/scroll and pauses follow. Five model tests added for admission, lifecycle, roots/nested/leaves, repeated labels/paging and errors. 709 Rust tests passed (7 ignored), formatting and build passed. Disposable native expansion/navigation/Back, dismissal/position retention, input isolation and Home smoke passed; IME/interrupted/large-tree coverage remains pending. No new endpoints, writes, settings or persistence; no reference hover/grace behavior.

## Copy MD

Explicit current-reader Copy MD converts accepted raw-body wiki labels and wholly valid include lines to portable Markdown, preserving ordinary source bytes and code. It deliberately preserves code literals, midline includes and unsupported include tails rather than reproducing destructive reference text-level conversion. Source/body/owner revalidation, a 256 KiB refusal limit and token-qualified submission feedback are implemented. Nine model tests added; 718 Rust tests passed (7 ignored), formatting and build passed. Disposable native clipboard round trip and submission feedback passed; full owner/race/IME coverage remains pending. Reference definitions and uncertain continuations remain conservative literals. No resolution, requests, export, clipboard reads, writes or persistence.

### Reader internal-link hover candidate
One transient accepted-reader body-link card reuses graph preview transport, lifecycle and inert rendering. Source/link/target, exact displayed content, tab/request/revision, launch/storage, working-scope revision, measured geometry and reader offset qualify delayed admission. The public text-layout byte lookup verifies the pointer is still on the exact link before a delayed read. No reader scroll handle is replaced. Other input owners take precedence. Accepted reader cards now offer explicit Pin into the shared graph pin collection; transient cards have no drag/resize, nested-link or clipboard behavior. Shared preview reads are limited to two live workers (including canceled UI jobs), 256 KiB bodies and 64 KiB titles. This is bounded support, not full hover/selection/accessibility parity; native hover and IME checks remain pending.

Validation: 725 Rust tests passed (7 ignored), formatting/native build and independent review passed. Disposable native normal/fragment clicks, Back and unresolved-click regression checks passed with 12 canonical files unchanged. Actual pointer hover timing, grace/card scrolling and interruption remain unverified; see NATIVE-QA.md.

### Native Reload menu candidate
Conditional workspace-root View → Reload / `Cmd-R` availability and action-time admission reuse `load_notes`; no app-global handler or second pipeline. Ready live reader/Home only, with accepted reader launch/storage provenance retained for explicit retry after read failure. Existing route/history restoration and dirty-transition guards remain authoritative. A current-request list/body token prevents duplicate reload dispatch, and old completions cannot release a newer token. Editor/dirty, other input/IME, modal/preview, About/background and quit owners are excluded from this native action. Tag/Graph and snapshot menu reload are not implemented. Model tests added; cloud Rust and native menu/accelerator validation remain pending.

Native Reload validation: 730 Rust tests passed (7 ignored), formatting/native build passed. Disposable note/Home/history/search-menu smoke is recorded in NATIVE-QA.md; pending/failure/IME native cases remain unverified. Home requires a recorded Home reload route or safe-session Home; route-less initial Home without session opt-in remains disabled to avoid the existing first-note fallback.

### Plain-paragraph selection candidate
One accepted live unstyled paragraph supports paint-only forward/backward range selection and explicit plain-text Copy selection. Grapheme-safe endpoints preserve authored displayed linebreaks, not soft wraps. Source/tab/request/revision/launch/storage, displayed-content identity, reader bounds/offset, scale, viewport and working scope are revalidated before copy. Drag cancels competing automatic positioning without replacing ScrollHandle; no global Cmd-C, input handler, auto-copy, request, persistence or vault write. Copy reports submission only with token-qualified feedback. Other owners, unsupported blocks and geometry changes cancel rather than guess. The bounded adjacent-paragraph extension is described below; rich/keyboard/accessibility parity and actual IME remain unverified. Earlier single-paragraph smoke does not validate the extension’s native event sequencing; model checks do not prove native hit testing.

### Recent notes and fixture restart continuity

The explicit Recent rail popup lists up to 100 successfully opened live notes,
newest first, deduplicated by validated vault and note ID. Closing a tab retains
its recent entry. A named working scope shows that vault and the launch vault;
the launch/default scope shows all recent entries. Accepted title updates do not
change recency. Failed, stale, preview, and editor-follow reads do not add visits.

Ordinary launches retain current-process-only recents with no session disk I/O.
Explicit disposable-fixture session persistence adds schema 6 recent-list restart
continuity, independently of open tabs; legacy schemas restore empty recents.
Restore stays on Home and performs no title fetch or synthetic visit. Unknown-vault
metadata fails closed rather than preserving an unavailable row as interactive.
Isolated native completed-save restart, closed-tab retention, cross-vault same-ID
reopening and accepted revisit ordering passed. Race/native offline-peer checks
and immediate/abrupt quit durability remain unverified. Hover/focus-to-open behavior is not yet implemented. The popup does not fetch titles or preview notes; it uses the normal
guarded note route when a row is activated. Close, Escape, and outside clicks do
not change the reader scroll handle or offset.

### Weekly task reading subset

Home has an explicit Weekly tasks view for one working vault, Sunday-aligned on
the device-local civil date. One bounded dated-task read supplies all seven days
and current-week Overdue; week navigation reuses the snapshot. Completed tasks are
excluded, scheduled/due same-day duplicates collapse, and source note activation
uses normal guarded navigation. Partial data, invalid dates, and per-section row
limits are explicit. Full Calendar, other week starts, events, task mutation and
persistence are not provided. Existing strict dated-task wire validation remains
unchanged; unsupported wire states are reported as errors.

Local PDF inspector: explicit macOS-only standalone image-style local attachments,
one selected page/document, source-qualified owned reads, bounded cancellable
helper. Original reader/Find/Copy MD source stays intact. Fixed resolution fits
viewport; PDF fences, inline embeds, text/search/forms/actions and persistent
cache remain unsupported. Isolated Japanese/Latin page navigation, encrypted
rejection, Close and Home/tab return passed; cross-vault races, resize stress
and accessibility remain unverified.

PDF enlargement reuses the accepted current-page image without new I/O. It is a
static snapshot modal, not the reference's independently paged PDF viewer. No
page navigation/actions/zoom; Close, Escape and backdrop dismiss. Reader media
input/routing guards cover both image and PDF overlays. Isolated current-page
enlargement, dismissal, routing containment and guarded Quit passed. Narrow
layout scroll retention and native storage races remain unverified.

Accepted PDF pages can be explicitly shown at the exact standalone attachment
occurrence. The source label remains visibly shaped (source Find/Copy MD intact),
with a fixed-height aspect-fit page appended below it; this is not full source
notation replacement. One shared accepted raster is displayed, and other
occurrences require Hide first. Show/Hide uses existing guarded passage restore.
No PDF text search, automatic loading, or additional decode. Isolated duplicate
occurrence, below-page passage retention and Find checks passed; narrow layout,
bottom clamping and native race/error injection remain unverified.

Rendered host-query list/board/gallery and visible calendar rows offer an explicit Open note control.
It uses one bounded, source-scoped engine wiki lookup per activation, preserving
resolved vault identity; absent/empty targets alone fall back to title lookup.
Snapshot/row/page/scope tickets reject stale work. Included queries and whole-card/keyboard/modifier/hover parity are deferred. No creation or external
opener; failed resolution keeps the accepted preview. Native list/board/gallery navigation and Back passed in an isolated title-mode fixture. Calendar, cross-vault, id-v1 and in-flight races remain native follow-ups. Calendar controls cover only first-three visible rows per date; day/+N remain inert.

- Explicit inline host-query summaries: strict paired source/result fence correspondence, source retained, one fixed-height text-only panel, 100 total rows and explicit truncation. Uses accepted snapshot only; rich layout stays modal. Includes/other transformations reject inline admission. Surface-qualified resolver navigation and guarded passage restoration; no source/normal-vault/session writes. Isolated native first-query Show/Hide, selected second occurrence projection and row navigation passed. Scrolled Hide passage retention, empty/truncated panels, Find/Copy interactions and pending-lookup races remain unverified natively.

Successful rendered-preview include headers offer explicit Open source note from accepted note ID/kind/vault. Exact host request, occurrence, preview epoch, storage and registry gates are revalidated; aliases/line numbers never select targets. Existing guarded note routing performs the destination read. Included body links, whole-card/keyboard/modifier behavior and excerpt-anchor jumps remain outside this slice. Native validation pending; normal vault/session write gates unchanged.

Include-source validation: 796 Rust tests passed (7 ignored), native alias-to-identity and distinct-source navigation/Back/Quit smoke passed without fixture writes. Cross-vault, journal, stale-state races and keyboard/IME remain native follow-ups.

Accepted included prose has explicit per-link Open controls for the first 16 supported parsed wiki/Markdown occurrences, not direct inline hyperlink activation. Included vault/etag/identity and host route ownership remain separate through bounded resolution; exact occurrence and owner tickets gate completion. Heading/block fragments retain their authored wiki targets. Unsupported Markdown/external/code/generated-query links and modifier/keyboard/hover parity remain excluded. Existing header Open source and query navigation remain separate actions sharing the lookup bound. Native validation pending; normal-vault gates unchanged.

Included wiki validation: 801 Rust tests passed (7 ignored), isolated foreign/host same-title resolution, failed-target preview retention, Back and Quit passed without canonical writes. Heading control destination checked; exact fragment placement, block/race/keyboard/IME cases remain native follow-ups.

Included explicit link controls additionally admit simple relative Markdown note titles with case-insensitive literal .md removal before strict percent decoding. Encoded .md remains part of the decoded title; query/hash/path/scheme/domain-like or malformed targets stay inert. This is deliberately narrower than reference Markdown linking. Included namespace and host route owner remain separate; existing exact tickets/worker cap apply. Host Markdown parsing and wiki fragment behavior are unchanged. Native verification pending.

Included Markdown validation: 803 Rust tests passed (7 ignored), isolated
foreign/host same-title and same-ID resolution, Back, missing-target retention,
excluded controls and Quit passed with 20 unchanged canonical hashes. Unicode,
empty-vault, cap/race/keyboard/IME native cases remain follow-ups.

### Dedicated Calendar: bounded read-only month slice

Calendar now has a separate tab/history route, Sunday-first month grid and
Previous/Next/Current month controls. One scoped activity-range snapshot supplies
note-activity counts. All real dates are selectable, including zero-count dates,
which can still have tasks/events; this is an intentional difference from an
empty/inert cell. Selected-day sections reuse the bounded agenda/task/event
adapters and exact note navigation. Surface, month revision, selected-day serial,
registry, scope, request, launch and storage guards reject stale work.

Calendar route persistence is fixture-only schema 7 (read versions 1–6 too),
with safe Home startup; month/day/results are not saved. No ordinary writes or
journal/event/task edits are enabled. Full-calendar planning interactions and broader Calendar parity remain unimplemented. Isolated native
zero-activity task/event display, vault scope, Search/task navigation, Back, Reload,
Next/Current month and safe Home restart passed. Dirty/IME, rapid-response races
and interrupted-save scenarios remain unverified natively.

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

### Explicit existing Calendar journals

Calendar offers read-only daily/monthly journal lookup controls, scoped to the
selected vault and exact journal kind/title. Missing, ambiguous and unavailable
results remain on Calendar; there is no creation or generic title-resolution
fallback. Normal note reads carry an exact journal expectation through both
metadata discovery and final body acceptance to reject post-lookup identity,
kind, title or vault changes. A mismatch after route acceptance shows an error,
not a different body. Memory-only qualified day restoration reads fresh results
on Back. This differs from automatic day-cell journal navigation. Isolated native GUI verified daily/monthly opening, same-title normal-note
exclusion, selected-day Back restoration, missing feedback and foreign scope.
Interrupted lookup flows, mid-read mutation and real IME remain unverified
natively.

### Read-only rich task lists

Accepted host-note task lists with doing (`[/]`), waiting (`[?]`), cancelled
(`[-]`), priority, scheduled or due metadata appear as STATE / TASK / SCHED / DUE
rows. Nested tasks keep their indentation; text styling, internal links,
priority and ordinary text remain in the task cell. Dates and state come from
the task metadata accepted with that exact note body. No extra request, checkbox
mutation, task editing or date picker is added.

This first slice preserves source order: view-only sorting is not implemented.
It admits only pure unordered task lists with one-line task paragraphs, complete
source-line correspondence and at most 256 projected rows per document (nesting
up to eight levels). Plain `[ ]` / `[x]` lists, completion-stamp-only lists,
mixed or unknown-marker lists, multiline/multiple-paragraph items, ordered lists,
unsupported cell content and missing/invalidated metadata retain ordinary source
rendering. Included excerpts, snapshots and query output are not projected.

Rows keep the original reader block indices and block IDs. Find and links use
the displayed table cells, not hidden source paragraphs; Copy MD still uses the
unchanged authored body. Partial text selection in tables remains unsupported,
as with existing native tables. Save-side metadata invalidation falls back to
source rendering until a fresh accepted read. Native layout, Find/link geometry,
clipboard and interruption validation for these rich rows is pending.

Rich-list admission additionally requires exact authored text and metadata correspondence.
Only unique trailing standalone `[#A]`, `[sched:YYYY-MM-DD]` and
`[due:YYYY-MM-DD]` tokens are projected. Progress cookies (`[3/5]`, `[50%]`),
repeat/completion tokens, block IDs, code spans, ambiguous or duplicate tokens,
and any mismatching item keep the entire list in its original reader form.
This conservative fallback preserves progress and links rather than silently
dropping notation not represented by the task listing metadata.

Host-context-dependent Markdown reference links (including collapsed and shortcut
forms), footnotes and non-inline parser links also keep the whole list in its
original reader form. Inline Markdown and wiki links remain eligible; the table
never reparses a resolved host reference without its definition.

### Explicit local display math (bounded subset)

Standalone host `$$…$$` occurrences can be explicitly rendered locally, then
shown below their unchanged visible source. One accepted raster/worker, exact
source/request/occurrence ownership, bounded PNG decoding and guarded Show/Hide
passage restoration. Inline math, automatic replacement, included/query models,
transformed readers and unsupported syntax remain literal. No equation actions,
remote services, imports or vault writes. Source Find/Copy semantics remain
visible; this is not full reference math parity. Pure correspondence/bounds
regressions are added; final GPUI/native layout and interruption QA is pending.

- Flowcharts: bounded read-only Mermaid flowchart TD/TB/LR subset, rectangles and
  decisions with explicit arrows/labels, stable DAG placement and outer-lane
  skip-rank routes. No cycles, other Mermaid diagram types, markup, interaction or
  full Mermaid parity. Original source retained; native visual QA pending.

- Rust/Nix fenced code: bounded display-only lexical colors, lossless original
  UTF-8 ranges and unchanged Find/Copy. Rust lifetimes/chars, nested comments and
  raw strings are distinguished; Nix closed indented strings and escapes are
  opaque strings. Nix interpolation/path/URI ambiguity conservatively leaves the
  remainder plain; no injected-language or semantic-token parity. Pure source
  regressions pass; native visual QA remains pending.

- Callouts: five case-insensitive GitHub-style marker-only quote introductions
  receive a title and light/dark border. Source Copy MD is unchanged; the first
  marker line is removed from the visible body and inline byte ranges are shifted
  together. Block count/indices remain stable, including marker-only paragraphs.
  No custom titles, collapse, or quoted-text selection expansion. Unknown/code/
  escaped/styled markers fall back. Pure mapping harness passes; native QA pending.

- Selected Markdown lines: separate exact-source copy action for a released
  single plain paragraph; whole touched lines and original CRLF preserved, with
  existing source/document/scope/geometry ownership rechecked at copy time.
  Single-paragraph operation remains available alongside the extension below. Ambiguous rendered
  transformations, links/styles/quotes and payloads above 256 KiB remain unavailable.
  Pure source-line model tests pass; native pointer/clipboard verification pending.

- Adjacent paragraph selection: viewport-measured, exactly source-mapped plain
  paragraphs can share a forward/reverse drag with bounded gap bridging. Unknown,
  offscreen and noneligible crossings cancel; no auto-scroll. Original single-
  paragraph fallback remains. Source-line copy preserves interior blank lines;
  owner/geometry/source checks remain action-time. One-entry data-only source-map
  cache avoids repeat parsing. Pure model regressions pass; full native event-order
  and clipboard validation remain pending.

- Task date order: the existing read-only Note tasks inspector cycles SCHED/DUE
  ascending, descending and source order. Stable equal dates and missing-last
  ordering retain original line identities. Only one fully matched top-level rich
  list (256 rows maximum) is admitted. Nested/multiple/ambiguous inputs remain in
  source order. This deliberately does not sort the in-reader checklist, avoiding
  changes to body anchors, Find, links or selection. Native control QA is pending.

### Reader preview pins (cloud-only candidate)

Ready reader hover cards can be explicitly frozen into the existing four-card preview collection. Partial content and diagnostics stay attached to the accepted snapshot; loading/failed reader cards remain transient. Ordinary reader navigation retains pins without changing reader scroll/selection/history at Pin time. Exact identity controls keep Close independent of a different transient card and Open as page uses guarded navigation. Available-registry, storage/session and working-scope changes retire reader-owned pins. The existing graph behavior suppresses a new hover card for an already-pinned target; explicit selector/title controls raise that existing card, rather than automatic hover reordering. No new fetch, nested preview, editing, media or persistence is added. Actual-source model checks pass separately from unverified full GPUI/native interaction.
