# Full-graph previews: bounded native slice

The Full graph tab now supports mouse-hover read-only previews, separate from the
compact reader's local graph. `graph_preview.rs` owns the deterministic lifecycle;
`workspace_preview.rs` supplies a single coalesced timer, scoped GET transport and
independent overlay rendering. Parsed Markdown is cached per accepted card identity,
pruned with its active slot, and never reparsed on pointer movement. No note writes, seen/read marking, browser opening,
main reader Find/code-copy callbacks or focus calls are used by hover/pin.

- 260 ms hover intent, 220 ms pointer-transfer grace. Repeated same-node movement
  does not restart opening; repeated blank-canvas movement does not extend closing.
- One transient plus **at most four explicitly pinned cards**. Pin at capacity shows
  a limitation and retains all existing pins and the transient. This is a native limit, not parity.
- Pin/Unpin/Close and Open as page are wired. Card pointer events carry captured
  request identity and survive slot moves. Unpin replaces the disposable transient, never another pin. Unpin under the pointer remains open
  until leaving. Unsupported drag/resize/collapse controls are not shown.
- Requests retain captured vault/note, graph generation, owned launch, and storage
  generation. Only a matching live slot can receive a result. Graph pan/zoom/drag,
  reset and graph replacement cancel transient intent. Accepted navigation drops
  transient content, but keeps pins. Dirty Cancel does not run that cancellation.
- Pins retain exact targets across reader navigation, graph reload and working-vault
  changes. **Storage-epoch change, server failure or replacement clears all cards**
  conservatively, rather than retaining silently stale user data.
- Loading, error, empty body, partial/unavailable diagnostics and source read-only
  state are distinct. Preview header shows title once; authored body headings remain.
  The existing inert native Markdown subset is used; links, embedded content and
  actions are not activated. Render/include/diagram parity is not claimed.
- Cards use captured near-pointer placement clamped to the viewport, independent
  scrolling and existing light/dark colors. Pin-time placement slots add a modest
  cascade offset without moving other cards when closing or raising one. Clamping
  can overlap cards. A compact wrapping selector below the tabs stays above all
  cards: select a scoped title to raise that pin, or Close to remove only that pin.
  Raising does not refetch, move the card or steal keyboard focus. A new hover
  returns the transient to the front. While pins exist, a reserved band below
  tabs holds the height-bounded selector; cards start below it and shrink to fit,
  so the selector does not cover their header actions. No window-manager parity is claimed.

## Verification and remaining gaps

Synthetic model and fake read-boundary tests cover exact delays, coalescing,
late/closed request rejection, two vaults with identical IDs, four-pin capacity,
pointer identity, out-of-order completions, identity-based raising and closing,
under-pointer Unpin, navigation retention, session invalidation,
storage preflight/postflight rejection, content diagnostics and clamped geometry.
Existing dirty-guard tests remain in the full suite. Runtime visual, focus, pointer
transfer and dirty-dialog interaction have **not** been exercised by GUI automation.
Keyboard/AX and rich Markdown parity remain separate work. No personal vault,
system clipboard, browser or app-launch testing was used for this implementation.
