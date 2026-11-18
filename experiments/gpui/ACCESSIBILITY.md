# Native accessibility boundary

The pinned GPUI 0.2.2 implementation has no semantic accessibility-node API for
its custom `Div`, text, or input elements. The macOS `GPUIView` registers
`NSTextInputClient` methods for platform text input, but does not register
`NSAccessibility` roles, labels, values, children or actions. Its drawn element
tree therefore does not become an AppKit accessibility tree. Focus handles,
key contexts, tooltips and `EntityInputHandler` are not substitutes for that tree.

This matches the native QA observation: the OS exposes the window and AppKit
menus, not the note body or search field. The experiment now supplies a fixed
`Track Native Experimental` OS window title instead of an empty title. The
transparent titlebar and existing window-control policy stay unchanged. The
fixed title does not reveal the current note, query or vault path. This improves
window identification only; it does **not** make the reader/search accessible.

A complete fix requires a GPUI framework/platform bridge (or a deliberate
framework upgrade with equivalent supported APIs):

- Stable semantic node IDs and a role/name/value/state tree associated with the
  active layout, including hidden/removed-node invalidation.
- Correct screen bounds, focus and selection notifications, and safe action
  dispatch for buttons and editable text.
- UTF-16 text/range/selection semantics for the search/editor controls, coordinated
  with IME composition rather than exposed through an unrelated text proxy.
- VoiceOver and Accessibility Inspector validation for navigation, overlays,
  current-note changes, composition, selection and clean close.

Do not add decorative accessibility labels to the GPUI view and call this parity,
or expose a second invisible native text field whose focus/content can diverge
from the real input. No dependency upgrade or AppKit accessibility bridge is
included in the window-title change.

Source evidence in the prepared pinned dependency:
`src/elements/div.rs`, `src/element.rs`, `src/platform/mac/window.rs` and
`src/window.rs`. The title uses GPUI's supported `TitlebarOptions.title`, which
macOS forwards to `NSWindow.setTitle:`. Custom semantic element exposure is a
remaining framework limitation, not a completed native QA item.
