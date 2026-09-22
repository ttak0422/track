# ADR 0078: Tangle plans own output collisions

Status: Accepted

## Context

Literate source documents produce files for an external consumer, including a Nix build. A file's
content must be predictable without adding configuration precedence or depending on input-file order.
Implicit concatenation makes it hard to express a complete replacement later in a document.

## Decision

Build and validate a full tangle plan before writing any output. Within one source document, the last
block targeting a resolved output path supplies its entire content. Composition is explicit through
textual noweb references. This intentionally differs from Org's same-target concatenation.

Across different source documents, a shared resolved output path is an error regardless of input
order. Normalize paths and resolve existing symlinks before comparing them. Also reject a planned
file that would need to be a directory for another output. Return source locations and overridden
blocks in the plan so the effective output is reviewable with dry-run.

Tangling extracts source, never executes dependencies or reads evaluation results. No global,
language, or note-level precedence settings are needed for output collision handling.

## Consequences

Existing notes that concatenate several blocks into one file must explicitly compose those fragments
with noweb or put the complete content in one block. Same-document overrides are allowed even during
a multi-document invocation; only collisions between documents are errors.

Preflight validation prevents invalid plans from writing files. It does not provide a filesystem
transaction or protect against concurrent filesystem mutations; an I/O error can leave earlier
outputs written.
