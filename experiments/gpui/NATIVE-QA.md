# Native validation log

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
