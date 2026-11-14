# Full-graph preview foundation (not a shipped UI)

`src/graph_preview.rs` is currently compiled for tests only. The full-vault Graph tab prerequisite now exists alongside the compact reader
aside. Neither surface integrates this preview model yet; the model produces no
production requests, editor changes, or focus changes.

The model implements a 260 ms hover intent and 220 ms pointer-transfer grace,
coalesced same-target intent, exact captured vault/note identity, stale request
rejection, one transient and **at most one explicit pin**. Capacity failure keeps
the existing pin. This one-pin limit is a native choice, not reference parity.
Successful empty bodies, loading, failure, partial/read-only diagnostics remain
distinct. Pin retains its target across navigation and graph scope changes;
owned-session invalidation removes everything. Unpin returns to transient closure.

## Integration interface and prerequisite

1. Use the full-graph tab and captured working-vault overview transport in
   `workspace_graph.rs`. Do not repurpose the local graph endpoint or `show_graph` aside.
2. Give the full-graph adapter its own `Previews` and monotonic clock origin.
   `set_scope` receives owned-server identity, graph generation, captured vault.
   Call `invalidate_session` on server failure/replacement. Call `navigate` only
   after a reader transition is accepted, never before a dirty-draft Cancel.
3. Feed hover-capable non-drag node transitions to `hover`; leaving nodes and
   preview bounds uses `leave_opener`, `enter_preview(&Read)`, and
   `leave_preview(&Read, now)`. Pass each card’s captured identity, not the current
   slot: pointer presence follows the card through pin/unpin, and old card events
   cannot hold or close a replacement card.
   Suppress intent during pan/zoom/drag and on non-hover pointer input.
4. Maintain one cancellable scheduled wake for `next_deadline`. Same-node moves
   do not restart it. At wake, `advance` returns at most one `Read`; dispatch only
   GET note for its captured vault/note and owned session. On completion call
   `complete` with that exact request. Also enforce storage epoch checks in the
   transport adapter. Canceled/closed slots cannot be resurrected.
5. Render transient and pin as independent bounded overlays using captured anchor
   clamped to viewport. Render the header title once, body with safe inert native
   Markdown, and diagnostics. Do not share main reader Find, code-copy, scroll,
   or focus state. No controls for unsupported drag/resize/collapse.
6. Explicit Pin must surface `CapacityReached` as a one-pin limit, not silently
   evict. Hover of an already pinned target deduplicates. Close only the slot.
   Open as page passes its exact target into the existing dirty-draft guarded
   navigation. Hover/pin must never mark seen/read or mutate notes.

Tests use synthetic identities/bodies and a fake read-command collector, not
network transport. GUI pointer/focus/placement, dirty-guard integration, HTTP
request auditing, and keyboard/AX behavior remain unverified and unimplemented.
