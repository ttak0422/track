# 0039. Capture, refile, and archive: heading-anchor text surgery

Status: Accepted

## Context

A knowledge base needs primitives for moving text around without retyping it: dropping a quick
entry under a fixed heading (capture), relocating a subtree or list item to where it belongs
(refile), and setting a finished subtree aside without deleting it (archive). Org-mode ships all
three (`org-capture`, `org-refile`, `org-archive-subtree`); Obsidian leans on plugins for the same.

The vault already has the pieces these need: the `[[note##heading]]` link grammar with its heading
anchors (ADR 0008), section extraction that `![[...]]` includes use (ADR 0031), a template engine,
the append/reindex path behind `track append`, and `track rm`'s soft-delete-into-trash. The open
question was how to compose them safely rather than inventing a parallel editing stack.

## Decision

- **One anchor grammar, resolved strictly.** Targets are `Note#Heading` — the same string a
  `[[Note#Heading]]` link uses, with extra `#` pinning a level. `link.ResolveAnchor` refuses an
  anchor that matches more than one heading instead of guessing, because these commands mutate
  files. Navigation may adopt the first match; a move must not.
- **Section surgery lives in the link package.** `CutSection`, `CutListItem`, and `AppendUnder`
  (in `internal/track/link/section.go`) operate on note text with the same section bounds as
  `Extract`, so what a command moves is exactly what an `![[note##heading]]` embed would show. No
  command-layer text parsing.
- **Never lose text.** A cross-note move writes and re-reads the destination (`writeVerify`) to
  confirm the exact bytes landed *before* it strips the source. A failure after the destination is
  written duplicates rather than deletes, and is surfaced. A same-note refile re-resolves the
  destination heading against the post-cut body, since cutting shifts line numbers.
- **Capture composes the template engine.** The captured text is passed as the template's
  `{{ title }}`, so an existing one-line template frames it (e.g. `- [ ] {{ title }}`) with no new
  `{{ body }}` variable. The default target is the configured `capture_inbox`, created on first use.
- **Archive records provenance, and is distinct from trash.** The moved subtree lands in a
  configurable archive note (`archive_note`, per-year via `{{year}}` by default) with a
  `*Archived from [[Source]] on <date>.*` line inserted under its heading. The `[[Source]]` link
  keeps resolving and joins the archive note's backlinks. Archive is a living move; `track rm`
  remains whole-note soft deletion into `.track/trash`.

## Consequences

- The only genuinely new engine code is `section.go`; the CLI commands are thin compositions of it
  plus the existing resolve/template/index paths.
- Provenance is plain Markdown, so it renders everywhere and needs no schema.
- `refile --line N` is ordinal and can rot as the source note is edited between reading a line
  number and running the command; heading anchors remain the durable selector, and `--line` is for
  the immediate "move this item" case.
- Verifying a write by reading the whole file back is O(file size) per move; note files are small,
  so this buys "never lose text" cheaply. If very large notes appear, the check can narrow to the
  changed region.
