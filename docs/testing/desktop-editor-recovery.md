# WKWebView React editor recovery regression

On macOS with the existing Go, Node, Python and Swift SDK toolchain:

```sh
make desktop-app
python3 scripts/desktop-editor-recovery.py build/desktop/Track.app/Contents/Resources/track
```

This standalone integration test builds a small AppKit harness from the production
`WebViewCoordinator`, `TrackServerProcess`, and `TrackWebCore` sources. It loads the
built live React frontend from the real Go server. No React internals, mock API,
injected replacement editor, persistent WebKit store, or production vault is used.
The coordinator initializer retains the application's original fixed-port defaults;
the harness asserts those defaults before injecting its own loopback origins.

The Python driver creates a synthetic note with `track new`, config and cache under one temporary
directory. It reserves an available adjacent loopback port pair until launch. The
supervisor requires its child's unique readiness token as well as HTTP readiness,
so a competing bind causes failure instead of adopting another server. Stop and
cleanup operate only through owned process handles. The driver verifies that both
listeners and the temporary PID lease disappear after the harness exits.

The probe checks these transitions:

1. Open the fixture note through React, select Edit, and dispatch input through the
   actual textarea's native setter and bubbling input event. Check the dirty tab
   marker and enabled Save button, then switch Preview/Edit to remount the textarea
   and prove the draft lives in React state. The server still has the original body.
2. Reload through WKWebView, return Cancel through the coordinator's existing
   confirmation callback, and check that the document and draft remain intact.
3. Stop and reap the owned Go child with the production supervisor. Click Save while
   the server is absent; require the real save error and retained dirty draft.
4. Start a new owned Go child and call the production `serverDidRecover`. Check the
   same document/draft and unchanged stored body. Append text, click Save, require
   the clean Saved state, and compare the API body with the full expected draft.
5. Remount the textarea again and compare its saved state. Independently compare
   exact UTF-8 file contents, including Japanese, an accent, punctuation and newlines.
   The API comparison uses the editor body; the file comparison includes the writer's
   required final newline.

This improves regression coverage; it does not claim to fix a production data-loss
bug. The older `desktop-recovery-smoke` remains a document/popup survival smoke and
is not evidence of React save correctness.

## Coverage boundary

This harness exercises the production recovery coordinator and process supervisor,
but does not instantiate `AppDelegate`, dispatch the native Retry button, or click
native confirmation panels. Cancel is supplied through the existing coordinator
callback; window-close/quit/back/forward modal interaction is outside this test.
The interruption is a controlled stop/restart, not a crash/SIGSTOP recovery test.

Concurrent writes producing HTTP 409 and ordering between SSE invalidation and
reconnection need a separate deterministic conflict fixture. They are not covered
by this unchanged-disk scenario. Likewise this does not establish IME composition,
accessibility, security, performance, or persistence across an app restart.
