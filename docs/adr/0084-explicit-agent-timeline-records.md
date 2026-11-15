# Explicit agent timeline records

## Status

Proposed.

## Context

Markdown checkbox tasks have line-based identities and a transition sidecar. They cannot safely
represent long-lived agent task references after edits. A useful timeline also needs task hierarchy,
weighted estimates, authored reports, discussion and clear separation between requests and results.

## Decision

Add explicit stable-ID project records in a separate, versioned vault journal. A shared Go engine
validates and projects records for CLI, HTTP, Web and Neovim. Markdown task timelines remain a
separate projection; optional note links do not create implicit task synchronization.

Retain the ADR 0082 boundary: no built-in agent dispatch, account bridge, transcript collection or
model invocation. An external integration explicitly submits records through CLI/API. Cooperative
controls are requests and receipts only, not a claim that a process was stopped. Source labels are
provenance/routing, not authentication. No terminal TUI or new repository is required.

Writes use optimistic revisions, source/event idempotency and cross-process locking. The bounded
append-only journal is atomically replaced and replayed on reads. Corruption is refused rather than
reset. These authoritative files are backed up but excluded from static publication.

## Consequences

Web and Neovim can implement different presentations over one contract. The first implementation
uses full-record task updates and explicit authored reports. Automatic agent adapters, automatic
summaries, retention/rotation and remote authorization require separate designs. Users must not
submit private context they do not intend to persist. Progress is an estimate; receipts do not
independently verify external process state.
