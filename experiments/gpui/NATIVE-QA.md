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
