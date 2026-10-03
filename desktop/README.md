# Track for macOS

`Track.app` is the supported macOS AppKit/WKWebView shell around the existing live Go/web
workspace. Go remains the source of truth for vault behavior and the Vite production bundle is
the UI. The former SwiftUI shell has been retired; see [retirement boundaries](#swiftui-retirement).

## Build and run

From the repository root on macOS:

```sh
make desktop-app
open "build/desktop/Track.app"
```

The release bundle is `build/desktop/Track.app`, with bundle identifier `com.ttak0422.track.web`.
The build path and bundle identifier remain unchanged, so existing desktop website data stays
associated with this shell. Previously built SwiftUI bundles are not removed or overwritten. The
app is unsigned and not notarized; the first launch may require the user to approve it in macOS
privacy/security settings. It is intentionally not App Sandbox constrained. File uploads use the
standard macOS file picker.

The normal launch honors the same configuration sources as `track web`:

- `TRACK_CONFIG` selects the machine config file.
- `TRACK_VAULT` selects a vault path; the configured `default_vault`/`vault_dir` and `$HOME/track`
  fallback continue to work when it is unset.
- `TRACK_CACHE_DIR` selects the derived-data cache directory.
- `open -a "build/desktop/Track.app" --args --vault work` selects a registered vault by name.
- `open -a "build/desktop/Track.app" --args --vault-path /absolute/path/to/vault` selects an explicit
  absolute path. `--vault` and `--vault-path` are mutually exclusive.

Environment variables must be present in the app's launch environment. When launching through
LaunchServices does not inherit a shell environment, run the executable directly, for example:

```sh
TRACK_CONFIG="$HOME/.config/track/config.yml" \
TRACK_VAULT="$HOME/notes/work" \
TRACK_CACHE_DIR="$HOME/Library/Caches/track" \
"build/desktop/Track.app/Contents/MacOS/Track"
```

## Verification

```sh
make desktop-verify
make desktop-smoke
make desktop-recovery-smoke
make desktop-termination-test
```

`desktop-verify` performs a release app build, the Swift lifecycle/URL-policy regression runner, the
Go test suite once, a SIGSTOP-child deferred-termination test, and ShellCheck when installed.
`desktop-termination-test` runs that isolated SIGSTOP test by itself. The
runner is a SwiftPM executable rather than XCTest because the selected Command Line Tools installation
does not ship `XCTest.framework`. `desktop-smoke` launches the actual bundled app and WKWebView with
`TRACK_CONFIG`, `TRACK_VAULT`, and `TRACK_CACHE_DIR` all rooted in a disposable temporary fixture.
It checks that the live React UI appears in `#root`, `/api/vaults` returns HTTP 200, and the API
reports that fixture vault. The smoke test uses a nonpersistent WKWebsiteDataStore and removes its
temporary vault/cache afterward. It never selects a configured user vault or user app.
`desktop-recovery-smoke` additionally exercises external and internal `target=_blank` handling through
the real WKWebView, enters a draft, stops and retries the Go server, then checks that the same document
and draft value survived while API connectivity returned. The popup test suppresses actual browser
launches; production behavior still routes only eligible user-initiated external links to the browser.

The system Command Line Tools SDK is used by default (`/Library/Developer/CommandLineTools` and
`MacOSX26.5.sdk`). Override `DESKTOP_DEVELOPER_DIR` or `DESKTOP_SDKROOT` when building with another
installed macOS toolchain. The frontend is staged in `build/desktop-web-dist`; a Go build overlay
maps those files into `internal/track/webui/dist` only for the app binary. The tracked placeholder
and any existing ignored build assets in that source directory are not rewritten.

## Continuous integration

The `macOS desktop` CI job runs on `macos-15` alongside the Ubuntu checks. It resolves the
runner's installed Xcode/SDK through `xcode-select` and `xcrun`, passes the existing desktop
toolchain overrides, and uses the pinned Nix development shell for Go and Node. The job has a
30-minute limit and requires a GUI login session; missing prerequisites fail the job.

It runs `make desktop-verify` (release bundle, Swift regressions, Go tests, and isolated
SIGSTOP-child termination), then reuses the bundle for the WKWebView UI/API and recovery
smokes. Each smoke uses disposable config/vault/cache fixtures and requires explicit successful
results within its timeout. GUI failures are not skipped or treated as successful checks.

## Runtime boundaries and limitations

- Track starts its bundled Go server at `127.0.0.1:18765`; the Go static-app listener uses its
  existing adjacent-port rule at `127.0.0.1:18766`. Both ports and the `127.0.0.1` origin are fixed
  so WKWebView's persistent settings and tabs survive restart. A collision is shown with a Retry
  action; Track does not choose a new origin or adopt a server that happens to answer on the
  port. The Go Host/Origin guard remains unchanged.
- Only the child process this app launched is signalled. Shutdown is asynchronous, sends a graceful
  termination first, then force-terminates that same child after a deadline, and waits for Foundation
  to confirm its exit before AppKit is allowed to terminate. The Go child also exits when its app
  parent disappears, covering a crashed or forcibly terminated shell.
- A server retry does not reload an already-rendered WebView. It retains the current document and
  dispatches online/focus events so the live query and event-stream clients can reconnect without
  discarding editor-local input. Explicit Reload remains behind the conservative discard warning.
- WKWebView uses its persistent website data store in normal operation. It receives no JavaScript
  message handler, native object, arbitrary shell capability, or custom URL-scheme handler. The
  existing web UI can render sandboxed HTML embeds, remote HTTP(S) iframe embeds, and vault-local
  static apps. Static apps remain on their static-only origin, separate from the live API.
- A top-level external HTTP(S) URL is sent to the default browser only after a direct link gesture.
  Remote HTTP(S) subframes may render in WebKit. Redirects, scripted external navigation, `file:`,
  `javascript:`, `mailto:`, and arbitrary schemes are not handed to the OS. Script-created external
  popups fail closed when WebKit cannot establish a direct user gesture.
- macOS Edit actions (Undo/Cut/Copy/Paste/Select All), Quit, Back, Forward, and Reload are provided.
  Track cannot reliably inspect dirty state across the React editor, embedded HTML, and static
  apps, so it conservatively asks before top-level page navigation, Back/Forward, Reload, window
  close, and Quit. The dialog states this limitation rather than claiming exact dirty detection.
- Native audio bridging is not implemented in this stage. Browser/WebKit audio behavior is whatever
  the existing web UI provides; no native audio permission or bridge is added.

## SwiftUI retirement

The old `native/` SwiftPM package, its dedicated verification scripts, and the `native-app` /
`native-verify` Make targets have been removed. Build this shell with `make desktop-app` instead.
The [last pre-retirement source](https://github.com/ttak0422/track/tree/52b8380d125d688b7e468a2f7d537808144cc77c/native)
and its build instructions remain available in Git history. Older architecture and parity evidence
under `docs/` describe that retired implementation, not requirements verified by this shell.

This retirement does not establish full SwiftUI feature parity or migrate its preferences/windows.
In particular, native audio bridging remains outside this shell's current capabilities. The Go
engine, HTTP APIs, CLI/LSP, web frontend, Neovim integration, and Nix packages are retained.
No installed application, vault, configuration, cache, or runtime data is removed by this source change.
