# Native reading shortcuts

The live React workspace enables reading shortcuts only when Track.app's main-frame document-start
script sets `window.__trackNativeReading` on the exact workspace origin. This is a read-only marker,
not a JavaScript-to-native bridge. The separate static-app origin, embedded documents, published
static build, and ordinary browser workspace do not gain this context.

## Boundaries

- `web/src/keybindings/bindings.ts` owns serializable defaults, action IDs, and exact modifier/repeat
  matching. `dispatchKeybinding` accepts a replacement binding list; a later configuration loader
  can validate data and supply it here without changing the action implementations.
- `readingContext.ts` owns interaction/overlay exclusions. `useReadingKeybindings.ts` supplies the
  native Preview-only context and action registry using the existing router and tab store.
- `tabCycle.ts` snapshots note-tab order during a keyboard walk. The existing MRU tab strip remains
  unchanged. Promoting a selected tab must not reduce a three-note walk to a two-note toggle.
- No configuration schema, persistence, settings UI, or native command dispatcher is added here.

## Focus and safety

The reading area's existing `.reader` scroller receives a tab stop only in native note Preview
mode. An unmodified primary click on ordinary preview text/margins focuses it without changing
scroll position; selecting text and clicking controls do not. Shortcuts require that exact element
to own focus, not an arbitrary descendant. Edit/Split mode, IME (including WebKit key code 229),
consumed events, modified keys, dialogs, search, and open menus keep their own behavior.

Escape releases the shortcut context while retaining the reading area's place in the focus order.
The next Tab can reach the note's controls/links; Shift+Tab can return to the tab strip and rail.
Refocusing or clicking the reading area enables shortcuts again. There is no global bare-Tab trap.
Native Find's AppKit field owns its keyboard input independently of the WebView.

Scrolling targets `.reader`: Preview-only notes flow into this outer scroller, unlike Split mode's
inner `.note-preview`. Three computed line heights per press are immediate and repeatable, without
queuing smooth animations. Tab repeat is consumed without navigating; with fewer than two note
tabs it is left untouched. View sentinel tabs are excluded, as is a home-note route without an
active tab. Opening a note uses `navigate(tabRoute(id))`, preserving the note editor's existing
unsaved-change blocker; no tab/draft state is mutated by a keyboard action.

## Verification

Colocated Vitest suites cover configurable dispatch, modifiers, IME, repeat, focus release,
selection/controls/editor/overlay exclusions, browser fallback, tab membership, and listener cleanup.
Real-router/store tests exercise three-note wrap through MRU reordering and Cancel/confirm on a
dirty note. Native QA should additionally use actual WKWebView key events, the native Find field,
Japanese IME, and the injected marker's workspace/static-app/frame boundaries.
