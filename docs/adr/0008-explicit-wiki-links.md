# ADR 0008: Explicit `[[...]]` Links Replace Implicit Auto-Links

## Status

Accepted (supersedes [ADR 0003](0003-implicit-longest-match-autolinks.md))

## Context

ADR 0003 made links implicit: any registered title or alias became a link wherever it appeared in note text. Two problems surfaced:

- **Performance.** Detecting links meant scanning the whole note body against the entire keyword dictionary (≈ one entry per note). The cost is roughly O(lines × line length × keywords), and it runs on every keystroke in the editor. It grows with the vault.
- **Accidental links.** A note titled `Go` turned every "Go" in every body into a link. CJK text has no word boundaries, so incidental substring matches are common and hard to avoid.

## Decision

Links are explicit, written `[[text]]`, and resolved against note titles and aliases by exact match.

- Extraction of `[[...]]` is O(line length); resolution is an O(1) dictionary lookup. Detection no longer depends on the number of notes.
- Inner text is single-line, may not contain brackets, and is trimmed of surrounding whitespace before resolution. `[[target|display]]` links to `target` while showing `display`, Obsidian-style.
- Fenced code blocks and self-links are still excluded.
- Unresolved `[[...]]` (no matching note) are not written to the graph and not returned as document links; the editor highlights them in a distinct group so they read as "note not created yet".
- The Go LSP gains `textDocument/completion`, triggered on `[`, offering titles and aliases inside an open `[[`. The Neovim Lua plugin consolidates onto the LSP as the sole link frontend; the pure-Lua substring matcher is removed.
- Existing notes are not auto-migrated: their bodies are not rewritten. Users add brackets where they want links.

## Consequences

Links are predictable and intentional, and detection is cheap regardless of vault size. CJK still works because resolution is exact-string, not word-boundary based.

The cost is that references must be authored with brackets instead of appearing for free; completion on `[` mitigates the typing overhead. Notes written under the old implicit scheme lose their automatic links until brackets are added.
