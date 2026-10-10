# Clean and dirty native termination regression

```sh
make desktop-clean-termination-test
# Or reuse a release build:
python3 scripts/desktop-clean-termination.py build/desktop/Track.app/Contents/Resources/track
```

The driver compiles the production `AppDelegate`, coordinator and process supervisor unchanged
into a standalone AppKit harness. Constructor injection supplies an owned ephemeral loopback
pair and a nonpersistent WebKit store. These are not launch flags or user settings; production
keeps its fixed origins and persistent store. Each run has a separate bundle identifier,
synthetic vault, machine config and cache. No installed application or user vault is selected.
The driver reserves adjacent ports until launch, and the supervisor verifies its child's readiness
token. Cleanup only targets the process group that the driver created, never a process found by
name or port. Both listeners must disappear. A graceful exit removes its PID lease; a force-killed child may
leave only a stale lease naming a dead PID, removed with the temporary fixture.

The actual window delegate receives `performClose`, and the installed native menu receives a Cmd-Q
key equivalent. OS termination is simulated by calling `applicationShouldTerminate`; this test
never asks macOS to shut down, log out or reboot. A test `NSApplication` subclass records replies
and exits after checking that the Go child has already been reaped. The separate existing
`desktop-shutdown-test.py` checks the real AppKit reply-to-exit handoff as well.

| Scenario | Required result |
| --- | --- |
| Unedited note (Preview/Edit): window Close, Cmd-Q, OS callback | No warning; one positive deferred reply |
| Saved note, empty tab strip, multiple clean tabs | No warning; disk bytes unchanged or successfully saved |
| Dirty note: Cancel then Save and retry Close/Quit | One warning, negative then positive reply; exact saved disk bytes |
| Dirty note: Continue | One warning, positive reply; original disk bytes retained |
| Dirty OS callback and repeated requests | One pending check/sheet, one reply per completed attempt |
| No window / startup before first document | No warning; owned server startup is cancelled safely |
| Probe error, missing capabilities, 3.5-second blocked WebKit probe | Conservative warning; Cancel replies once; late clean result is ignored |
| Go child suspended with SIGSTOP | No dirty warning; graceful deadline, force stop and reap precede approval |
| Save pending against a SIGSTOP'd server | Cancel keeps draft; resume completes Save; next Quit is clean |
| IME compositionstart before the buffer changes | Cancel protects composition; compositionend restores clean state |
| Dirty note with two tabs open | Cancel and Save preserve the active draft; clean tabs do not mask it |

Native confirmation buttons are activated through their real AppKit controls. Editor actions use
DOM buttons and native textarea setters with bubbling input events, without React internals or a
mock server. IME events are synthetic; this does not claim testing a physical input method.
The pending-save case suspends and resumes only the fixture's recorded child. Probe failures and
latency are injected only in the disposable WebKit document.

## Red/green and coverage boundary

At base `bca14a2`, `applicationShouldTerminate` and `windowShouldClose` called the synchronous
potential-data-loss alert unconditionally. With only the fixture constructor seam added, the
native `clean-close` case failed with `prompts=1`, `replies=[]` and
`unexpected unsaved-edits warning in clean-close`. Navigation's existing editor probe did not
participate in either path. The regression requires `prompts=0`, `replies=[true]` after the fix.

Unknown pages, voice input, open dialogs and focused embedded frames remain conservative.
An unavailable probe cannot establish a clean editor, so it still warns after a bounded timeout.
The read-only state probe does not add a native command or message bridge. The native Cancel
button leaves the existing web Save action available; this change adds no new save workflow.

Run `web/src/nativeNavigation.test.tsx` for pending/failed saves, current DOM text, a newer edit
arriving during a save, deletion and mount/unmount behavior. Also run
`desktop-editor-recovery.py` for the existing real offline-save/retry flow and disk verification,
and `desktop-verify.sh` for Swift lifecycle/URL-policy and Go regression tests.
