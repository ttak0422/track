# Native validation log

## Ordinary editing release gates

Ordinary vault editing is **disabled**. The fixture switch is not a release
switch. Historical observations below apply to their recorded builds, not
implicitly to the current executable. Before enabling ordinary editing, record
fresh results for the candidate build in an isolated fixture:

| Gate | Evidence available | Still required |
| --- | --- | --- |
| Exact-source explicit save | Model, transport and real-engine conflict/lost-ack tests | Current native input → Save → dirty indicator → disk verification, including final LFs |
| Pending save and navigation | Model guards and production pending-transition controller tests | Hold a real acknowledgment; Cancel departure, then release it; no unexpected navigation or second PUT. Repeat with Save and continue and verify exactly the requested destination |
| Native termination routes | Earlier fixture app-menu Quit, Cmd-Q and window-close observations | Current Dock Quit, repeated quit, pending/failed/uncertain save and Cancel on every native route; verify exact owned child and ports reclaimed only after permitted exit |
| IME and editor focus | UTF-16/composition model tests | Actual preedit, commit/cancel, Save, mode switch, tab navigation and quit with a real input method; no lost/duplicated text or shortcut interception |
| Tab and session ownership | One-active-draft model; fixture metadata-only session tests | Multiple open tabs with a dirty active note, background close, return/cancel and relaunch; no draft replaced by restored metadata |
| Recovery boundaries | Failed/uncertain writes retain the in-memory draft; explicit fixture checkpoints implemented | Verify current native recovery actions, durable versus pending status, restart/quit and conflict preservation. Ordinary recovery remains disabled |
| Storage/session changes | Captured identity/revision and stale-completion tests | Current native behavior after owned-server loss or storage change: retain draft, refuse unsafe save, present recovery without automatic retry |

Headless tests can strengthen request sequencing, exact destination consumption,
source/etag identity, LF handling and process cleanup. They cannot close the
native delivery, modal focus, Dock, IME or rendered-state gates. Tab metadata
persistence does not persist draft bodies. Safety approval for body editing also
does not establish create/delete, metadata-editing or full editor UX parity.


## Disposable editor fixture, 2026-11-13

Build: `9871df1` source, Apple Silicon/macOS 27.2. Separate application bundle,
public help-vault copy, temporary config/cache and dedicated ports. Existing
application windows and personal vaults were not used.

Observed:

- Native text and fixture warning rendered; hidden-button native chrome opened.
- Clean Cmd-Q exited the initial fixture, and its two server ports were released.
- A later search-focus session did not respond as expected to Cmd-A, Backspace
  or Cmd-Q. App-menu Quit did exit; owned ports were released. This is an open
  input-routing observation, not evidence of dirty-exit safety. A later diagnostic
  reproduction of the same key sequence resolved Cmd-A, backspace and Cmd-Q to
  the correct actions and exited cleanly. The earlier cause is not established;
  no speculative platform shortcut patch was applied.
- Startup storage was initially partial. Explicit Reload after readiness reached
  ready/read-write, and search returned public fixture notes. Lack of automatic
  readiness refresh remains a compatibility gap.
- Edit mode accepted a fixture-only change. App-menu Quit displayed the dirty
  prompt; Cancel kept the application and draft. Explicit Save acknowledged the
  update, cleared the dirty mark and preserved authored text. Disk inspection
  confirmed only the temporary fixture changed.
- After another draft change, an external fixture-file edit caused Save to be
  rejected before PUT. The UI retained the unsaved draft and original revision.
  Quit then Discard exited without overwriting the external version; both owned
  server ports were released.

Not yet exercised: pending/uncertain save, repeated quit,
Dock Quit, native window close, active IME, multiple tabs, native source conflict
reconciliation, resize/fullscreen, and matched light/dark visual captures.

Unit and transport tests do not replace these native checks. Ordinary vault use
remains read-only; fixture editing is not release-ready.


Optional input diagnostics require both the developer-fixture opt-in and
`TRACK_NATIVE_INPUT_TRACE=1`. They log command-key identifiers/modifiers and
unusual native text codepoints, not note/query contents. This diagnostic does
not change input behavior and is disabled for normal vault use.

## Follow-up checks

The diagnostic fixture again retained an unsaved draft and showed the app-menu
Quit guard; Discard exited and released both owned ports. Dock access through
the UI automation timed out, so this does not verify Dock Quit. Active IME
composition remains untested; ordinary vault editing remains disabled.

Controlled loopback HTTP tests hold a PUT response to check the pending-save
leave gate, including cancellation of leaving without cancellation of the write.
Recorded-write simulations with a disconnected, 500, or malformed response
retain the draft and original revision and send exactly one PUT. Subsequent GET
inspection alone does not clear dirty state. These exercise real transport plus
the leave state machine, not native event delivery.

Startup now waits through both initializing and partial storage within the
existing 15-second bound, before publishing the interactive session. It does not
reload underneath navigation or a draft. Readiness after that deadline still
requires Reload; the earlier GUI symptom is not yet revalidated on this change.

## Search and startup follow-up

An intermediate build with partial-readiness waiting opened the public fixture
without the earlier partial warnings and returned search results without manual
Reload. Enter in the search palette opened the first result; clean Cmd-Q exited
and released the owned ports. Arrow-key selection/highlighting is not yet visually
verified: the diagnostic trace recorded Enter but no arrow event during that
automation attempt. No claim about native IME candidate navigation follows.

The next navigation increment adds scoped search result keys and guards composition
in the keymap, avoids a dirty prompt/reload for the already-active tab, and replaces
internal debug destination text with a themed save prompt. Heading fragments use
verified note identities and delayed layout anchors, retaining the intent across
the draft guard. Native fragment and dirty-fragment tests are still pending.

A test-only socket failure was reproduced on macOS: accepted sockets inherited
nonblocking mode and returned WouldBlock immediately. Resetting the controlled
server's accepted sockets to blocking fixes the harness without relaxing any
production timeout or save validation.

## Heading and native close follow-up

On the f8ba5c1 build, a same-note plain Markdown heading link in the disposable
fixture scrolled Calendar display to the top of the reader after activation.
This validates one native fragment path, not encoded anchors or dirty transitions.
Window → Close Window on a clean note exited the fixture and released its ports.
A later dirty-window-close preparation was interrupted before editing; that case
remains unverified.

## Reader Find: pending native acceptance

Use only the disposable fixture; these are planned checks, not observed results.

- Cmd-F opens an independent floating query field without moving the tab strip.
  Repeated Cmd-F selects its query; workspace search retains its own query.
- Mixed-case text, Japanese, literal punctuation and table text highlight without
  losing link styling. Enter/Shift-Enter wrap; Done/Escape clear highlights and
  restore reader focus without changing note content.
- Keep committed results during IME preedit, then update on ordinary or unmark-only
  commit. Cmd-G/Shift-Cmd-G must not navigate a workspace search result.
- Change tabs, refresh the source and close/reopen Find; stale highlights or deferred
  scroll requests must not affect the new document. Dirty fixture editing must stay
  untouched and Find must be unavailable while that editor is active.
- Test light/dark, narrow windows, multiple hits in a tall paragraph, and table
  cells. The new shaped first-match-line reveal requires the checks below; the
  earlier block/cell anchoring did not establish this behavior.

## Home, Find and dirty window close, 2026-11-14

Build: `c055fe0`, isolated Editor QA bundle using the same disposable public-help
fixture. The copied executable matched the locally validated build by SHA-256.
No personal vault or existing application window was used.

Observed in light mode:

- Home displayed its scoped activity heatmap, date range and contribution count.
  This is a rendering check, not matched Web parity or accessibility acceptance.
- Opening Calendar events and using the reader Find control with `calendar`
  displayed 15 matches. Shift-Enter wrapped from 1 to 15; Previous moved to 14
  and revealed the corresponding lower paragraph. Current and other matches used
  distinct backgrounds, and a highlighted inline link retained its underline.
- Escape dismissed Find. Cmd-F reopened the retained query with its contents
  selected. Find did not move the tab strip, and its rail control was absent in
  fixture Edit mode. IME composition and exact matched-range scrolling were not
  established by this sequence.
- A disposable unsaved heading edit triggered the save prompt through native
  Window → Close Window. Cancel retained the window and draft. A subsequent Cmd-Q
  displayed the prompt again; Discard and continue exited the application.
  Disk inspection confirmed the unsaved marker was not written, and the owned
  backend port was released.

Dock Quit, active IME, native pending/uncertain save handling, multiple dirty tabs,
dark/narrow visual comparison and selection/focus coverage remain pending.
Ordinary vault editing remains disabled.

### Dark-mode follow-up

The same executable rendered Home activity and Calendar events in dark mode.
Find distinguished its active and other matches while preserving inline-link
underlines. Escape followed by clean Cmd-Q exited. This is not a matched Web
comparison or a contrast/accessibility audit. Attempting to obtain Dock controls
through the native automation surface returned `timeoutReached`; no Dock Quit
was performed and no OS input-source settings were changed.

## Lost save acknowledgement, 2026-11-14

The `c055fe0` executable used a new public-help fixture and the test-only
`qa/save_fault_proxy.py` adapter on dedicated loopback ports. The adapter forwarded
one original PUT to the real Go backend, then dropped its successful response.

- Save and continue from the quit prompt did not exit after the response loss.
  The editor retained the draft/dirty mark and displayed “save outcome unknown”.
- A second explicit Save was refused rather than retried. Inspect server content
  fetched a comparison without replacing the draft or replaying the quit action.
  The existing final-newline ambiguity remained explicitly reported.
- The adapter recorded exactly one successful PUT and one dropped acknowledgement;
  the disposable file contained the authored marker. This proves the uncertain
  outcome was not merely a failed preflight or a fabricated backend response.
- Discard followed by shutdown released public/upstream listeners. The adapter's
  expected `reaped` audit entry was absent, so owned-child reap is not claimed for
  this real-backend sequence. A subsequent automation observation showed a fresh
  unavailable Home before a final clean Cmd-Q; the restart/observation sequence
  remains ambiguous and needs a dedicated lifecycle follow-up.

Native pending-success timing, Dock Quit and active IME remain unverified.
This test does not enable ordinary editing or durable recovery.

A follow-up clean GUI shutdown with the shorter adapter grace again lacked the
reap audit entry; this did not establish the original cause. A separate non-GUI
run with a new public fixture and real Go child did record SIGTERM, successful
child reap (13 ms, exit 0) and adapter exit 0. This narrows the remaining gap to
the GUI/adapter lifecycle sequence rather than proving it safe. The scaffold now
records lifecycle signals and cleanup timing without logging note contents.

## Fenced-code controls, 2026-11-14

A disposable public-help fixture used the native executable with SHA-256
`d488c781537af5c6ab4a5c9fe468969ab82b9bc1afedcaef07e85810e5515cf3`.
At a 1906 × 1050 light-theme viewport, the CLI note displayed SH captions.
After explicitly focusing the reader, Option-C revealed the first eligible code
block and its Copy control; another Option-C moved the visible control to the
next block. Shift-Option-C returned to the preceding block. Escape followed by
pointer hover over its header showed the control without activating it.
The right-hand note aside remained visible throughout. This caught and fixed an
overly broad guard that treated an enabled aside graph as an unavailable reader.

No Copy action was activated, preserving the existing system clipboard. Actual
clipboard contents, copied feedback timing, assistive-technology labels and
keyboard operation while Find is open are not validated by this observation.
The first Cmd-Q observation showed a fresh Home; a second Cmd-Q reported app
exit. As with the earlier lifecycle observations, this sequence is not evidence
of clean first-attempt process termination. Normal editing remains read-only.

### Nonactivating clean-exit observation

A follow-up of the same binary recorded a launcher PID in an append-only audit
before its `exec`. After one Cmd-Q from clean Home, observations used application
inventory rather than querying the closed app's accessibility tree. The first
inventory still listed the app during shutdown; the next did not. The known PID
returned `no such process`, both owned listeners were absent, and no new launcher
entry appeared. This establishes clean single-command exit for this fixture run.
It does not prove why earlier accessibility observations showed fresh Home, nor
validate dirty, Dock, IME or proxy child-reap cases.

A fresh clean proxy fixture repeated the nonactivating observation with no saves
or fault arm. One Cmd-Q exited the native process, adapter and real Go child;
all three recorded PIDs returned `no such process`, four recorded public/upstream
listeners were absent, and no second launch was recorded. However, the adapter
audit again contained only `started`, without `cleanup_enter` or `reaped`.
This confirms process disappearance, not graceful adapter child reaping. The
proxy lifecycle audit remains an open investigation independent of app lookup.

### Inherited signal mask in the QA proxy

An isolated masked-exec reproduction explained the missing adapter audit:
registering a Python handler does not unblock inherited SIGTERM. A local dispatch
worker probe also found SIGTERM/SIGINT blocked while the main thread was unblocked.
The masked adapter ignored SIGTERM past the native grace period and emitted only
`started`, matching the GUI symptom. The test-only adapter now registers handlers
before unblocking those two signals, before spawning its Go child. A regression
verifies inherited bits, signal-driven cleanup, child reap and timely exit.
Seven adapter tests pass; production native signaling/timeouts are unchanged.
The post-fix GUI repeat was blocked by the locked Mac, so actual native/proxy
reap confirmation remains pending. No app was launched by that blocked attempt.

### Headless real-engine save races (2026-11-14)

Added ignored `live::save_races::owned_engine_post_preflight_conflict_and_lost_ack`.
It requires an absolute `TRACK_NATIVE_TEST_BINARY` and
`TRACK_NATIVE_TEST_SYNTHETIC_ONLY=1`. Each run creates an empty temporary vault,
explicit sibling config/cache locations, and two unique synthetic notes using
`track new --title` with piped stdin. No shell, existing server, user vault,
manual note-storage format, GUI or clipboard is involved. The normal owned
startup/readiness and native save preflight remain active. Canonical vault and
note paths, title/body/revision and child liveness are checked before writes.
The test-only bounded loopback gate cannot forward to other origins or notes.

Verified against a binary built from checkout `c394df8` with `go build ./cmd/track`:
SHA256 `b5c10e65873fef9588bf31d0aa5f95de40fff4bf21aa057feb6c373effafd61c`.
The ordinary offline/locked Rust suite passed: **271 passed, 6 ignored**.
The explicit real-engine test passed: **1 passed**, with these separate outcomes:

- Conflict: after the native storage/source/storage preflight, the gate held the
  complete primary PUT. One deliberately acknowledged competing native save
  changed the revision. The original PUT then received the real engine's HTTP409
  changed-on-disk response. Native completion retained Conflict, authored draft,
  original baseline/etag and canceled the pending leave. The competing body
  remained on disk. One gated primary PUT plus one deliberate competitor save;
  no third retry or substituted revision.
- Lost acknowledgment: the gate consumed and validated the real upstream HTTP200
  acknowledgment, then closed the downstream connection. Native completion was
  Uncertain, with draft/baseline/etag retained and leave canceled. Explicit GET
  comparison showed the committed read-model text; disk retained both authored
  final LFs. Comparison did not clear dirty/uncertain state, exact reconciliation
  refused the LF ambiguity, and explicit Cancel stayed on the draft. One PUT.
- Both cases preserved note identity/title/tags. Pending duplicate saves and
  edits were rejected. Owned child reaping and release of both engine listeners
  and both test relay listeners were asserted; temporary fixture data was removed.

Sandboxed attempts stopped before any PUT while waiting for writable storage.
The synthetic-only diagnostic identified `read own start time: fork/exec /bin/ps:
operation not permitted`. The successful run allowed the engine's self-process
verification; no readiness or production safety check was disabled. This does
not establish a problem with the earlier public-help seed. The final test uses
only fresh synthetic CLI notes, independently simplifying fixture isolation.

This is headless engine-plus-draft-model evidence, not Dock Quit, modal focus,
IME, clipboard or rendered dirty-state evidence. The conflict checks a competing
revision visible at the engine's revision check, not atomic protection against
every external filesystem race. Production save code is unchanged.

### Pending save destination ownership

The actual workspace now stores pending departure in an attempt-bound controller.
The first fully qualified destination remains frozen, and an acknowledged save
can consume it only once, after the editor accepts its submitted baseline. Cancel,
obsolete attempts and unsuccessful input acknowledgment cannot authorize a later
navigation. The existing single active draft policy is unchanged, including an
explicit draft decision before closing a different tab.

Synthetic tests connect the production controller to the draft model and tab
identities. They cover same numeric IDs in different vaults, Cancel during a PUT,
retained failure outcomes, changed environment, rejected widget acknowledgment,
obsolete completion, clean/no-op save, composition and preserved link payloads.
These tests observe accepted destinations and tab changes, not real native event
routing. The regular Rust suite passes (280 tests; 6 opt-in tests ignored), as do
build, formatting, termination-gate/preparation checks and seven proxy tests.
The previous real-engine tests are separate evidence and were not rerun for this
controller-only change. No GUI or personal-vault validation was performed here;
the release gates above remain open and ordinary editing remains disabled.

## Page history: pending native validation

Model and disposable-fixture tests cover bounded chronological history, branching,
vault-qualified IDs, Cancel, frozen save tickets and stale read/save completions.
No native keyboard or IME delivery was tested for this slice. When GUI validation
is authorized, check Back/Forward rail availability on Home, Graph and notes;
reader-focused Cmd-[ / Cmd-]; reopening closed notes; same-ID cross-vault routes;
failed GET then Back; repeated Back during Save/Discard/Cancel; and late GET/save
after Cancel or newer navigation. Confirm a reload retains the selected route,
Graph uses the current working vault, empty Home does not become the configured
Home note, and mouse side-button tab cycling is unchanged. Ordinary editing must
remain disabled; dirty-flow checks use only isolated disposable fixtures.

### Build identity (headless validation)

The executable now exposes compile-time package version, full commit, tracked-source
state and target through `--version` and the existing diagnostics drawer. The command
returned before GUI/source/server startup. The repository at `91133ad` with the build
identity changes reported that full revision and `modified`, as expected. The Git fixture
script `qa/test-build-identity.py` checks archive/unknown, clean, modified, untracked-file
exclusion and linked-worktree metadata. Repeating the Cargo build without changes did
not recompile the application. Rust tests: 296 passed, 6 ignored. Help site generation
and prerender succeeded. Native diagnostics layout/readability is still unverified;
this does not close menu/About/clipboard or native parity gates.

## Read-only tags and flags: native checks pending

Headless coverage verifies accepted-source tags over stale list hints, empty flags,
both known flags, literal unknown flags, source clearing and vault-qualified identity
changes, including an accepted launch-vault alias returning a named vault.
External-reference labels preserve their distinct vault targets.
No native GUI was launched for this slice. Before claiming read-only metadata parity, check:

- Current-note tags and both flag labels appear legibly in light/dark and narrow views.
- Trail, children, backlinks and cross-vault rows retain readable flags and correct targets.
- Switching notes (including equal IDs in different vaults), pending/failed first reads,
  literal Home and Graph never retain previous-note tags or flags.
- A deliberately retained body on server failure keeps its matching confidentiality
  label and existing error notice; configured Home notes still show their metadata.
- Relation clicks still pass the fixture dirty-navigation Save/Discard/Cancel guard.

These are simple muted-ink labels, not decorative stamp or accessibility parity.
`CONFIDENTIAL` is display-only, not authorization or access control.

## Multiple graph pins: native checks pending

Headless coverage exercises four pins plus one transient, same IDs in distinct
vaults, out-of-order read completion, selected close/unpin, stale actions, raising
without content or placement changes, pin capacity preservation, all-card cache
pruning and session invalidation. Existing storage/dirty-navigation gates remain.
No GUI was launched for this slice. Validate before claiming native parity:

- Pin four different graph notes; a fifth Pin explains the limit without dropping
  any card. Compare identical titles/IDs in two vaults using selector scope labels.
- The selector stays below tabs and above cards; title click raises just that pin,
  Close removes only that pin, and mouse/wheel gestures do not reach the graph.
- At narrow and short sizes the selector wraps and remains reachable. Check light
  and dark themes, edge-clamped overlapping cards and long titles/vault names.
  While pins exist, cards must stay below the reserved selector band, including
  at the native minimum height (480px); all card-header actions remain reachable.
- Raise/close does not move surviving cards. New hover brings a transient forward.
  Pinning/raising does not steal keyboard focus or trigger extra note reads.
- Navigate with multiple pins, then exercise dirty Save/Discard/Cancel. Check that
  storage epoch changes and owned-server replacement clear every card and cache.

This is a bounded comparison UI, not full window-manager parity. Pin drag/resize
was added subsequently; its native validation checklist follows.

## Viewport paint culling: native comparison pending

Pure tests retain tangent/padded circles, crossing edges with offscreen endpoints,
zero-length segments and transformed camera coordinates; invalid geometry is rejected.
Labels and picking remain on their existing paths. Automated suite: 312 passed,
7 ignored. Optimized standalone geometry suite: 28 passed, 4 ignored; synthetic
predicate measurements are recorded in GRAPH.md. Verify border antialiasing, compact
node outlines, selection highlights and crossing edges under native pan/zoom before
claiming visual parity. No native FPS or GPU improvement has been measured.

## Background force cancellation: native validation pending

Headless suite: 315 passed, 7 ignored. Cancellation tests cover initial, mid-solve
and final checkpoints, empty/zero-iteration solves and replacement/drop token
isolation. Confirm with a fixture that rapid graph replacement and node dragging
do not allow a late snapshot to move nodes or override the camera. Verify settling
still completes normally when only panning or zooming. Cancellation does not
interrupt initial circular placement/allocation; no native latency claim is made.

## Pinned preview movement and resize: native validation pending

Headless suite: 318 passed, 7 ignored. Model tests cover bounded rectangles at
680×480, movement/resize, independent pin geometry, unpin/invalidation, stale
movement/end callbacks and released-button cleanup. Verify with fixture data:

- Drag only the title; Open/Close/Unpin must not initiate movement. Resize using
  the bottom frame strip, with all header actions remaining visible.
- Move across other cards and graph nodes without triggering graph pan/selection
  or wheel input; confirm the active pin is frontmost.
- Release inside/outside the window, leave the capture area, switch applications
  and refocus. No capture layer or movement should remain stuck.
- Resize the native window; cards must remain below tabs/selector and inside it.
  Test minimum window size, long title/vault labels and capacity messages.
- Open a dirty-navigation dialog during a gesture and confirm its controls remain
  above the capture layer. Keyboard/AX equivalence remains unimplemented.

## Shaped Find line reveal: native validation pending

Headless suite: 321 passed, 7 ignored. Tests cover UTF-8/prefix validation, code
and cell offsets, supplied measured-line scroll/clamp calculations and invalidated
layout generations. Source review confirms text prepaint precedes measurement;
these are not font shaping or actual GPUI visibility tests. With a disposable
fixture, check late matches in long wrapped paragraphs/code/table cells, Japanese
and emoji, list prefixes, and links. Verify Enter/Shift-Enter reveal the active
match's first line without losing inline styling, link clicks or code Copy.

Change query, source, tab and window dimensions around the deferred frame. Check
that new content is not moved by an old request and that resize-during-Find stays
usable. Multiline matches reveal only their first line; horizontal reveal remains
out of scope. Ordinary note bodies and reading metadata are not written.

## Fixture-only history persistence: native validation pending

Model and temporary-directory tests cover bounded history/cursors, schema 1 reads
and schema 2 writes, unknown/duplicate field rejection, artifact preservation,
cursor-only changes, safe-Home startup without snapshot rewrite, and rejection
after newer accepted navigation. Existing ownership/path/lock/revision/conflict
checks remain. Tests do not establish native startup timing or OS shutdown.

With only the explicit disposable fixture opt-in, visit notes and Graph, restart
and confirm Home opens without a prior-note request. Use Back/Forward explicitly;
check the saved Forward branch was truncated by the new Home visit. Navigate while
startup is pending and confirm the newer route/history survives. Repeat with dirty
Save/Discard/Cancel and pending/uncertain writes. Verify ordinary launches create
no session files. Queued metadata is not guaranteed flushed on abrupt quit; this
is neither durable draft recovery nor ordinary-profile persistence.

## Pinned selector keyboard controls: native validation pending

Use disposable fixtures with one through four pins. Check the selector hint is
readable at minimum window size, F6/Shift-F6 enters first/last pin only from the
reader or unfocused workspace, and focused controls scroll into view. This is an
experimental native shortcut, not existing Track keyboard compatibility; verify
macOS function-key settings and conflicts before treating it as a stable binding.
Reader Tab must still change document tabs. Pin Tab/Shift-Tab must cycle only live
pin controls, with a visible focus border. Title arrows move 8 pixels and Shift
resizes, clamped within the viewport. Enter/Space activates only the focused
control; Escape exits without closing a note. Unpin/Close must retain sensible
focus without hover or newly created pins stealing it.

Test stale controls after scope/storage invalidation and pin replacement. Editing,
IME composition, Find/search, menus, pending dialogs and pointer dragging must not
accept pin keys. Open as page must retain dirty-navigation guards. Verify key
repeat, modifier combinations, tiny windows and simultaneous pointer input.
Transient previews remain pointer-only; native focus, VoiceOver and full keyboard
parity are not established by the headless key/model tests.

## Explicit build-information copy: native validation pending

In a disposable fixture, invoke Copy Build Info from the application menu and
from diagnostics. Paste into a scratch editor and compare against this exact
executable's `--version` output. Only package version, full compile-time revision,
tracked-source state and target should appear, never note content, vault paths,
server URLs or diagnostics. Opening the drawer, selecting a note and hovering a
control must leave the existing clipboard untouched. Verify menu discoverability,
keyboard menu invocation and Quit remain intact. Headless payload tests cannot
establish actual macOS pasteboard delivery or accessibility.

## Fixture-only working-vault persistence: native validation pending

Use an isolated temporary fixture registry with launch and secondary vaults.
Select the secondary working vault, then restart into safe Home and confirm the
selector retains that scope without reopening a saved note. Open tabs from both
vaults and verify their identities remain unchanged, and search still returns
federated results. Change only the scope (not tabs/history), restart, and check
that the new choice persists. Cancel a dirty scope transition and verify no new
choice is stored. Repeat with pending/failed Save; do not weaken the edit gate.

During delayed startup, make a newer accepted route/scope choice and verify it
wins over restoration. Removing the saved scope or altering fixture configuration
must preserve the old state file and show a persistence warning, not silently
reinterpret it as the launch vault. Legacy snapshots should start at launch scope
without a startup rewrite. Ordinary mode must still create no session files.
These checks remain unperformed; model tests do not prove native restoration,
selector labeling, dirty-dialog handling or shutdown durability.

## Owned event-stream notices: native validation pending

Use synthetic fixture notes/data and only the server owned by that fixture app.
Change a fixture note and a data file externally; check their notices without
body replacement or automatic navigation. Dismiss a painted notice while a
second event arrives; the second change must remain visible. Break/restart the
fixture stream and check disconnection and post-reconnect gap notices. No notice
may claim all served vaults or pinned previews are synchronized.

From a dirty fixture draft, use notice Reload and exercise Cancel, pending Save
and failed Save. Check the draft and route remain protected. Dismiss must only
acknowledge a notice, not save, reload or remove a current disconnected warning.
Check narrow widths, themes, keyboard/AX discoverability and IME focus. Close the
app during header wait, active streaming and reconnect delay; inspect owned-child
cleanup without touching an existing vault or unrelated listening process.
These GUI checks remain unperformed; loopback tests are transport evidence only.

### Shell fence lexical hints (pending native QA)

Use disposable notes containing `bash` and `sh` fences with comments, quoted
Japanese text, escaped spaces and line continuations. Compare the exact source
with Copy and Find, including CRLF and multiline strings. Check light/dark
contrast, wrapping and narrow-window overflow. Heredocs, substitutions and
context-sensitive shell syntax are not a supported grammar; verify that the UI
never suggests code execution or modifies the source. No native acceptance is
claimed by the tokenizer tests.

### Reader position identity (pending native QA)

In disposable fixtures, scroll two same-ID notes from different vaults to distinct
positions and revisit via tabs and history. Confirm unchanged content retains its
position. Then change the source, reload, and restart the owned server: confirm
stale offsets are discarded. Also exercise a slow loading placeholder, a failed
read, a fragment link and Find to ensure position bookkeeping never overrides an
explicit jump. Check geometry clamping after resize. No on-disk reading position
or seen/read milestone should be written.

### Daily agenda (pending native QA)

Use disposable vaults with notes recorded as active on adjacent dates. Select Home
activity days, including zero-count/partial days, and confirm the engine's active
notes appear. Change scope/day rapidly, resize the calendar and retry a failed
request; old responses and row callbacks must not navigate. Open a result and
verify vault identity and dirty-state navigation guard. Check long titles, more
than 100 results, narrow windows, keyboard access and VoiceOver. No journal or
note should be created by selecting a day.

### Daily task list (pending native QA)

In disposable vaults, exercise TODO/DOING/WAITING/DONE/CANCELLED tasks with only
scheduled, only due and both dates. Confirm exact-day matches, no duplicate row
when both match, and no overdue or recurrence expansion. Trigger independent
agenda/task failures and retry; the other successful list should remain visible.
Switch day/scope during reads, then click old rows: stale callbacks must remain
inert. A result opens the source note without changing task state or promising a
line jump. Check more than 100 matches, partial empty results, long text and
keyboard/accessibility behavior.

### Local raster subset (pending native QA)

Use disposable PNG/JPEG assets with Japanese names, transparency and landscape/
portrait dimensions. Verify source captions remain searchable, four-image limit,
fit inside narrow reader, and Find/heading jumps after image reflow. Navigate,
reload/change source and restart server during delayed reads: stale frames must
not appear. Test unavailable/oversize/corrupt assets and explicit Reload retry.
Confirm inline/table/code, external/data/file URLs and SVG remain literal without
network fetch. Inspect memory/GPU lifecycle, color/orientation and accessibility
separately; headless decode tests do not establish native visual correctness.

### Full graph keyboard selection (pending native QA)

Use an isolated graph with disconnected nodes and similar titles. F7/click into
the canvas, then exercise arrows, Home/End, Enter, Escape and Tab. Verify deterministic
selection, camera reveal, note identity and the dirty-navigation guard. Check
modified keys, repeated Enter, pointer drag, temporary/pinned previews, menus,
search, dialogs and IME composition never cause accidental navigation. Switch
graph scope/reload while settling and ensure cleared/stale nodes cannot open.
Validate Tab exit, focus indication, narrow layouts and VoiceOver independently.

### Calendar display zones (pending native QA)

Use disposable vault events spanning UTC midnight, offset timestamps with dates
on either side of UTC, fractional seconds, multiple dates, leap day and cancelled
status. Verify the UTC heading stays visible even for an empty result and original
event-zone labels are not confused with display-zone membership. All-day ranges
retain civil dates; no deleted events appear. Apply Asia/Tokyo, New York DST days,
and Apia’s skipped date. Verify typing does not fetch, successful Apply changes
the accepted label, and failure preserves the previous labelled snapshot with
a separate error. Device-zone following and preference persistence are not
claimed. Change zone/scope/day or restart during delayed reads; old responses must not
appear. Exercise independent event/agenda/task errors, Retry, 101+ results across
pages, long titles, narrow layout, keyboard and VoiceOver. No event/note writes or
external link opening should occur. No native evidence is claimed for this slice.

### Thematic rules (pending native QA)

Use a disposable note with `---`, `***`, spaced underscores, quoted/list rules,
setext headings and fenced/indented code containing the same text. Verify divider
spacing, quote borders/list indentation, light/dark contrast and narrow layouts.
Find must skip only actual separators and retain surrounding/code matches. Check
heading/Find jumps and screen-reader semantics; no native evidence is claimed.

### Recorded milestone labels (pending native QA)

Use disposable notes with positive seen/read milestones, missing fields and
unavailable metadata. Verify Read precedence, no NEW/unread inference, and no
label carried across same-ID notes in different vaults or loading placeholders.
Reload changed metadata and verify the accepted source wins. Confirm narrow/light/
dark rendering and accessibility, and that reading never sends a milestone POST.

### Footnote display (pending native QA)

Use disposable notes with repeated/missing references, duplicate/unused
definitions, Unicode labels, and nested lists/quotes/code/tables in definitions.
Verify visible labels, source-position definitions, preserved ordinary links,
Find results and subsequent outline jumps. Click references in paragraphs/tables
and verify a unique definition is revealed without fetching or changing tabs.
Missing/duplicate labels stay inactive. Change note/source, open an overlay or
enter fixture editing between paint and gesture; stale jumps must not execute.
Check light/dark/narrow layouts, selection and VoiceOver.

### Whole-vault hierarchy (pending native QA)

Use disposable nested/empty/partial forests with repeated titles and flags.
Open, expand, collapse and refresh; selecting a title must use the normal
guarded reader. Change scope/server/storage while a delayed response or click
is pending. Close before a response, then reopen and retry. Verify no stale
branch/note opens, no isolated/cyclic structure is inferred and no writes occur.
Check narrow/light/dark layouts, long titles, bounded scrolling, keyboard and
VoiceOver. Network/model tests are not native interaction evidence.

### Current-note task inspector (pending native QA)

Use disposable notes with all task states, dates/priorities, undated tasks,
Unicode/long text and more than 100 rows. Open/close/page, Reload changed content,
switch same-ID notes across vaults and enter fixture editing while a stale
control is retained. Verify source order, dates and source-line labels, no stale
rows or extra requests, and no writes/state changes. After fixture Save or
accepted comparison, old embedded tasks must be unavailable until Reload.
Exercise narrow/light/dark
layout, scrolling, keyboard and VoiceOver. This does not validate task editing.

### Reader relation pages (pending native QA)

- Use an isolated fixture with more than 100 children and backlinks. Verify both
  page controls, engine order, flags and cross-vault destinations at narrow widths.
- Change/reload the source between pointer-down and activation; stale rows and
  breadcrumbs must not navigate. Exercise loading, error, edit and closing gates.
- Check keyboard focus and accessible labels. Headless pagination/source tests do
  not establish native interaction parity. Ordinary editing remains disabled.

### Included-note inspector (pending native QA)

- Use isolated fixtures with same-vault, cross-vault, unresolved and selected-line
  includes; compare literal excerpts and diagnostics with the owned engine result.
- Verify opening/Refresh/collapse behavior, long text scrolling and no request
  while closed. Host Find/headings must retain their original ranges.
- Switch source or close while a request is pending. Old responses must not
  repopulate the inspector. Check native keyboard, focus and accessible labels.
- No normal editing or nested link/image execution is enabled by this inspector.

### Explicit fixture reading checkpoints (pending native QA)

- Build from a known clean checkout and use an isolated session fixture. Save a
  visible Markdown block, restart to Home, manually open that same note and Resume.
- Verify large paragraphs/code resume at block start, never claimed exact-line.
  Metadata, task/include inspectors and previews must not become checkpoints.
- Change body revision, viewport size, scale or build; Resume must refuse. Pending
  jumps must be cancelled by manual scroll/Find/navigation and source replacement.
- Check queued-write/error feedback, keyboard/AX and missing native measurement.
  Ordinary launches must have no checkpoint controls, geometry collection or I/O.

### Rendered include preview (pending native QA)

Use disposable fixtures containing supported standalone includes, adjacent
include directives, selected sections, errors, empty excerpts and unsupported
list/quote/table placements. Verify all surrounding content remains in order,
unsupported locations retain their text and notices, and target origin labels
are distinct across vaults. Switch source, Refresh or close during projection;
old results must not reappear. Host Find/heading/checkpoint actions must not act
on the preview or hidden reader, including already queued actions. Close should
restore the original reader and offset without persisting projected positions.
Check keyboard, narrow/light/dark layouts, scrolling and VoiceOver. No links,
assets, tasks or nested includes should execute. No native evidence is claimed.

### External Markdown handoff (pending native QA)

Use only disposable folder snapshots and the current owned server. Verify
Paste reads clipboard only on click; confirmation hides the token and explains
one-use consumption. Cancel before Open must not consume. Check valid/expired
URLs, foreign ports, malformed payloads, interrupted consume and replacement
failure while an old snapshot is visible. Never retry automatically. Switch
files/pages, close, restart the owned server and change host routes during a
pending result; stale snapshots must not publish. Check normal reader source,
Find and scroll remain unchanged, with no vault/file writes or image/link loads.
Verify long Unicode filenames, 101+ files, bounded errors, narrow/light/dark
layout, focus, keyboard/IME and VoiceOver. Headless tests do not validate these
native flows; normal editing remains disabled.

### Embedded task boards (pending native QA)

Use a disposable note with five task states and two empty taskboard fences.
Verify state/source order, counts, dates, empty and untitled tasks, three-lane
paging and 51+ cards in one lane. Each board keeps independent pages. Replace the
source while rendering, and verify no stale metadata or page action appears.
Included taskboards and invalid/nonempty fences must retain source, not host tasks.
Combine query/taskboard blocks beyond eight and check explicit limit notices.
No checkbox, state mutation, drag/drop or line navigation should be available.
Check narrow scrolling, keyboard/IME, VoiceOver and theme contrast separately.

### Query layouts (pending native QA)

Use a disposable query-only note and a note mixing includes, headings and queries.
Open Rendered content, then Rendered preview. Verify ordered list metadata and
side-by-side board lanes, empty results/lanes, hidden-title cards and duplicate
titles. Exercise 101+ list rows, five lanes and 51+ cards in one lane. Later pages
must remain reachable, and controls must not navigate or write. Replace/close the
preview during interaction and verify stale page actions do nothing. Confirm
unknown layouts and malformed/over-limit payloads retain source plus a notice.
Verify Sunday-first calendar grids for leap February, a six-week month and sparse
months across a year boundary. Check one-month paging, three titles plus remaining
count per date, empty dated results and readable original source. Impossible or
duplicate dates must fall back for the whole block, never shift or lose rows.
For galleries, test five or more cards, title hiding, icons/no-cover/loading/error
faces, local PNG/JPEG thumbnails, hostile paths and missing include origins.
Switch pages and close/replace the preview during fetches; no stale cover may
appear under another source or vault. Hidden pages must not fetch.
Check narrow layout, horizontal board scrolling, keyboard focus, IME, VoiceOver
and light/dark contrast. No native evidence is claimed for this slice.

### Fixture draft recovery integration (pending native QA)

Use a fresh disposable fixture with edit/session authorization and the additional
`--developer-draft-recovery` opt-in. Type Japanese and combining text, observe
Pending versus Saved locally, then reopen using the same fixture binding. No note
should open or save automatically. Open its source and explicitly Recover draft;
verify it is dirty, unchanged and not saved. Externally change/delete the source
and confirm recovery is retained without rebasing. Exercise explicit saved-draft
discard confirmation and Cancel.

Delay/fail local checkpoints and clear acknowledgements during Save/Discard plus
navigation, Window Close, Cmd-Q and Dock Quit. A timeout must suppress the pending
transition without claiming filesystem cancellation. Cancel must retain the
editor; late callbacks cannot navigate or quit after cancellation. Verify unknown
engine saves retain recovery and ordinary launches never touch recovery files.
Test IME, VoiceOver, narrow layout and multi-tab focus. These native scenarios
remain unverified; headless tests do not satisfy them.

### Explicit editor follow (pending native QA)

Use an owned disposable fixture session and a synthetic editor publisher. Verify
Follow editor is initially off, enable navigates only to registered saved notes,
and same IDs in different scopes are not confused. Burst updates must coalesce,
not replay every intermediate note. Stop, manual navigation, Find, rendered
preview, handoff, editing and closing must take precedence without save prompts.
Exercise a delayed request, storage restart, registry change, stream gap and
10-second publisher expiry. Paths must never open. With a long note, verify
approximate top/middle/bottom viewport following, including wrapped text and
non-scrollable content. Missing measurements must show unavailable, not jump.
Queue a delayed reveal, then manually scroll, resize, Find, change notes or Stop:
no later frame may move the reader. Confirm no automatic resume or persisted preference.
Native keyboard, IME and lifecycle checks remain unverified.

### Static numeric charts (pending native QA)

In an owned disposable fixture, open Rendered preview with line and grouped-bar
fences containing positive, negative, zero and null values. Check axes, category
order, legend colors, null gaps and zero baselines at narrow/wide widths and both
themes/high-DPI settings. Unsupported options must retain the full source, not a
partial chart. Verify retained source, included-origin placement, close/reopen
and stale actions after note replacement. No zoom, hover-detail or click action
should imply support. Keyboard, accessible labels and visual comparison with the
existing client remain unverified; ordinary vault editing remains disabled.

### Inline ViewSpec resolution (pending native QA)

Use an owned disposable fixture with supported inline metric line/bar specs,
unsupported file sources and engine-invalid values. Verify correct block-local
loading/result/error and original source retention. Open/close/reopen or replace
the note while resolution is delayed: late results must not reopen or update a
new preview. Confirm cross-vault includes require their own verified origin,
duplicates do not repeat requests, and invalid/file-backed specs never submit.
Ordinary readers, handoffs and pinned previews must not resolve ViewSpec.
Native keyboard, high-DPI layout and accessibility remain unverified.

### Native mindmaps (pending native QA)

Use disposable notes with heading/list maps, multiple roots, duplicate labels,
Japanese/emoji/combining labels and empty fences. Check node spacing, edges and
text at narrow/wide sizes and both themes/high-DPI. Empty host maps must use the
same captured source and exclude fenced headings; empty includes must not borrow
host headings. Unsupported indentation/caps retain the entire source. Links stay
inert and do not open browsers or notes. Source replacement/close/reopen must not
show a prior tree. Native keyboard/AX and reader scrolling remain unverified.

### Rendered-preview code/source Copy (pending native QA)

Use disposable host/include fixtures with tagged and untagged code, Unicode,
CRLF, nested quotes/lists, empty bodies and repeated identical snippets. Activate
only a visible Copy control and check the pasted plain text in a disposable
scratch target: no fences/caption/provenance, exactly one presentation newline
removed, no trimming of other whitespace. Check retained ViewSpec input versus
generated chart/query output and the over-256-KiB disabled state.

Refresh, close/reopen, navigate and replace the source around activation and
feedback reset; stale controls must never copy a different snippet. Confirm no
clipboard access during render, hover or refresh. Exercise keyboard focus,
Enter/Space activation and readable feedback in both themes and high-DPI layouts.
Headless fake-sink tests do not verify the system clipboard, native focus or AX.
Normal vault editing remains disabled.

## Current fixture startup, 2026-11-15

Build `35bb72d`, clean, ran in the existing isolated QA application bundle with a
fresh temporary vault/config/cache and explicit session/recovery opt-ins. No
personal vault was opened. The native Home screen rendered, but immediately
reported failed fixture recovery and a changed vault registry; diagnostics showed
ready storage and one verified vault. This is a reproduced startup defect, not an
editing or recovery pass. Cmd-Q exited the clean fixture and the owned server
port was no longer listening. Clipboard, dirty quit and IME remain untested for
this build. Ordinary editing remains disabled.

After refreshing the registry only once storage is ready, the same fixture
reached `Fixture recovery ready` and no longer reported a registry change.
The first retest correctly rejected noncanonical `/var` fixture arguments;
using their canonical `/private/var` paths resolved that fixture setup error
without weakening validation. The native search opened `Native QA Copy` and
rendered its Japanese Lua snippet. After Cmd-Q and relaunch the saved note tab
was present alongside Home, with Home active. This verifies startup and tab
presence, not active-tab fidelity, draft restoration or editing safety. The
modified build was based on `35bb72d`; the tested change is limited to startup
registry refresh. The live-gap notice remained visible. Final Cmd-Q removed
the owned listener. Native clipboard, dirty quit, IME and ordinary-vault edit
acceptance remain pending.

## Launch-vault recovery and dirty quit, 2026-11-15

Clean `ca0700b` admitted the disposable session, but rejected Edit because the
engine intentionally returns an empty wire vault label for its launch vault.
The corrected build preserves that label and admits it only for an empty request
scope in the verified, identity-bound fixture registry. Named scopes are never
inferred from missing labels. Ordinary vault editing remains disabled.

On the corrected fixture build, Edit opened the existing Japanese-code note.
Typing an ASCII draft suffix showed the unsaved marker and `Saved locally · not
saved to the note`. Cmd-Q displayed Save/Discard/Cancel; Cancel kept both app and
draft open. A second Cmd-Q followed by explicit Save and continue saved the suffix
through the engine and exited. The fixture file contained the suffix afterward,
and the owned server port was closed. This verifies one Cancel and successful
Save quit path, not Discard, failed-save quit, crash restoration, or IME marked
text. Japanese text already present in the source rendered correctly but is not
an IME-composition test.

Home activation after tab restoration is the existing intentional safe-start
policy, covered by session tests; it is not an active-tab restoration defect.
Clipboard testing remains pending: the initial include fixture used an unresolved
ID reference, so no Copy interaction was accepted as evidence.

## Crash recovery and discard, 2026-11-15

On clean `b17a7a9`, a fixture draft was checkpointed without changing its source.
Unicode inserted through native paste was present in the checkpoint as Japanese
text and decomposed `e` + combining acute. Automated key typing did not insert
those non-ASCII characters; this is not accepted as IME composition evidence.
Only the verified disposable GPUI PID was killed. The owned server stopped,
and relaunch with identical fixture identity reported an unsaved draft on Home.

Opening the matching source exposed a recovery-gate defect: the passive local
graph sidebar was treated as the full Graph surface, suppressing Recover.
The correction retains the full-graph/gesture and source-identity gates but does
not reject the passive aside. On that corrected build, scrolling the diagnostic
panel exposed Recover draft (no save); explicit activation restored the dirty
Unicode draft without an engine write. Cmd-Q then Discard and continue exited,
removed the checkpoint and left the source unchanged. The server port was free.
The diagnostic panel is scrollable; controls below its first page are not absent.

The rendered-preview Copy code control appeared on code-bed hover and reported
Copy requested after activation. Pasted payload verification remains pending,
so this is not a complete native clipboard acceptance result. Save-fault proxy
and marked-text IME scenarios remain pending. Ordinary vault editing stays off.

## Clipboard and uncertain quit, 2026-11-15

On clean `8804ab4`, the explicit rendered-preview Copy code control was activated
on a disposable Lua block. Cmd-V into the native fixture search field displayed
`print("日本語")`, without Markdown fences or provenance text. This verifies the
native clipboard round trip for this Unicode code example, not general text
selection, rich clipboard formats, or IME composition.

A new canonical temporary fixture used the Save fault adapter with the real Go
engine and all fixture edit/session/recovery opt-ins. After editing a note,
Cmd-Q opened the dirty-quit prompt. The adapter was armed to drop one successful
acknowledgement, then Save and continue was selected. The app stayed open in
Uncertain with the draft/checkpoint retained. Adapter events recorded exactly
one `put_ack` (200) and one `dropped_ack`; the source contained the submitted text.
Read-only Inspect server content displayed the saved body without replacing the
draft, replaying Quit, or sending another PUT. A later Cmd-Q still prompted.
Explicit Discard and continue then exited, removed only the local checkpoint,
and preserved the committed source. Adapter cleanup recorded SIGTERM and child
`reaped` status 0 in 9 ms; both public and upstream port pairs were free.
WebSocket interruption notices are expected in this Save-only adapter and do not
establish live-update parity. No automatic save retry occurred.

Actual marked-text IME composition and pending-success acknowledgement timing
remain unverified in native GUI. Ordinary vault editing remains disabled.

## Held successful acknowledgement, 2026-11-15

With the same disposable Save adapter fixture, Save and continue from Cmd-Q was
held after the real PUT succeeded. A test-only watcher released the adapter
1.7 seconds after its `held` event, before the normal client timeout. The native
window showed Saving, editing locked, and no duplicate save. After acknowledgement,
the checkpoint was removed, the submitted source was retained, and adapter cleanup
recorded the owned child reaped with status 0 in 11 ms. All four test ports were
free. A subsequent UI observation produced a fresh startup window whose adapter
failed to bind; it was closed. Do not count that inspection as evidence that the
original saving window remained open. The source/checkpoint and original adapter
lifecycle establish completion of the guarded Save-and-continue path.

Native IME composition remains unverified. Looking up the existing input-source
menu timed out in Computer Use; no IME configuration was changed. The separate
IME attempt's clean fixture window was closed without modifying its source.

Source review found that switching Edit/Split to Preview could unmount the
marked-text owner without consulting the live input handler. Mode changes now
fail closed while that handler reports composition; same-mode selection and
changes after commit/cancel remain allowed. A pure admission-matrix regression
covers the rule. It is not Workspace/input-method integration evidence, and
native IME acceptance remains open.

## About executable identity, 2026-11-15

The application-menu About action displayed the version, full 40-character
revision, clean source state and build target in a readable native window.
Repeated activation did not create another About window. Its Close button and
traffic-light close returned to the existing Home/note route. On a disposable
dirty note, opening and dismissing About preserved the visible draft and Edit
mode; Cmd-Q still presented Save/Discard/Cancel. Explicit Discard then exited
without writing the sentinel to the source.

The first native run found Window → Close Window did not dismiss About because
its action tried to update an already-dispatching window. Window updates are now
deferred. On clean `ebe48d1`, the same menu action closed only About, retained
Home and the owned server, and About reopened successfully. Final workspace
quit stopped the fixture server. Clipboard non-interference is source-reviewed,
not a native sentinel test in this run; About performs no clipboard action.
Native IME and ordinary-vault editing acceptance remain unresolved.

## Guarded Save/Close keyboard actions, 2026-11-15

On clean `e47a5c1` with the isolated owned-server fixture, Cmd-S from focused
source input saved the typed sentinel and stayed in Edit. Source readback matched,
the dirty marker cleared, and no recovery checkpoint remained. After another
unsaved line, Cmd-W opened the existing close-tab confirmation. Repeating Cmd-S
and Cmd-W behind it neither saved the sentinel nor replaced the decision; Cancel
preserved the visible dirty draft.

With that dirty workspace behind About, Cmd-S did not save and Cmd-W dismissed
only About. The underlying draft and tab remained. Cmd-W followed by explicit
Discard closed the note and selected Home; closing the last Home tab left the
application and owned server alive on empty Home. Further Cmd-S/W there were
inert. Final Cmd-Q exited, released the fixture server port, and left only the
previously acknowledged sentinel on disk, with no checkpoint. Native marked-text
IME, all overlay permutations, and pending network timing under these new keys
remain outside this run; existing model/transport tests cover their guards.
Ordinary-vault editing is still disabled.

## 2026-11-15: reader metadata badge implementation

Known source flags now retain separate literal DEPRECATED and CONFIDENTIAL labels
with theme palette treatments; unknown flags stay neutral inert text. Empty
labels are omitted. Tag width constraints and a wrapping badge row avoid adding
per-character UI nodes. Existing accepted-source and recorded-milestone guards
remain unchanged. This is not flag-based authorization or watermark parity.

Implementation and independent source review ran in the cloud. Native macOS
unit tests passed (590 passed, 7 ignored); the macOS build passed. GUI light/dark,
narrow viewport and long unbroken Unicode label wrapping were not exercised for
this increment. These checks remain open, as does real IME composition. Ordinary
vault editing remains disabled.

## 2026-11-15: dedicated tag-page implementation checks

Cloud implementation and independent source review added a dedicated read-only
launch-vault tag page, separate from the tab strip and search. Model tests cover
literal descendant/case matching, contextual Unicode casing, more than 50
matches in source order, stale source/response/row rejection, interaction-owner
admission, page-only history, and fixture schema 1–5 migration. A review found
and corrected storage-generation refresh becoming permanently stale: stable
new response generations are now adopted only after the original request and
launch identity still match, then row tickets are rebound.

macOS build and full Rust suite passed: 602 passed, 7 ignored. Formatting and
whitespace checks passed. No new GUI acceptance is claimed: note-to-tag-to-note
Back/Forward, Reload, retained-tab interaction, long tag/title layout and actual
IME remain pending. Ordinary vault editing stays disabled.


## Fixture title creation: bounded native smoke (2026-11-15)

The title-only developer-fixture dialog uses the launch vault's default template.
Ordinary vault writes remain disabled. At this smoke's revision, uncertain POST
outcomes retained title/destination and blocked navigation/quit without a durable
exit path. The subsequent receipt work is documented separately below.

Validation on an isolated empty fixture with a controlled default template:
- Click New, focus the title field, paste a title and press Enter: a created receipt
  with an engine-allocated ID appears.
- Explicit Open verifies and displays the nonblank generated template in Preview.
  Back returns to Home and retains the created-note tab.
- Repeating the title displays HTTP 409, retains input and does not overwrite the
  original. Escape returns to Home. Cmd-Q while the dialog is open is blocked;
  Cmd-Q after dismissal exits and both owned ports are closed.
- Post-quit disk inspection finds exactly one note with the expected template bytes.
- A separate real-engine fixture confirms first-note creation, default-template
  output, duplicate 409 with unchanged bytes and blank-title 400.

The full native Rust suite passes (623 passed, 7 ignored), as do build, formatting
and whitespace checks. Synthetic transport tests assert exact title-only POST
payload/count and zero PUT, malformed/lost acknowledgements, readiness, changed
fixture bindings, linked outputs and post-success read failure. The GUI smoke did
not record an HTTP method transcript, so it does not independently prove zero PUT.

Still unverified: actual Japanese IME composition, dirty Create dialogs in native
UI, held/lost POST responses through native quit/window-close, double activation
under a held response, accessibility/focus traversal and full layout parity.
Pasting text is not evidence of IME composition. No generic creation-parity claim.

The smoke exposed a pre-existing empty-list decode failure: the real engine emits
`{"notes":null}` for an empty vault. Notes now accepts that explicit null as an
empty list, while missing/malformed list shapes still fail. Exact wire evidence
and HTTP/model regressions cover the correction; the final null-decoder change
was rebuilt/tested but not re-run through the GUI.


## Durable fixture creation receipts

`track-native-create-v1.json` and its separate lock live only in the explicitly
isolated fixture cache. They do not enable or share the tab/draft artifact writer.
The single owned-client receipt owner uses the existing descriptor-relative
no-follow/ownership checks, bounded reads, exact bytes/device/inode compare token,
atomic replacement, file sync and directory sync. Binding is an identity/change
detector, not cryptographic authentication. Titles are bounded to 8192 UTF-8 bytes;
receipt artifacts to 64 KiB. Ordinary/non-edit clients never inspect/create them.

A durably acknowledged Pending intent precedes the only POST. Readiness and exact
fixture/receipt/lock ownership are rechecked after persistence before dispatch.
Crash before dispatch is deliberately indistinguishable from crash after an
unacknowledged write: restart shows unresolved evidence and never retries.
Acknowledged Created and definitive rejected/not-sent results are checkpointed;
checkpoint uncertainty freezes the writer and cannot authorize another request.

Once no worker is in flight, **Keep unresolved receipt and close** verifies/syncs
the exact Pending record without editing/deleting it. It releases only the
creation dialog: existing dirty/recovery/IME quit guards still apply. New reopens
the unresolved receipt instead of posting. Restart shows it again. Corrupt,
foreign, replaced or inaccessible evidence is retained and creation fails closed.
No title search, absence-based resolution, automatic source navigation or
uncertainty-erasure path is implemented. Storage failure can still block a safe
exit; the UI never presents forced termination as rollback.

New regressions include actual subprocess exit immediately after intent sync,
response loss/restart/no retry, shared-client duplicate admission, durable intent
present when POST arrives, receipt/lock replacement during held readiness GET,
unsafe storage before any request, ordinary-client no-touch, stale completion,
post-rename acknowledgement loss and retained-exit admission. The implementation
passes the native macOS Rust suite (642 passed, 7 ignored), formatting and cloud
whitespace checks; the native debug build also succeeds.

A fresh isolated fixture was seeded through the regression subprocess, which
exited immediately after durable intent sync without dispatching POST. Native GUI
startup restored its exact title and uncertainty warning. Escape and Cmd-Q did
not discard it. Explicit Keep receipt closed the dialog; New reopened the same
receipt rather than a title-entry form. Keep then Cmd-Q exited. Relaunch restored
the receipt again, and a second acknowledged exit succeeded. Post-exit checks
confirmed byte-identical receipt content, zero Markdown files and both owned
ports closed. This run had no session/draft flags. The workspace itself reported
read/storage unavailable, so it is evidence for local receipt recovery/exit, not
a successful live-engine creation or healthy-reader smoke. HTTP method absence
was not separately instrumented in this GUI run. Actual lost-response creation
is covered by transport tests, not this seeded startup observation.

Native follow-up: hold POST then request all quit paths; release a lost response,
keep receipt and quit, restart and verify the same title/evidence without a POST;
repeat with a retained dirty draft and real marked-text IME. Check creation stays
disabled after acknowledging uncertainty, and that a malformed receipt is never
erased. No normal-vault write release gate is opened by this increment.

## Idle anchor repaint and read-only navigation

A read-only disposable fixture exposed delayed same-note heading jumps: the
reader did not repaint the changed offset until another input. GPUI's anchor
queues a frame callback without invalidation; the reader had already deferred
the anchor call into a frame. Heading and footnote paths now queue a single
refresh after that offset callback, preserving existing source/epoch guards.

Native verification with a fresh two-note fixture and 30 paragraphs confirmed
that both the heading link and the footnote reference reveal their bottom-of-note
targets after a single click, without subsequent pointer or keyboard input.
Outline navigation back to the top, opening the second note, Back and Forward
also rendered the expected note. Clean Cmd-Q exited and released both owned
ports; all 10 canonical body/metadata hashes remained unchanged. No edit,
session or recovery opt-in was used. The 642-test Rust suite, formatting and
native build pass; independent review found no additional P1/P2 issues.

A separate read-only six-note fixture also verified scoped tag listing (including
descendant tags), closing the tag surface without losing its source tab, note
flags disappearing on an unflagged note and returning with Back, and Japanese
and long tag labels at a wide window size. Its 18 canonical hashes were unchanged.
These checks do not establish narrow-window/theme parity, full-graph interaction,
rapid interrupted anchor transitions, dirty-fragment behavior or native IME
composition. The ordinary-vault editing gate remains closed.

## Measured full-graph camera fitting

A read-only eight-node fixture reproduced a tiny initial overview and, after
Reload graph, a nearly overlapping central dot until Reset graph view. Graph
replacement reset viewport dimensions to zero and asynchronous force completion
could fit before actual canvas measurement. Auto-fit now waits for a finite
positive measured canvas and consumes a generation/camera-revision ticket once.
Later pan, zoom, selection and reset invalidate earlier fit intent. Scope/reload
replacement policy is unchanged; a successful fit cancels transient previews.

Five new regressions cover initial/replacement measurement, invalid dimensions,
stale layout or camera intent, one-shot fit and late force results. The native
macOS suite passes 647 tests (7 ignored); formatting and build pass. Independent
review found no P1/P2 issue after retaining transient-preview cancellation.

The rebuilt native app showed all eight overview nodes and labels on first open
and two Reload activations without extra input or Reset. Blank-space drag panned
the graph and remained stable while idle; explicit Reset restored the fit.
Earlier bounded checks also exercised single-click note opening and Back, title
visibility and one preview Pin/Close. Canonical fixture hashes (18 files) remained
unchanged and clean quit released both owned ports. Keyboard selection was not
visibly confirmed; event delivery versus input-owner rejection remains unresolved.
Pin capacity, interrupted settling gestures, native pinch, multi-monitor/narrow
layout and performance claims remain unverified. No ordinary-vault write gate
changed. This repairs the existing experimental graph controls, not a new public
command or launch option; user-facing help controls remain unchanged.


## Initial live-update coverage (2026-11-15)

An isolated owned-engine startup reproduced the initial connection race three
times: the ready token and HTTP 200 vault/storage responses preceded storage
initialization, so `/api/events` returned 503. About 180 ms after launch storage
was ready and SSE connected. Ready-stream headers arrived in under 1 ms, followed
by `: connected` and a `: ping` heartbeat after 25 seconds. Reconnection did not
replay events or honor Last-Event-ID. No timeout increase was needed.

With the rebuilt native app and the read-only six-note fixture, startup displayed
“Live updates connected after a delay; earlier changes may have been missed.”
Dismiss removed the notice; Cmd-Q exited and both owned listener ports closed.
All 18 canonical fixture files remained unchanged. Synthetic transport/model
tests cover subsequent EOF/rejected reconnects and stale dismissal without
weakening total-gap/editor-follow invalidation. Actual interrupted-stream GUI
behavior and IME composition were not tested in this pass.


## Footnote origin return (2026-11-15)

A new read-only fixture has three references to the same definition: at the top,
after 20 paragraphs, and in a table after 35 paragraphs. After positioning the
middle reference near the viewport center, a single click jumped to the bottom
definition; Return to reference restored the same middle context without extra
input, not the first reference. The table reference independently restored its
original table context, including after ordinary scrolling at the destination.
The one-shot Return control disappeared after use.

Following the table reference again and opening Beta from backlinks removed the
Return control. Normal Back restored Alpha without reviving that stale action.
Cmd-Q exited, ports 24462/24463 closed, and all 10 canonical fixture files matched
the baseline. Four model regressions supplement these pointer checks. Native
resize/reflow, keyboard activation, accessibility and IME were not verified here.
Ordinary vault editing remains disabled.

### Selected-day Previous/Next

- Model coverage: civil month/year/leap/DST-date stepping, range limits, stale
  controls/results including ABA, all-section reset and busy/retry state.
- Pending Mac QA: select a Home day; Previous/Next updates notes/tasks/events to
  one date, including empty days. Repeat rapidly with slow/failing reads; old
  rows must not appear under the new date. Check both disabled range boundaries.
- Verify scope and accepted display zone remain unchanged, and menu/dialog,
  composition, dirty-editor and quit ownership block input. Actual GUI/IME,
  keyboard and accessibility are unverified. No new mutation endpoints.

Native macOS pointer smoke on 2026-11-15 used an isolated read-only fixture.
Today showed two activity notes plus its scheduled task and all-day event.
Previous selected yesterday with no activity and its own task/event; another
Previous showed the completely empty day without old rows. Next twice restored
each date and its matching results. The latest-day Next and earliest-range
Previous controls were disabled, and clicking the earliest boundary did not
change the date. Display zone remained UTC. Cmd-Q exited, both owned ports
closed, and all 12 canonical fixture files retained their baseline hashes.
Rapid/delayed/failing native reads, scope/owner interruption, keyboard, narrow
layout and actual IME remain unverified. Rust tests: 658 passed, 7 ignored;
formatting, native build and help-site generation passed.

### Typed property internal links

- Model checks cover exact types/up suppression, duplicate order, malformed props,
  property-only targets, heading/unsupported fragments, URI no-external dispatch
  and stale source/row/verification callbacks.
- Pending native checks: property-only note and heading clicks, Back to source,
  plain up, repeated/Unicode/long values wrapping, unresolved inert rows, source
  replacement and dirty/dialog/composition/quit guards. No GUI/IME/keyboard or
  accessibility outcome is claimed yet; ordinary vault writes remain disabled.

Native macOS pointer smoke on 2026-11-15 used an isolated read-only fixture
whose source body contained no links. Its metadata-only target opened the expected
note, and Back returned to the source. A heading target opened the same note with
its heading visible; the short body fit the viewport, so exact fragment scrolling
was not established. Duplicate RELATED values retained their order. A missing
link and plain URL each remained inert when clicked. A plain string UP remained
visible; a child's typed link UP appeared as its parent breadcrumb without a
duplicate property. Cmd-Q exited, both owned ports closed, and all 12 canonical
fixture files retained their baseline hashes. Rust tests: 664 passed, 7 ignored;
formatting, native build and help-site generation passed. Narrow/Unicode layout,
source replacement and dirty/dialog interruptions, keyboard/accessibility and
actual IME remain unverified. Ordinary vault editing remains disabled.

### Journal On this day candidate (native validation pending)

- Models cover daily/leap-date eligibility, own-vault/date/launch response checks,
  qualified self exclusion and engine order, 100-row display boundary, source/tab/
  revision/retry invalidation and empty/partial/error/malformed-null distinction.
- Pending native: daily journal shows exact activity order, result click + Back,
  month/year/ordinary-note absence, named-vault independence from Home, empty and
  partial/failure Retry, long Unicode titles in short/narrow reader aside.
- Dirty, save, recovery, dialog, actual IME, keyboard and quit-interruption flows
  are not newly verified. No build/test execution is claimed by the cloud worker.
  Ordinary vault writing remains disabled; no new mutation endpoint is used.

Native macOS pointer smoke on 2026-11-15 reused the isolated read-only agenda
fixture. Daily journal 20261115 showed its two activity notes in engine order.
Opening the first row navigated to the expected normal note, where the journal
section was absent; Back restored the journal and both rows. Month journal 202611
and year journal 2026 had no On this day section. Cmd-Q exited and both owned
ports closed; all 12 canonical fixture hashes remained unchanged. Native empty,
error/Retry, foreign-vault, long-list, interrupted/IME and keyboard/accessibility
scenarios remain unverified. Rust tests: 669 passed, 7 ignored; formatting, native
build and help-site generation/text inspection passed. Normal vault writes stay
disabled.

### Focused reader j/k candidate (native validation pending)

- Models: lowercase/no-modifier admission, repeated three-line movement, both
  edges, 72px fallback, invalid geometry, plain-block/click/focus/source rejection.
- Pending native: click long plain prose, j/k and held repeat, top/bottom clamp,
  Escape then no movement until a fresh eligible click; link/code/table/input
  focus must retain keys. Check navigation/reload/resize and competing owners.
- Actual IME and selection/input interruption remain unverified. This scoped
  prose path adds no input handler, clipboard, persistence, fetch or vault write.

Native macOS reader smoke on 2026-11-15 used a long, isolated read-only fixture.
An initial extra paragraph wrapper narrowed the body incorrectly; attaching the
click listener to the existing row fixed that layout before publication. In the
corrected native build, clicking plain prose enabled j/k movement of about 89px
per press. Repeated separate presses accumulated immediately, both boundaries
clamped, Escape disabled movement, and a fresh plain click restored it. Shift+j
was inert. Search received input through the active IME rather than moving the
reader; literal ASCII input and complete IME composition were not established.
A held-key tool call produced one visible step, so OS key-repeat behavior is not
claimed. The native Quit menu exited; all 10 canonical fixture files retained
their hashes and both owned ports closed. Rust tests: 675 passed, 7 ignored;
formatting, build and help-site generation passed. Link/code/table exclusion,
navigation/resize invalidation, selection, full keyboard/accessibility and
interrupted owner flows still require native coverage. Normal vault writes
remain disabled.

### Native View Back/Forward

- Models cover unique bracket bindings, Home/Tag/Graph boundaries, runtime owner
  blocking and focus fallback without taking existing/background focus.
- Pending Mac: native menu enabled/disabled state, no-focus Home and full graph,
  one navigation per bracket press, tag history and About/background isolation.
  Check reader Escape does not revive j/k, input selection/marked text, dirty/save/
  recovery/creation dialogs and quit interruptions. No actual IME claim yet.
- No new route, read/write endpoint, persistence or Reload action.

Native macOS pointer smoke on 2026-11-15 found retained focus after Back to Home
left Forward unavailable. Repairing only known unmounted focus handles fixed
that path; live local/full graph and input handles remain excluded from repair.
The corrected build verified View Back from Alpha to Home, disabled Back at the
boundary, enabled Forward and successful return to Alpha without a reader click.
Full graph Back restored Alpha; opening Search disabled both native history
items. A synthetic Cmd-[ did not visibly navigate with the active IME, so native
bracket delivery is not claimed. Tag/About, live local-graph focus retention and
interrupted/dirty flows still need native coverage. Quit exited; all 10 canonical
fixture hashes stayed unchanged and both owned ports closed. Rust tests: 680
passed, 7 ignored; formatting, native build and help-site generation passed.
Independent final focus review found no P1/P2 issue. Normal vault writes remain
disabled, and full IME composition remains unverified.

## Raster enlargement

Native macOS pointer smoke on 2026-11-15 used two generated geometric PNGs in a
new isolated read-only fixture. Landscape and portrait overlays retained the
whole red border and square grid proportions. Content click and wheel left the
first overlay and background unchanged. Close, Escape and backdrop dismissal
worked; the corrected build retained the bottom-of-reader position after
portrait Escape/backdrop dismissal. Native history items were disabled, and
Quit from the open overlay exited with both owned ports closed. All 12 canonical
body/sidecar/asset hashes stayed unchanged.

Review caught overly broad Quit blocking, editor-follow continuing behind the
modal and removal of Enlarge rows changing reader height; all were corrected
before publication. The final diff passed independent P1/P2 review. Rust tests:
685 passed, 7 ignored; formatting and native build passed. Source/allocation,
admission, fit and Quit-gating models are covered. Reuse of the accepted image
Arc and absence of new read/decode calls were source-reviewed, not measured with
a live request counter. JPEG, resize/small-window, full keyboard/AX, source-change
and IME/dirty/recovery combinations still need native coverage. Ordinary vault
writes remain disabled.

## Inline raster aspect-ratio correction: native validation pending

Inline reader images now use one natural aspect ratio with a coupled 620px-width / 480px-height cap, then shrink to the available parent width without upscaling. Model tests cover portrait, landscape, square, tiny and narrow-parent geometry. Verify tall PNG/JPEG and landscape images in wide/narrow windows; verify Enlarge and Close preserve the unchanged reader layout. No loader, decoded allocation, Enlarge geometry or write gate changes. Cloud diff-check passed; Rust/build/native checks not run here.

Native macOS inline correction smoke on 2026-11-15 verified the generated
landscape raster at approximately 568×331px inside a parent narrower than the
620px cap, and portrait at approximately 206×480px. Square grid cells and the
full border retained natural proportions. Portrait Enlarge/Close retained the
reader offset. A window-edge drag did not change the tiled window dimensions,
so live resize and still narrower windows remain unverified. Quit exited, both
owned ports closed, and all 12 canonical fixture hashes stayed unchanged.
687 Rust tests passed, 7 ignored; formatting and native build passed. Independent
review found no P1/P2 issue. Normal vault writes remain disabled.

## Copy title: original checklist and bounded native evidence

Check explicit Copy title submits exact Unicode/plain title, preserves focus, wraps long titles and gives renewed “Copy submitted” feedback on rapid clicks. Navigate/Back/reload while a timer is pending; stale timers/callbacks must not affect another title. Pending/error, Home/Tag/Graph, Enlarge, Find/search, composition, editor/save/recovery and quit owners must not copy. No Cmd-C hook or clipboard readback is introduced. Three model tests were added before the bounded native run below. Unchecked cases in this checklist remain unverified.

Native macOS pointer smoke on 2026-11-15 verified Copy title on the isolated
Raster Enlargement QA note, its submission acknowledgement, and exact plain-title
round trip into the fixture search input using the system paste action. Feedback
returned to Copy title. Search preserved the title-row footprint and reader
position; native Quit exited and both owned ports closed. All 12 canonical
fixture hashes stayed unchanged. The clipboard intentionally contains the test
title; no prior clipboard was read. Long-title wrapping, rapid clicks, keyboard
activation, accessibility, interruption and actual IME remain unverified natively.

Independent review identified layout instability when overlays removed the
button or the feedback label changed width. The accepted note retains a fixed
button footprint, with action-time guards disabling unavailable copies. The final
Rust suite passed 690 tests, with 7 ignored; formatting and native build passed.
Ordinary vault writes remain disabled.

## Authored block links: native validation pending

Verify same-note, cross-note and metadata `#^ID` links reach terminal paragraph/list anchors; repeated IDs choose first, case differs, missing target reports no jump. Check Unicode/emphasis/link prose, ordered/task indentation, Find offsets and code Copy remain correct; bare/invalid/code/table markers remain visible. Navigate/reload/resize/scroll/open another owner while a jump is queued; old callbacks must not move replacement readers. Check Back and canonical fixture hashes. Six new model tests; cloud diff-check passed, Rust/native/IME not run here.

Native macOS block-link smoke on 2026-11-15 used two CLI-created disposable
notes. Cross-note paragraph and tight-list jumps, first-duplicate selection,
Back to the source and same-note paragraph reveal worked without further input.
Paragraph/list markers were hidden while Unicode, emphasis, ordinary links,
numbering and task indicators remained visible. A missing block retained the
existing destination position and reported no fragment jump. Quit exited; both
owned ports closed and all 10 canonical fixture hashes stayed unchanged.

Independent review found a protected-link whitespace range issue and an extra
unguarded frame inside the native ScrollAnchor API. Marker projection now refuses
protected-range truncation; block navigation applies measured positions directly
inside its guarded callback. The final Rust suite passed 697 tests with 7 ignored;
formatting and native build passed. Full Find, metadata links, case-distinct and
ordered/task activation, interrupted/resize, keyboard/accessibility and actual
IME scenarios remain unverified natively. Arrival highlighting and centered
placement are not implemented; current targets reveal at the reader top, clamped
to available scroll extent. This is a bounded subset, not full block-link parity.
Ordinary vault editing remains disabled.

## Block arrival cue: native validation pending

After successful same/cross-note block reveal, verify the destination has a subtle
background for about 1.5 seconds and that repeated jumps renew it. Failed/missing
jumps must not start a cue; navigation, a newer jump, Find/search/modal or source
change must clear it without later revival. Check that cue appearance/expiry does
not move content, change Find/style/link ranges or create a history entry. Three
model tests added; cloud diff-check passed, Rust/build/native checks not run here.

Native macOS cue smoke on 2026-11-15 verified a cross-note paragraph arrival in
dark mode and a tight-list arrival in light mode. The correct destination row
received the background cue, which expired without changing text layout or reader
position. Back returned to the source. Native Quit exited; both owned ports closed
and all 10 canonical fixture hashes stayed unchanged. The final Rust suite passed
700 tests with 7 ignored; formatting and native build passed. Independent review
found no P1/P2 issue. Rapid/repeated cue replacement, modal/IME interruption,
keyboard and accessibility remain unverified natively; source/timer invalidation
has model coverage. Centered placement is still unavailable. Ordinary vault writes
remain disabled.

## Native Find menu: disposable read-only smoke

On macOS, a two-note fixture with three English and three Japanese matches separated
by 16 prose paragraphs verified Edit → Find opening without a prose click, initial
empty-query Next/Previous disabling, pointer Next 1→2, Cmd-G 2→3→1 and
Shift-Cmd-G 1→3. Repeated Cmd-F selected the retained query without advancing.
Pasted Japanese text survived closing/reopening; this is not IME-composition proof.
Done closed the bar. Cmd-G with a closed bar resumed the existing search behavior
at its first result rather than continuing the previous active result. An absent
query displayed No matches; clearing the query disabled native Next/Previous.
Home, global Search and the active About window disabled all three Find commands.
About closed normally and the native Quit menu exited the fixture.

All 10 canonical fixture files stayed unchanged and both owned ports closed.
704 tests passed (7 ignored), formatting and debug build passed; independent review
found no P1/P2 issue. Actual IME composition, inactive/reactivated-window races,
Graph/editor/image-modal interactions, stale-dispatch races, full accessibility,
and local Enter/Shift-Enter delivery remain unverified natively. Existing safety
and source guards remain in force; normal vault writes stay disabled.

## Reader Hierarchy panel: disposable read-only smoke

A five-note official-CLI fixture verified Root → Child → Grandchild and Leaf under
Root, with Unplaced absent from the engine hierarchy. In a narrow 680px native
window, opening the rail panel from Unplaced near its bottom preserved paragraph
27 at the same position. Roots and direct children initially appeared; expanding
Child revealed Grandchild. Root and leaf had no interactive collapse controls.
Panel wheel input, j and Cmd-F left the underlying reader unchanged. Escape,
explicit Close and outside-click dismissal each retained the same reader position.
Reopening resets non-root expansion; no persistent expansion is claimed.

Grandchild title activation closed the panel and opened the correct note; Back
returned to Unplaced at the earlier position. Root title independently opened Root.
Home still displayed its original inline, initially collapsed hierarchy. Native Quit
exited normally; all 16 canonical files were unchanged and both owned ports closed.

709 tests passed (7 ignored), formatting and debug build passed. Independent review
found a ScrollHandle replacement risk on opening; this was corrected by reusing the
existing overlay pause without replacing the handle, then the near-bottom checks
above passed. Re-review found no remaining P1/P2. Actual IME, interrupted reads,
scope/storage replacement, large/paged tree wheel behavior, repeated labels and
full accessibility remain unverified natively. No ordinary vault write gate changed.

## Copy MD: disposable clipboard smoke and remaining coverage

Use an isolated accepted note with Unicode/ordinary Markdown, aliases/empty aliases, heading/block fragments, numeric targets, nested labels, code literals, valid include options, malformed/escaped syntax and table cells. Explicit Copy MD should submit portable raw-body text and renew “Copy submitted” feedback without focus/reader movement. Paste only into an authorized fixture control for a roundtrip; do not inspect the prior clipboard. Check navigation/reload and rapid-copy timers, disabled modal/input/IME/quit/recovery states and >256 KiB refusal without truncation. Unknown include tails, midline includes and code intentionally remain literal. Nine model tests added; 718 Rust tests passed (7 ignored), formatting and debug build passed. Independent review found a reference-definition URL preservation issue; reference spans and conservative duplicate/continuation protection were added, then re-review found no remaining P1/P2.

A disposable two-note fixture verified explicit Copy MD and transient submission feedback. A plain-text editor sink in the fixture directory received Japanese text, normal Markdown/table/link syntax, flattened alias/fragment/numeric targets and valid include labels exactly as expected; inline/fenced code, malformed wiki text, unknown include suffix and midline include remained literal. The saved sink matched the expected conversion of the accepted HTTP body, whose terminal newline is omitted by the engine. Reader content position did not move when copying. All 10 canonical files stayed unchanged and both owned ports closed after native Quit and the owned response probe. No prior clipboard was read.

Rapid timer races, source-switch/oversize refusal, full owner combinations and actual IME remain model-only or unverified natively. This is not full clipboard, accessibility or native editing acceptance.

## Reader body-link hover: model coverage and limited native smoke
Source/revision/tab/content/offset invalidation, shared worker lifetime, response identity/size and rest/grace/card-hold/stale-close have model regressions. On macOS, 725 Rust tests passed (7 ignored), formatting and debug build passed; independent review found no P1/P2 issue.

On a disposable read-only fixture, check fast pass versus 260 ms rest; move into whitespace within the same row before rest; move from link to card during 220 ms grace; scroll only the card; leave and Close. Switch targets rapidly, resize/reflow or scroll the source during a pending timer, then navigate or open another owner. Check fragment click exactly once and Back, unverified/external/local-heading links inert for preview, and graph pins retained with reader hover suppressed. Verify the source position, history and canonical hashes remain unchanged by hover/Close. Loading/error/partial/busy responses must not masquerade as empty content. Current automation lacks documented pointer-move-only support, so do not claim actual hover timing/grace acceptance from click or model tests. Actual IME, selection and accessibility interactions remain unverified.

A disposable three-note native fixture verified normal resolved-link navigation, cross-note fragment arrival with its Destination marker visible, Back restoring the source and unresolved-link clicks remaining inert. No preview overlay stranded the reader. Native Quit exited; all 12 canonical files were unchanged and both owned ports closed. These click regressions are not hover acceptance: pointer rest, link-to-card grace, card-only scrolling and rapid interruption still require native verification. Ordinary vault editing stays disabled.

## Native View Reload: limited read-only smoke
Five model tests cover active/ready/owner/focus admission, accepted source provenance for retry, stale pending-token completion, recorded/safe Home routing and exactly one Cmd-R binding (no bare/modifier aliases). On macOS, 730 Rust tests passed (7 ignored), formatting and debug build passed.

On the isolated read-only fixture: open a non-first note, invoke View → Reload and Cmd-R separately, verify the same route/title and Back/Forward history (no extra visit). Check ready Home, disabled initial loading, rapid repeat while pending, Search/Find/input and modal exclusion, and About/inactive windows. Exercise recoverable list/body failure and retry with retained accepted identity separately. Record unchanged canonical hashes and owned-port shutdown. Dirty/IME and failure combinations remain unverified unless explicitly exercised; no ordinary-vault writes or auto refresh are authorized.

A disposable two-note fixture checked View Reload and Cmd-R on the accepted second note without first clicking prose; its title/route remained, and one Back returned to Home without a duplicate visit. Established Home reload retained Home, and Search input disabled the menu. Initial route-less Home without safe-session mode was disabled. Review of this case added recorded-Home/safe-session admission without changing reload routing; that additional branch has model coverage, not a second native run. All 10 canonical files were unchanged and both owned ports closed after native Quit. Exact HTTP dispatch counts, rapid repeats during pending reads, injected failures, dirty/IME/About and interruption cases remain unverified natively. Ordinary vault editing stays disabled.

## Plain-paragraph selection: limited native smoke
Ten new model tests cover strict plain-only eligibility, UTF-8/grapheme endpoints (Japanese, combining accents, emoji), forward/backward/empty/outside ranges, 256 KiB limits, exact linebreaks, source/geometry invalidation and feedback tokens. macOS formatting, debug build and 740 Rust tests passed (7 ignored). The additional regression covers synthetic leave during repaint without consuming or clearing the range.

Use only an isolated read-only fixture: drag a partial wrapped paragraph forward and backward, inspect visible highlight, click Copy selection and independently verify exact clipboard roundtrip. Single click must still acquire existing j/k reading ownership; empty drag must not write. Drag away and back to the origin must not trigger that click path. Test paragraph padding/another block/outside window, Escape/outside click, exact Copy-button mouse-down preservation, repeated copy, resize/reflow/scroll/navigation/another owner before Copy, and rapid old callbacks. Link-containing/styled paragraphs remain unselectable and normal link click/hover remains unchanged. Preserve source offset, history and canonical hashes. Native partial-selection, actual IME and accessibility acceptance must not be claimed without direct evidence.

An isolated read-only fixture verified visible forward selection of `Alpha 日本語`, explicit Copy selection submission, and Cmd-V into fixture Search displaying that exact visible string. Reverse dragging across soft-wrapped lines highlighted the intended continuous range without moving the reader. Escape cleared the range and disabled Copy; ending a drag outside the paragraph left no selection. A normal link still navigated to its target. This is a visible clipboard roundtrip, not a byte-level clipboard dump. All 10 canonical files were unchanged and both owned ports closed after menu Quit. Repaint-generated hover leave initially canceled native drags; retaining the validated measured layout fixes that case. Rapid source changes, repeated copying, all reflow/scroll/owner cases, stationary-click j/k, actual IME and accessibility selection remain unverified natively.

## Search-result tag refinement: limited native smoke
Six new pure/input-model tests cover exact duplicate no-op, whitespace/case/Unicode preservation, bounds, composition, and stale query/result lifecycle. macOS formatting, debug build and 746 Rust tests passed (7 ignored). On an isolated fixture, click `#a` and distinguish engine matches `a`, `a/b`, `ab`; verify query remains open, note/route/history/scope unchanged, no forced input focus, repeat no-op, newer query/IME rejection, and title/keyboard note activation unchanged. Snapshot tags must stay inert. Confirm canonical hashes; actual IME and unexercised native event combinations remain unverified.

Search-tag fixture smoke: `Refine` returned four notes. Clicking `#a` retained the query as `Refine #a` and narrowed results to `a` and `a/b`, excluding `ab` and `other`; search stayed open and the reader route stayed unchanged. A repeated tag click did not duplicate the term. Title click and separately observed Down then Enter both opened the expected result. An initial rapid combined-key attempt was inconclusive; it is not evidence of rapid-input acceptance. All 14 canonical files remained unchanged and owned ports shut down. Native IME, foreign-vault and stale-response injection remain unverified.

## Explicit in-reader includes

The owned render response can replace verified standalone include rows inside the
accepted host reader after an explicit Show action. Excerpts are inert snapshots:
no source navigation, images, nested requests or mutations. Host outline and
Copy MD remain tied to the original source; Find and plain-text selection are
unavailable while expanded. Collapse is required before refresh. One appended
terminal LF is accepted only when bounded authored block models remain equal;
all other source transformations and diagnostic-bearing includes retain the
original body with an inspector notice.

A disposable fixture verified whole-note, heading/only-contents and line-range
cards between unchanged host passages, literal nested includes, host-ending
outline navigation, Cmd-F rejection while expanded and collapse back to source.
The first native attempt exposed the engine's added terminal LF; the narrow
regression covers extra LF, CRLF, leading LF, trailing space and newline removal.
Native testing also exposed a 432-pixel passage jump when restoration ran during
prepaint. Deferring the guarded offset update until the next frame fixes the
observed case. Expansion and collapse through the persistent reader control
kept the host ending at the same position after an outline jump, and kept a
naturally scrolled passage in place in both directions. Two regression tests
cover the offset calculation and rejection after scroll/geometry changes.
Formatting, debug build and 753 tests passed (7 ignored). All runs preserved
all 10 canonical files and closed both owned ports after Quit. Resize/scroll races,
foreign-vault origins, rich content, native Copy MD roundtrip and actual IME
remain separate acceptance checks; this is not full automatic transclusion.

### Recent notes candidate

Native validation pending: open disposable A/B/A, close A's tab and reopen it from
Recent; verify newest-first ordering and ordinary Back/Forward independently.
Check empty and scrollable lists, vault-qualified same-ID entries, named scope
filtering, unavailable targets, and rapid route/registry/scope changes. Near the
bottom of a note, Escape and outside dismissal must preserve the visible passage.
Background navigation, Find, editor/creation uncertainty, and composition ownership
must not be bypassed. Verify canonical hashes unchanged and no recent-history disk
I/O. Restart continuity and native IME interaction remain separate unverified gates.

A three-note disposable fixture verified Alpha/Beta/Alpha produces Alpha then
Beta without duplicates; closing Alpha retains its row and clicking it reopens
the correct note. Escape closes the popup. Opening it over a scrolled Alpha,
scrolling the backdrop, and outside dismissal leave the visible passage in place.
Cmd-W while the popup owns focus did not close a tab; Cmd-Q still exited through
the ordinary shutdown path. All 12 canonical files remained unchanged and both
owned ports closed. macOS formatting, debug build and 761 tests passed (7 ignored).
The initial Home-only empty state, long-list scrolling, foreign scopes, injected
stale responses, restart and actual IME remain unverified natively. Review found
and fixed non-user Reload/startup visits and missing modal input containment
before this smoke; those background admission branches have model coverage.

### Weekly tasks candidate

Native checks pending: open Weekly tasks in disposable Home; inspect seven civil
days, scheduled/due deduplication, excluded completed rows, current-week Overdue,
empty days, Previous/Next/This week, and snapshot reuse without per-day fetches.
Open a source row and return with Back. Scope, registry, storage, route and request
changes must reject old completion/row controls; rapid week changes reject old
selection callbacks. Check incomplete data and truncation notices. Verify canonical
hashes unchanged and no session writes. Monday starts and full Calendar are outside
this slice; real IME remains separately unverified.

### Weekly task agenda: isolated native smoke

A disposable three-note launch vault verified the explicit Home weekly panel.
The current Sunday-start week showed one same-day scheduled/due task once,
a separate task on both its scheduled and due days, and an overdue task only
in the current-week Overdue section. Completed and undated tasks were absent.
Next week showed the expected future task without Overdue; previous week showed
seven empty days; This week restored the current projection. A task row opened
the correct note, Back returned to Home, and reopening fetched a fresh panel.
The bounded panel scrolled to Saturday without moving its controls or the Home
search field. Collapse and application Quit succeeded. All 12 canonical fixture
files remained unchanged and both owned server ports closed.

This is not native coverage of foreign-vault scope races, partial responses,
midnight rollover, dense lists, accessibility, or IME. Those remain separate
validation gates; ordinary vault editing and session persistence remain disabled.

### Isolated PDF renderer feasibility (not a reader feature)

The internal macOS helper uses CoreGraphics with an in-memory data provider. It
runs before logger, UI, vault, session and engine initialization. It accepts no
file path or URL. No reader asset fetching or PDF controls are connected yet.
No new crate or external service was added.

Local generated fixtures verified upright portrait text/vector geometry, landscape
Japanese text, and separate page 1/page 2 counters and pixels. Invalid signature,
malformed document, encrypted document, excessive input and an out-of-range page
were rejected without an output frame. The observed BGRA frames were decoded and
visually inspected independently of the app UI.

Model/process tests cover byte/request/geometry/frame bounds, a non-reading child,
cancellation during input, deadline, excessive output and nonzero exit. The runner
owns one child at a time, uses nonblocking pipes, and kills/reaps on unsuccessful
exit. Limits are 8 MiB input, 256 pages, 1536 pixels per edge, 2 million output
pixels and a 5-second parent deadline; a 3-second CPU limit is secondary. This is
process isolation, not an OS security sandbox or a hard resident-memory limit.
PDF links, scripts, forms, annotations, external assets and session state are not
executed or interpreted by this helper. Interactive reader acceptance is pending.

## Local PDF inspector candidate (pending)

Use isolated CLI-imported local portrait/landscape/Japanese PDF assets and an
accepted note with standalone image-style references. Check explicit load,
Previous/Next endpoints and accepted counters, pending previous page identity,
resize aspect fit, corrupt/encrypted/oversize/missing errors, same filename in
another vault, rapid scope/tab changes and close during render. Reader passage,
Find/input ownership and normal Back must remain unchanged. Compare canonical
note/metadata/asset hashes; no ordinary session or PDF-cache writes are allowed.

### Local PDF inspector validation (2026-11-15)

- Isolated markerless fixture: explicit local Load rendered Japanese landscape
  page 1 and red page 2; Previous restored page 1. Pending state retained the old
  accepted counter, and endpoint controls were disabled. Portrait single-page
  output preserved its ratio; rail scrolling reached its controls without moving
  the source reader. Encrypted input returned a nonfatal rejection.
- Close after Load removed the image; Home navigation and tab return did not
  restore stale PDF state. Original source remained literal and unchanged.
- Native QA found empty canonical launch-vault names were rejected by the new
  transport. Explicit empty `vault` queries now remain source-qualified, matching
  the owned engine registry contract, and have a regression test.
- Independent review found retained-body admission during Reload. Identity now
  requires the accepted request, exact current note, and ready storage generation.
- All 13 fixture note/sidecar/asset hashes remained unchanged; owned ports closed
  after Quit. No ordinary vault was opened or written.
- Cross-vault/native reload races, resize stress, accessibility and actual IME
  composition were not verified. No claim of full PDF embed parity is made.

## Static PDF enlargement native check

- Disposable read-only PDF fixture: Japanese landscape page 1 enlarged with its
  aspect preserved; accepted page 2 enlarged as page 2, without resetting the
  inspector. Escape, Close and backdrop dismissal preserved the underlying page.
- Find, reader j and an image click did not route or advance behind the overlay.
  Guarded Quit remained available while the overlay was open.
- The accepted inspector control retains its footprint while disabled behind the
  overlay, avoiding a scroll-height change when the aside is in the reader.
- All 13 fixture note/sidecar/asset hashes remained unchanged and both owned ports
  closed after Quit. No ordinary vault was opened or written.
- Narrow-window bottom-scroll retention, resize stress, native storage races,
  accessibility and actual IME composition remain unverified. The overlay reuses
  the accepted image by construction; native request counts were not measured.
  This is a static snapshot, not a separately paged PDF viewer.

## PDF accepted-page reader projection

- Disposable fixture with 35 prose passages and two identical PDF references:
  the first loaded occurrence displayed its Japanese page below its visible
  source; after Hide and loading the second occurrence, only that location gained
  the projection. Other occurrence/Close controls required Hide first.
- Page 2 was accepted while projected. After naturally scrolling below the first
  PDF, repeated Hide/Show preserved the same visible passage and vertical offset.
- Find after expanding the later occurrence highlighted the visible tail passage;
  source labels and surrounding text remained available. Guarded Quit succeeded.
- All 13 fixture note/sidecar/asset hashes remained unchanged, and owned ports
  closed after Quit. No ordinary vault was opened or written.
- Narrow layouts, bottom-clamp transitions, page-error injection, source/scope
  races, clipboard/heading navigation and actual IME remain unverified. No extra
  fetch/decode is present in Show/Hide; request counts were not instrumented.
