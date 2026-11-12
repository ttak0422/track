# Native UI compatibility

The existing Web workspace is the UI/UX reference. Follow `docs/spec/design.md`;
the retired SwiftUI captures under `docs/evidence/native-parity` are not a current
acceptance baseline.

## Implemented shell direction

- Full-window sheet, restrained light/dark neutrals, vermilion as the only accent.
- Compact floating left icon rail, active-first tabs with overflow, no permanent note catalog.
- Search overlay preserves the underlying reader while querying; results remain
  separate from the current note and close on selection or dismissal.
- Reader plus context aside on wide windows; the aside stacks below on narrow ones.
- Actual metadata, title, source content, outline, hierarchy/backlinks and local graph
  use the server response, not fabricated placeholder data.
- A configured Home note is restored. Without one, activity Home remains unimplemented.
- Operational diagnostics are not primary chrome. Partial/unavailable data and
  read failures still produce visible warnings.

## Verification gates

Fresh current-Web reference captures use the public help vault, 1280×800 viewport,
light/dark themes, configured Home, note reader and search. They demonstrate the
reference only, not native equivalence. The native shell is compile/unit-tested;
the first independent native fixture confirms the new shell renders, while matched-size/theme comparisons are still pending. Use an isolated application instance and disposable fixtures for comparison.

Remaining gaps include tab persistence, all dock destinations/actions,
keyboard search-result navigation, Home activity, exact fonts, rich-format fidelity, persistent reading positions and
full accessibility/IME behavior. Missing actions must not look functional.

Compare both implementations at 900×650, 1280×800 and 1600×1000, both themes and
text sizes, with the same fixture. Include empty/error/partial/loading states,
search cancel and repeated navigation, narrow context stacking and graph pan/zoom.
Retain actual measured differences instead of declaring parity from compilation.

In-memory tab scroll positions, reader-focused tab shortcuts, and heading jumps are implemented. Native interaction verification remains pending. Markdown source soft breaks render as spaces; explicit hard breaks remain line breaks.
