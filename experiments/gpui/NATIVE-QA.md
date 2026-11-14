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
| Recovery boundaries | Failed/uncertain writes retain the in-memory draft | Verify the visible recovery actions and clearly disclose memory-only recovery; forced kill/crash recovery is not implemented |
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
  cells. Block/cell anchoring does not establish exact matched-range scrolling.

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
