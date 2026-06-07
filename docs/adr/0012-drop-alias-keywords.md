# ADR 0012: Drop Alias Keywords

## Status

Accepted

## Context

A note could carry alias keywords in its sidecar metadata (`aliases:`), in addition to its title. Each alias resolved a `[[alias]]` link to the note exactly like the title, through the keyword dictionary (the store `aliases` table plus the `keywords` view).

This is distinct from a **display alias** — the `[[target|shown text]]` form, where the text after `|` only changes how the link renders ([ADR 0008](0008-explicit-wiki-links.md)). Display aliases are not affected by this decision.

Alias keywords carried cost across the stack: a sidecar field, a SQLite table and a UNION branch in the `keywords` view, index ingestion, LSP completion and resolution, and CLI search. They also introduced ambiguity — the same term could be one note's title and another's alias, resolved first-match-wins — the very kind of duplicate the diagnostics discussion would have had to flag. In the obsidian.nvim-based workflow they are unused.

## Decision

Remove alias keywords. Link resolution keys on note titles only.

- The sidecar `Metadata.aliases` field is removed. Existing sidecars that still contain `aliases:` are read without error (an unknown YAML field is ignored), so the metadata schema version is unchanged.
- The store drops the `aliases` table and the alias branch of the `keywords` view. The SQLite `schemaVersion` is unchanged; existing caches are rebuilt with `track reindex --full`.
- Display aliases (`[[target|shown]]`) stay. Only the alias *keyword* is gone.

## Consequences

The keyword dictionary becomes one-to-one with titles, which removes the title/alias ambiguity and simplifies the store, metadata, index, LSP, and CLI.

Existing `[[alias]]` links stop resolving and show as unresolved. Unlike a title rename, an alias is not recorded in the rename history, so there is no automatic repair suggestion for it. Name variants are now expressed with a display alias (`[[Title|variant]]`) or by standardizing on the title. This is a breaking change, accepted because track is pre-release and aliases were unused in practice.
