# Virtual Workspace keyboard regression

`nix develop --command ./experiments/gpui/qa/check-keyboard-ui.sh` runs the actual
Workspace renderer, SearchInput, focus tree, pointer/key dispatch, tab activation
and reader scroll handles in the pinned GPUI `TestPlatform`. Run
`./experiments/gpui/prepare.sh` first. CI uses Rust 1.100.0 and macOS 15.
The opt-in `ui-tests` feature enables upstream GPUI test support; ordinary builds
keep their existing features. The added lock entries include upstream test
support's Linux backends even when these tests run on macOS.

Each case starts the existing test-only Go bridge after exact owned-process and
v2 manifest/nonce capability admission. The generated registered-vault fixture
supplies three long notes. No default vault, user configuration or native OS window
is opened. Protected file contents, modes and modification identities are
compared after explicit owned-process shutdown. TestPlatform has a noop text
system and virtual clipboard/windows; it does not activate AppKit.

The seven cases cover:

- Click actual prose, scroll with j, open Search through its rendered rail
  control, type j/k without scrolling or closing the popup, then Escape and
  continue j/k without clicking again.
- Repeat dismissal by clicking outside the rendered Search panel.
- Switch actual note tabs with Tab/Shift+Tab; keep Search text input separate;
  inject the real preedit callback and verify Escape/Tab key events cannot
  dismiss composing Search or switch notes. Unmark and verify normal routing.
- Open a Search result through Enter; the new note cannot inherit the prior
  reader owner and needs its own prose click.
- Resize while Search owns input. Dismissal must reject the old reader geometry;
  only a new prose click may reacquire scrolling.

- Continue Tab/Shift+Tab across three notes while MRU strip order changes.
- Hold the existing recovery-cleanup refusal flag in the real Workspace, refuse
  a mid-sequence Tab, release the flag, and retry without another click. The
  previous implementation reopened note 42 instead of the requested note 43.
  This injects a guard state, not a filesystem recovery failure or edit grant.

Draft Cancel, Save success/failure and Discard are additionally checked through
real EditSession/PendingTransition state. Their actual native sheets and input
remain separate unverified paths. Failing virtual assertions now explicitly
reap the owned fixture process and compare protected files before rethrowing.

The first case failed before the fix: GPUI had blurred the reader during mouse
down, so the rail's click callback captured the workspace root as its return
focus. Ordinary pointer invalidation also discarded reader keys. Return focus
and the existing reader owner are now captured on mouse down. Dismissal restores
only that owner after checking the exact note/request, body, storage/launch,
scroll epoch, focus, availability and geometry. The Escape action consumes its
key instead of allowing the root's independent Escape handler to run again.
A Search result navigation clears the saved reader owner.

The test initially crashed because it called `quit` through GPUI's temporary
borrowed VisualTestContext, which frees itself during its on-quit callback.
Cleanup belongs to the outer `#[gpui::test]` context; no dependency patch or
production workaround was added. A synthesized Tab also generates platform text
in GPUI's keystroke helper, so composition/navigation isolation uses actual raw
KeyDown events and injects preedit separately. This does not emulate a real IME.

These tests do not establish native pixels/font shaping, AppKit event delivery,
physical keyboard/mouse behavior, IME candidate windows/commit, accessibility,
clipboard integration, or dirty editor sheets. The older observation of Search
closing while typing was not reproduced by these cases. Foreground native QA of
those paths remains required, using disposable data and an isolated desktop.

The body-read error cases use an actual Go 404 after opening a missing note.
A successful Search does not erase that note's failure. Closing the failed tab
to Home removes its warning. Choosing a healthy note in the same UI update
that schedules an older failing request exercises stale completion isolation;
a separate workspace/list diagnostic must not be attributed to the note or
cleared by that body request. These are virtual-window tests, not AppKit.

A deferred-delivery case holds an already successful, admitted Go note response
before Workspace applies it. Releasing it for the same note fills the reader
without recording a new visit. In a second read, Home and a healthy note can
settle while the fetched response stays pending; releasing it later cannot
replace the healthy reader or history. This bounds UI delivery ordering, not
the Go HTTP deadline, blocked storage, or native window responsiveness.
