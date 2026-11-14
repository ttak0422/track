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
- A configured Home note is restored. Otherwise a scoped read-only activity
  heatmap is shown; journal opening and live refresh remain incomplete.
- Operational diagnostics are not primary chrome. Partial/unavailable data and
  read failures still produce visible warnings.

## Verification gates

Fresh current-Web reference captures use the public help vault, 1280×800 viewport,
light/dark themes, configured Home, note reader and search. They demonstrate the
reference only, not native equivalence. The native shell is compile/unit-tested;
the first independent native fixture confirms the new shell renders, while matched-size/theme comparisons are still pending. Use an isolated application instance and disposable fixtures for comparison.

Remaining gaps include tab persistence, all dock destinations/actions,
complete keyboard search-result navigation, activity journal actions, exact fonts,
rich-format fidelity, persistent reading positions and
full accessibility/IME behavior. Missing actions must not look functional.

Compare both implementations at 900×650, 1280×800 and 1600×1000, both themes and
text sizes, with the same fixture. Include empty/error/partial/loading states,
search cancel and repeated navigation, narrow context stacking and graph pan/zoom.
Retain actual measured differences instead of declaring parity from compilation.

In-memory tab scroll positions, reader-focused tab shortcuts, and heading jumps are implemented. A same-note heading jump and basic Find traversal were exercised; complete native
interaction verification remains pending. Markdown source soft breaks render as spaces; explicit hard breaks remain line breaks.

macOS native chrome now retains titled, resizable, closable and minimizable window
styles while hiding this window's standard controls through public AppKit APIs.
Launch bounds are 1200×800 with a 680×480 minimum. The bridge and Window menu are
compile-checked; runtime resize, fullscreen and titlebar behavior remain unverified.

## Matched visual and rendering evaluation

Use the same immutable public/synthetic vault snapshot and backend revision for
current Web and native. Record frontend commits, binary hash, OS/hardware, logical
content viewport, display scale, theme and text size. Compare content rectangles,
not whole-window screenshots with different browser chrome. Keep production data
and the user's existing windows out of the measurement session.

For each viewport above, capture Home, paragraph/table/code/link reader, Find,
search and a fixed graph snapshot in both implementations. Record missing
features and layout/hit-target differences before comparing speed. Keep fixtures
and graph positions identical; different force-layout convergence is not a
renderer benchmark. Disabled diagrams or omitted content cannot count as faster
rendering of the equivalent page.

Measure cold startup separately from warm reading and graph pan/zoom. After a
warm-up, repeat the same scripted interaction sequence at least five times,
reporting median and tail frame/input-to-visible-update times with the capture
method and raw samples. Separate CPU layout, scene preparation and actual frame
presentation when the tooling exposes them; report unavailable metrics rather
than substituting network completion or function-call timing. Alternate frontend
order and record background-load/thermal caveats. Screenshot appearance and the
existing CPU-only geometry benchmarks do not establish rendering performance.
Matched captures and presentation measurements are still pending.

### Display projection cost (non-GUI)

An opt-in synthetic debug-build benchmark measures allocation, title projection
and drop, excluding parsing/network/rendering. With 30 samples, 10,000 blocks
measured 3.646 ms median / 3.704 ms p95 with no substitutions, and 11.758 / 11.815
ms with every block substituted. This exposed avoidable repeated work, not an
observed frame-rate regression. Verified resolver results now retain an immutable
display snapshot, reused only when all source-provenance fields match; accepted
resolution rebuilds it once. The uncached fallback still clones raw content.
A separate Rc clone/drop-only loop averaged about 14.9 ns/op, but excludes
provenance checks, Find and drawing and is not a like-for-like speedup claim.
Reproduce with `cargo test --locked --manifest-path experiments/gpui/Cargo.toml
projection_cost_synthetic -- --ignored --nocapture`.

Clipboard transfer tests construct and inspect the actual GPUI ClipboardItem
used by the production copy path, including Unicode/whitespace and stale-action
rejection. They never access the system pasteboard. This validates the submitted
value, not OS delivery, paste behavior, permissions or accessibility.
