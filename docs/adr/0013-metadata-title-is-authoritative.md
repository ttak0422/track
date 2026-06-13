# ADR 0013: Metadata Title Is Authoritative

## Status

Accepted

Supersedes ADR 0006.

## Context

track previously treated the first H1 in a note body as the authoritative title and reconciled sidecar metadata from that heading. That made hand-editing a body H1 rename the note, but it also made the markdown body carry two jobs: user content and track metadata.

AI agents often produce complete Markdown articles through stdin, including a leading `#` heading. The old model either rejected that input on creation or interpreted the heading as a title mutation on reindex. This made it hard to save generated drafts verbatim and made note identity depend on normal body edits.

## Decision

The sidecar `metadata.title` is the only source of a note's title.

- Note creation writes the title to `.track/notes/<id>.yaml`.
- The markdown body is content only. It may be empty, start with `#`, contain multiple H1 headings, or contain no headings.
- Title changes happen through create/open/journal/append metadata writes, `track rename`, or LSP rename.
- Parsing and reindexing never derive a title from the body H1 and never rewrite sidecar title from body text.
- H1-required template validation and H1-position diagnostics are removed.

## Consequences

Editing a body H1 no longer renames the note or changes the `[[title]]` keyword. Users and agents can save complete Markdown bodies verbatim.

The supported rename path is explicit: `track rename` or LSP rename updates the sidecar title, records `.track/renames.yaml` history, and rewrites backlinks. If a sidecar is missing, track does not reconstruct the title from the body; normal creation paths must create sidecars and backups should include `.track/notes/`.
