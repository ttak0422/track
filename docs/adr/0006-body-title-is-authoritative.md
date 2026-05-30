# ADR 0006: Treat the Note Body Title as Authoritative

## Status

Accepted

## Context

track stores metadata outside markdown files to keep notes clean.
That creates a consistency risk: a user can edit the note title in markdown without updating the sidecar metadata title.

If the index trusts stale metadata, auto-link keywords and search results can disagree with the actual note content.

## Decision

When a note body contains an H1 heading, the first H1 is the authoritative title.

During parsing and reindexing, track reconciles sidecar metadata:

- if `metadata.title` differs from the first H1, update `metadata.title` from the body;
- preserve aliases, tags, and created date because they are not currently derivable from the body;
- if sidecar metadata is missing but the body has an H1, create sidecar metadata using that title.

## Consequences

Editing the title in the markdown body is enough to rename the note's primary keyword on the next parse or reindex.

Sidecar metadata is still needed for aliases, tags, created date, and future fields that are not represented in the markdown body.
