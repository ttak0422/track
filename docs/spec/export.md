# Export Specification

This document describes how track writes a note out to a portable format. The current target is Markdown. The design rationale is [ADR 0011](../adr/0011-markdown-export-pipeline.md).

Export is read-only with respect to the vault: it never rewrites the source note, its sidecar metadata, or the index.

## Scope

The current implementation exports a **single note** per invocation. Batch export (whole vault, a tag, or a search result) is future work; see [Future](#future).

Because only one note is exported, cross-note link targets are not known, so links are not rewritten into paths. They are flattened to plain text (see [Wiki links](#wiki-links)).

## Pipeline

Export runs in five stages:

1. **Load** — read the note body and its sidecar metadata. Split off legacy `<!--track ... -->` footmatter if present, keeping only the body.
2. **Scan** — extract track-specific spans from the body using the engine's existing parsers: `link.Refs` (wiki links), an action-link matcher (`[label](<...>)`), and `babel.ParseBlocks` (code blocks). Everything not matched is plain Markdown.
3. **Transform** — replace each scanned span with the renderer's output. Plain Markdown lines pass through unchanged.
4. **Assemble** — optionally prepend a metadata frontmatter block, then the transformed body.
5. **Emit** — write to stdout (default) or to a file (`--out`).

The output format is produced by a `Renderer`. The first and only renderer is Markdown; the interface exists so other formats can be added without changing the pipeline.

## Element Handling

### Headings

ATX headings pass through unchanged. The first h1 is the note title and is kept, since the body h1 is authoritative ([ADR 0006](../adr/0006-body-title-is-authoritative.md)).

### Wiki links

`[[...]]` links are flattened to plain text. No dictionary resolution happens, so export does not depend on the index.

| Source | Output |
| --- | --- |
| `[[Go]]` | `Go` |
| `[[Go\|ゴー]]` | `ゴー` |
| `[[note#heading]]` | `note` |
| `[[note##bar\|Label]]` | `Label` |

The display text wins when present; otherwise the note key is used. The heading anchor is dropped. Links inside fenced code blocks are not touched (the parser already skips fences).

### Markdown action links

Template-backed action links cannot be evaluated outside track, so they are removed:

| Source | Output |
| --- | --- |
| `[今日のjournal](<journal?offset=0>)` | `今日のjournal` |
| `[会議](<note?template=mtg&title=...>)` | `会議` |
| `<journal?offset=0>` (no label) | *(removed)* |

A labeled action link is flattened to its label; a bare angle-bracketed action with no label is dropped entirely.

### Babel code blocks

A language-tagged fenced block is emitted according to its `:exports` header argument. track-specific header arguments (`:name`, `:results`, `:visible-lines`, `:session`, and the rest) are stripped, leaving a plain language-tagged fence.

| `:exports` | Output |
| --- | --- |
| `code` (default) | source only |
| `results` | results only |
| `both` | source then results |
| `none` | nothing |

- Results come from sidecar v2 `last_run` for the block (see `docs/spec/babel.md`). The `:results` token set decides the shape: `output` emits captured stdout/stderr, `verbatim`/`scalar` emits the raw value.
- If `results` (or `both`) is requested but no stored result exists, the results portion is skipped and a warning is written to stderr; the source portion (for `both`) is still emitted.
- `:results silent` blocks have no stored result and therefore emit no results.
- `:visible-lines` is an editor-only display hint; export emits the full block body regardless.
- Plain fenced blocks (no language tag) are not Babel blocks and pass through unchanged.

### Legacy footmatter

A trailing `<!--track ... -->` block is removed during Load and never appears in output.

### Metadata

By default no metadata is emitted; the output is the body only. With `--frontmatter`, a YAML frontmatter block is prepended:

```markdown
---
title: ...
created: ...
tags: [...]
aliases: [...]
---
```

Only non-empty fields are written. Babel block metadata is never emitted as frontmatter.

## Options

| Option | Default | Effect |
| --- | --- | --- |
| `--frontmatter` | off | Prepend a YAML metadata block. |
| `--out <file>` | stdout | Write to a file instead of stdout. |
| `--exports-default <code\|results\|both\|none>` | `code` | Value used for Babel blocks that omit `:exports`. |

## CLI

```sh
track export <note-id-or-title> [--out <file>] [--frontmatter] [--exports-default <mode>]
```

The target is a note id or a title; titles resolve through the keyword dictionary like other commands. Output goes to stdout unless `--out` is given.

## Future

- **Batch export** of a whole vault, a tag, or a search result. With the export set known, wiki links can be rewritten as relative Markdown links (`[[a]]` → `[a](a.md)`) instead of being flattened — implemented as an alternative renderer rather than a change to the pipeline.
- **Additional renderers** (e.g. HTML). Formats that need paragraph or list structure would require extending the Scan stage, not just the renderer (see [ADR 0011](../adr/0011-markdown-export-pipeline.md)).
- **Per-note export exclusion** (an `org`-style `:noexport:` equivalent) so a note can opt out of batch export.
