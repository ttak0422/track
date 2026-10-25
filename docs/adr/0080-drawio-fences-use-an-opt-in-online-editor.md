# 0080: draw.io fences use an opt-in online editor

## Status

Accepted (2026-10-17)

## Context

The bundled static viewer renders draw.io files offline but cannot adjust connection points or
waypoints. The live workspace already owns a Markdown draft and an ETag-checked save operation.
Embedding a full offline editor would add a separate large distribution, resource and license
maintenance surface. General Markdown visual editing and attachment updates are separate work.

## Decision

Retain the vendored viewer (ADR 0065). Add a modal for complete, unindented backtick `drawio` fences
in the current live note. Before loading an iframe, disclose its external origin and require an
explicit **Open online editor** action. Use the official supported embed origin,
`https://embed.diagrams.net`, and its [JSON protocol](https://www.drawio.com/docs/reference/embed-mode/).
No new account, backend proxy, asset write endpoint or paid service is introduced.

Only the selected original XML is passed to that browsing context, with an exact target origin.
The iframe permits scripts and its own origin, with no popup, download, form or top navigation
capability; it receives no Track credentials or referrer. Incoming JSON is size-bounded and checked
against both the origin and the current iframe's Window. Each session mounts a fresh iframe.
Apply requests a full XML export with request correlation instead of trusting a delayed autosave.
The editor's own Save also applies XML to the draft. XML is validated without reserialization or
reconstruction from the displayed page, retaining compressed payloads, pages and layers.

The render handler annotates eligible source fences before expanding dashboard/query content.
Per-render opaque markers carry original line ranges and XML separately from rendered positions.
Only NoteEditor grants these capabilities; nested Markdown views and external Markdown do not.
The client binds each render to the exact request body and rechecks the whole draft, selected range,
fence delimiters and original XML before replacing only the selected interior. Markdown-breaking
fence lines, invalid XML and oversized XML are refused. No mutation uses a rendered line number.

Apply switches to Split and updates the existing draft. Track Save remains the only persistence
operation, with its original ETag, errors and conflict handling. A diagram session prevents clean-note
polling from replacing its source and is conservatively treated as unsaved until Apply or Cancel.
That state participates in browser unload, route/tab guards and native termination checks. Sessions
are invalidated on note switches; close/cancel confirm before discarding an opened online session.

## Consequences

- Viewing remains local and published bundles have no editor, external edit URL or write capability.
- Editing requires a network connection and entrusts the complete selected diagram to third-party
  code, including hidden pages and any embedded references. The UI and English help explain this.
- The hosted editor is maintained upstream and is not pinned with Track's viewer version. Protocol
  errors and timeouts preserve the draft. An offline editor distribution remains future work.
- Attachment editing, nested/indented fences, generated diagrams and Markdown WYSIWYG are excluded.
- Cross-origin focus, IME and dirty notifications are opaque, so any opened online session requires a
  discard confirmation. Visible Apply/Cancel controls are the reliable route when iframe shortcuts
  are consumed. Existing native navigation policy and workspace API origin guards are unchanged.
