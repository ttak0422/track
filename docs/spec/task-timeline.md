# Task timeline read model

The initial timeline is a per-note, read-only projection of existing Markdown checkboxes and
sidecar `task_log`. The Go task package supplies one versioned JSON shape for CLI, HTTP and
Neovim. It does not create a second authority or depend on SQLite for history retention.

- CLI: `track timeline (--id N | --title S | --path P)`.
- HTTP: `GET /api/timeline?id=N`, through ordinary vault middleware.
- Neovim: asynchronous `track.timeline.new(path)` adapter, with caller-owned display and refresh.
- Web: a read-only note panel, separate from the Lua renderer. No interactive terminal UI.

`items` preserve source order. `parent_line` is the nearest preceding lower-indent checkbox
within the list context; intervening unindented nonblank content resets that context. `depth`
is checkbox ancestry, not raw whitespace width. Non-checkbox list containers are not nodes.
`progress` counts explicit tasks, separating DONE and CANCELLED. It does not infer parent
completion or duplicate an estimate model. Empty collections are arrays, never null.

`history` preserves the persisted append order and original text/line values, including entries
whose task was removed. It is deliberately separate from current items: line numbers are not
stable IDs, timestamps may come from different clocks, and manual file edits are not an event
stream. No inferred joins or synthetic events are presented as durable evidence.

## Next boundaries

Stable task identities and their edit/migration rules must precede cross-note agent ingest,
idempotency keys, durable event cursors, or task-specific reports. Existing SSE is an
invalidation signal, not a replayable event log. New writers must use the existing task mutation
engine rather than rewriting task state through a second API. Automatic reports, estimates,
agent controls and conversation ingestion are not implemented by this slice.
