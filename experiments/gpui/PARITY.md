# Native workspace parity


**Safety gate:** ordinary-vault live and CLI launch is blocked pending a canonical-read-only engine contract. Existing startup/watch/read refresh may write activity sidecars. Synthetic/explicit JSON input remains available; engine-backed checks below describe implemented capabilities usable only with explicitly verified disposable fixtures, not ordinary-vault availability. The read fixture permission does not enable editing.
Target: the existing live Track.app workspace, not just the snapshot prototype.
The Go engine remains authoritative. Static-site generation and encrypted publishing
remain existing CLI/web capabilities; displaying embedded apps is still a desktop requirement.

Source-status audit: `7b035a74035e42f2c05113dee12a97bddb966fef` (#379).
This is a documentation-only inventory of the integrated GPUI source and help,
not a new build, test run or native acceptance result. **Every native parity gate
below remains unverified.** A bounded implemented slice is not full feature parity.
Historical fixture/native results belong to [NATIVE-QA.md](NATIVE-QA.md) and the
corresponding PR; they do not validate the current combined revision. In particular,
cloud model/adapter harnesses do not establish full GPUI compilation, native layout,
pointer/clipboard behavior, accessibility or IME correctness.

Use disposable fixture vaults, never a configured personal vault, for tests.
Ordinary live/CLI use is blocked until the engine can guarantee canonical-read-only access. Existing read paths can write activity sidecars as well as derived caches; “no mutation API” is insufficient.

## Integrated capability inventory

`[x]` means the **named bounded slice exists in source**, not that it is complete
or natively validated. Paths in this table are relative to `experiments/gpui/src/`.
Detailed controls and exclusions are in the [README](README.md) and
[experimental native help](../../docs/help/note/1785024017000.md).

| Bounded capability | Implemented | Native parity | Source / principal limit |
| --- | :---: | :---: | --- |
| Synthetic/JSON snapshots; explicit CLI vault reading | [x] | [ ] | `main.rs`, `backend.rs`; not the live editing contract |
| Owned HTTP notes, federated search, result-tag refinement and reason/excerpt labels | [x] | [ ] | `workspace_live.rs`, `search_refinement.rs`, `search_excerpt.rs`; excerpts are bounded inert text, snapshot search unchanged |
| Working-vault scope, launch-vault Tag page, qualified tabs and bounded Back/Forward | [x] | [ ] | `shell.rs`, `workspace_tags.rs`, `tabs.rs`, `navigation.rs`; ordinary settings persistence excluded |
| Accepted tags/flags, grouped typed properties and guarded property links | [x] | [ ] | `note_metadata.rs`, `note_properties.rs`, `shell.rs`; literal metadata, CONFIDENTIAL is not access control |
| Read-only relation paging and Home/reader hierarchy | [x] | [ ] | `reader_relations.rs`, `workspace_hierarchy.rs`; bounded click navigation, not complete hover/AX parity |
| Basic Markdown, tables, emphasis/code/strike and plain tasks | [x] | [ ] | `markdown.rs`; unsupported content retains conservative fallback |
| Five marker-only GitHub-style callouts and numbered footnotes | [x] | [ ] | `markdown.rs`; no custom/collapsible callouts, footnote backlink/endnote-layout parity excluded |
| Read-only rich host-task rows and inspector date ordering | [x] | [ ] | `markdown.rs`, `workspace_note_tasks.rs`, `task_date_sort.rs`; exact metadata mapping, inspector-only ordering |
| Code captions/Copy and JS-family, Lua, JSON, Python, shell, Rust/Nix lexical colors | [x] | [ ] | `code_copy.rs`, `code_highlight.rs`; bounded lexical hints, not semantic/injected-language highlighting |
| Source-reader Find/reveal, outline, wiki/Markdown links and authored block links | [x] | [ ] | `reader_find.rs`, `workspace_find.rs`, `markdown.rs`, `shell.rs`; namespaces and stale-source guards, not general fragment parity |
| Source-reader text selection and explicit selected-line/source-reference Copy | [x] | [ ] | `reader_selection.rs`, `selection_lines.rs`, `shell.rs`; mapped eligible paragraphs only, no auto-scroll/general rich selection |
| Current title Copy and portable Copy MD | [x] | [ ] | `title_copy.rs`, `markdown_copy.rs`; accepted source, bounded explicit clipboard submission |
| Owned render/include inspector, explicit inline includes and query summaries | [x] | [ ] | `workspace_includes.rs`, `included_notes.rs`, `query_inline.rs`; strict correspondence, not unrestricted transclusion |
| Included source/link and host-query row navigation | [x] | [ ] | `include_navigation.rs`, `include_excerpt_links.rs`, `query_navigation.rs`; explicit source-qualified controls, not whole-card/inline click parity |
| Rendered reading mode, preview Find, host link controls, selected-text Copy and Contents | [x] | [ ] | `rendered_reading.rs`, `rendered_find.rs`, `rendered_host_links.rs`, `rendered_selection.rs`, `rendered_contents.rs`; separate preview ownership and limits below |
| Query list/board/gallery/calendar snapshots and host task boards | [x] | [ ] | `query_view.rs`, `workspace_includes.rs`, `task_boards.rs`; bounded read-only layouts, no editing |
| Static line/grouped-bar charts and inline metric ViewSpec resolution | [x] | [ ] | `numeric_chart*.rs`, `inline_viewspec.rs`, `viewspec_preview.rs`; not all charts/metrics or file-backed specs |
| Static mindmaps and Mermaid TD/TB/LR DAG flowcharts | [x] | [ ] | `mindmap*.rs`, `flowchart*.rs`; bounded shapes/arrows, no cycles/general Mermaid/interactive diagram parity |
| Explicit local display-math raster | [x] | [ ] | `math_probe.rs`, `workspace_math.rs`; standalone host display math, visible source retained, not inline/full TeX parity |
| Bounded local PNG/JPEG display and enlargement | [x] | [ ] | `raster.rs`, `workspace_images.rs`; source-qualified attachments, not arbitrary remote media |
| macOS local PDF inspector, selected-page enlargement and explicit inline page/fence subset | [x] | [ ] | `pdf_probe.rs`, `reader_pdf.rs`, `workspace_pdf*.rs`; page images, no text/search/forms/actions |
| Home activity, selected-day agenda/tasks/events, Weekly tasks and Open tasks | [x] | [ ] | `workspace_activity.rs`, `workspace_weekly.rs`, `workspace_open_tasks.rs`; scoped bounded reads, no task/event mutation |
| Dedicated Calendar month, event/task/note previews and existing journals | [x] | [ ] | `workspace_calendar.rs`, `calendar_month.rs`; explicit day selection/journal lookup, not automatic creation |
| Calendar Today/Tomorrow/This week agendas | [x] | [ ] | `calendar_agenda.rs`, `workspace_calendar_agenda.rs`; Sunday-first, engine-zone anchor, local filters/pages |
| Home Recently created notes and separate Recent visits | [x] | [ ] | `workspace_recent_created.rs`, `workspace_recent.rs`; created order is not visit order; ordinary persistence excluded |
| Daily journal On this day | [x] | [ ] | `workspace_journal_day.rs`; read-only scoped discovery/navigation |
| Full-vault graph/camera/selection, keyboard opening and bounded preview pins | [x] | [ ] | `graph*.rs`, `workspace_graph.rs`, `workspace_preview.rs`; not full layout/performance parity |
| Reader internal-link hover and explicit reader preview pins | [x] | [ ] | `workspace_preview.rs`; shared four-card limit, frozen snapshots, no persistence/nested preview |
| Consumed external Markdown snapshot, local file/heading navigation, PNG/JPEG and Find | [x] | [ ] | `workspace_handoff.rs`, `snapshot_links.rs`, `snapshot_images.rs`, `snapshot_find.rs`; accepted in-memory files only |
| Explicit Editor Follow and absolute HTTP(S) links | [x] | [ ] | `workspace_follow.rs`, `editor_follow.rs`, `external_links.rs`; guarded/proportional follow, no automatic external opening |
| Owned SSE change/gap notices and guarded explicit native Reload | [x] | [ ] | `live_events.rs`, `event_awareness.rs`, `main.rs`, `shell.rs`; notices are not authoritative note identities |
| Session-only conservative current-note auto refresh | [x] | [ ] | `live_refresh.rs`, `workspace_refresh.rs`; current accepted note only, measured-position/interaction guards below |
| Native About, Copy Build Info, --version, View history and Edit Find controls | [x] | [ ] | `about.rs`, `build_info.rs`, `main.rs`, `workspace_find.rs`; not complete menus/shortcut parity |
| Display of existing read/seen milestones; fixture reading checkpoints | [x] | [ ] | `note_metadata.rs`, `session_store.rs`, `shell.rs`; no ordinary milestone writes/NEW inference |
| Fixture-only tab/history/scope/recent metadata persistence | [x] | [ ] | `workspace_session.rs`, `session_store.rs`; safe Home startup, no automatic restored-body fetch |
| Fixture-only one-draft editing, guarded save/exit and opt-in recovery | [x] | [ ] | `workspace_edit.rs`, `workspace_recovery.rs`, `draft_recovery.rs`; ordinary editing disabled |
| Fixture-only launch-vault creation and durable receipt evidence | [x] | [ ] | `workspace_create.rs`, `note_creation.rs`, `creation_store.rs`; no automatic retry/uncertainty reconciliation |

## Remaining broad parity gaps

These remain open despite the slices above: general create/edit/delete and metadata
editing; attachments/draw.io editing; durable ordinary workspace/draft/reading-state
migration; automatic seen/read milestones; full transclusion and line-copy linking;
all code/math/diagram/chart/query formats; audio/video/maps/Grafana/OGP/embedded apps;
calendar/event/task/journal editing and complete timezone/planning behavior; complete
graph layout/preview parity; themes, keyboard, selection, clipboard, accessibility
and IME; notifications/speech; packaging, startup/restart/termination and performance
acceptance. Do not infer completion from a module, unit test, screenshot or older smoke.

## Current bounded behavior and safety limits

### Reader, rendered content and snapshots

- Source selection is deliberately conservative. Exact selected Markdown lines and
  source references require current accepted source mappings; adjacent plain-paragraph
  selection is bounded and has no auto-scroll. Copy MD/title/code remain separate explicit
  actions. “Copy submitted” is an API submission, not a verified cross-app paste.
- Rich task projection requires exact host body/task correspondence and conservative
  one-line unordered lists, at most 256 rows/eight levels. Unsupported tokens,
  references, mixed/ambiguous inputs fall back to original source. Date ordering is in
  the Note tasks inspector for one fully matched top-level group, not reordered body
  anchors. Task boards and all task/date controls remain read-only.
- Callouts retain block indices and shift displayed inline ranges after removing the
  marker. Only NOTE/TIP/IMPORTANT/WARNING/CAUTION marker-only introductions qualify;
  escaped/styled/unknown introductions stay ordinary quotes. Footnote numbering and
  definition jumps do not imply automatic backlinks or browser layout equivalence.
- Rendered reading is session-only, one automatic render attempt per accepted owner;
  busy/error needs explicit retry. **Rendered Reload resolves accepted text; Source
  View > Reload retrieves the note.** Rendered-header history admits adjacent note/Home
  entries, not Tag/Calendar/Graph. Source keeps the full existing guarded routes.
- Preview Find searches eligible displayed host/include text, not query/task/diagram
  internals. Preview selection is one wholly visible non-link prose paragraph, supports
  inline styling, and caps copied text at 256 KiB; it does not select across excerpts.
  Contents uses host-only native anchors (256 headings/8192 blocks/2 MiB heading text).
  Same-host Markdown heading controls require matching source/rendered heading sequences;
  unavailable targets never silently jump to top. These controls do not change host
  source checkpoints/history. Find/selection/Contents and stale geometry do not compete.
- Include/query/host navigation remains explicit, namespace-qualified and bounded.
  The host and included source retain separate identities; accepted snapshots/caches
  never independently authorize a read or navigation. General inline/modifier behavior
  and arbitrary host fragments are not provided.
- Snapshot file links, headings, Find and images use only accepted in-memory entries.
  No filesystem/vault/network/OS fallback, imports or persistence. Missing targets stay
  inert; PNG/JPEG preparation has a single worker and explicit count/byte/pixel bounds,
  not a hard whole-process/GPU memory bound.
- Reader/graph previews share four pins. Reader pins freeze accepted content and retire
  on authority changes; ordinary navigation can retain them. Pin does not fetch again,
  mutate reader selection/history, add nested links/media or persist across restart.

### Math, diagrams and media

- Display math is an explicit local owned-child rasterization of standalone host
  `$$…$$`, with one accepted raster/worker and guarded Show/Hide. Authored source stays
  visible/searchable/copyable. Input/output/dimension/deadline bounds and a data-only
  source-reference cache are not arbitrary TeX support. No imports, remote renderer or
  vault writes; inline/included/transformed math stays literal. Redistribution must
  retain the bundled STIX Two Math font license (see README).
- Mermaid support is only bounded acyclic TD/TB/LR flowcharts with rectangles/decisions,
  arrows and labels. Unsupported diagrams retain source. Mindmaps/charts/ViewSpec are
  similarly bounded, not general browser renderer substitutes.
- PDF uses macOS page images and explicit loading, page selection, enlargement and
  admitted standalone/fence projections. No PDF text selection/search, form/action
  execution, persistent cache or automatic general embedding. Raster and PDF overlays
  retain reader input/routing guards; source Find/Copy semantics remain separate.

### Calendar and Home

- Calendar is a dedicated guarded tab/history route, with a Sunday-first month grid,
  selected-day details and independent event/task/note summaries (three previews each
  per date). Zero-activity dates remain selectable. Unknown/error/partial is not zero.
  Zone edits invalidate old month summaries and require explicit Apply/retry. Counts,
  membership and due-bar today come from accepted engine results, not guessed dates.
- Existing daily/monthly journals open only after exact scoped kind/title/identity
  verification; missing/ambiguous lookup does not create or fall back to generic title
  resolution. Month/day/zone/results are not persisted; Calendar route metadata remains
  fixture-only with safe Home startup.
- Today/Tomorrow/This week uses an engine-zone today anchor, civil ±1/±7 navigation,
  Sunday-first weeks, local All/Events/Tasks filters and 100-row pages. One serial batch
  shares read permits; cancellation retains a running worker's permit. Clock/focus
  invalidation does not establish native midnight/DST/IME correctness. Client byte/row
  limits and between-request deadlines do not bound engine index work or hard-kill HTTP.
- Open tasks preserves engine ordering and duplicates with local paging; Recently
  created requests the selected vault's first 100 creation-ordered regular notes.
  They are distinct from Weekly tasks and Recent visits. Source/registry/scope/storage
  changes retire old controls. No task/event/note mutation or ordinary persistence.

### Conservative live refresh

- Session-only opt-in observes change/gap hints on the existing 250 ms poll, with a
  500 ms quiet debounce, two-second minimum start interval and one physical worker
  across stop/start. New accepted notes get one check while enabled; errors wait for
  an event or explicit Retry. Storage checks bracket the read, not an atomic snapshot.
- Identical responses preserve content/caches/scroll. Source metadata-only changes
  preserve position when body/task inputs match. Body/task changes require a measured
  top; **any changed Rendered snapshot requires its measured top**. Otherwise retain
  accepted content with a notice to return to Source and use View > Reload.
- Inactive windows, edit/dirty/recovery/IME, Find, selection, modal/navigation or Editor
  Follow activity block replacement. Interrupted/stale work cannot publish afterward.
  No history/recent/checkpoint write or navigation; data/include/query-only dependency
  refresh remains outside this contract. Partial/error responses retain prior content.

### Fixture-only mutations and persistence

Ordinary editing stays disabled. A separately verified disposable edit fixture gates
one draft, Preview/Edit/Split, etag saves and dirty navigation/quit prompts. Recovery
additionally requires its explicit opt-in, owned server and the same verified edit/session
fixture roots. Protected checkpoints and ordered save/discard clears do not establish
power-loss durability or complete Dock Quit/IME behavior.

Session metadata requires a separate fixture opt-in, protected lock/revision/identity
checks and atomic writes. Corruption degrades to memory-only; startup restores safe
Home and does not fetch the last active note. It is not ordinary preferences, draft
migration, automatic scroll restoration or a guarantee of queued writes at abrupt quit.

Creation is explicit and launch-fixture-only: bounded intent is durably recorded before
POST; pending requests block quit, unresolved evidence survives restart and gates New.
Created-but-unverified stays distinct from failure. No automatic retry, title-based
success inference, named-vault creation, body override or general uncertainty
reconciliation. Existing receipt verification may permit retained close, not a second
POST. Fixture path checks are not a sandbox against hostile concurrent filesystem changes.

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
