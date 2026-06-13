# ADR 0011: Markdown Export Pipeline

## Status

Accepted

## Context

Notes need to be written out into a portable form that tools other than track can read. The first target is Markdown.

A note body mixes plain Markdown with track-specific constructs: explicit `[[...]]` links with heading anchors and display aliases ([ADR 0008](0008-explicit-wiki-links.md), [ADR 0009](0009-heading-anchor-links.md)), Markdown action links that drive template-backed note creation (`[label](<note?...>)`, see `docs/spec/links.md`), Babel fenced code blocks with Org-style header arguments (`docs/spec/babel.md`), and legacy `<!--track ... -->` footmatter ([ADR 0006](0006-body-title-is-authoritative.md), [ADR 0002](0002-versioned-sidecar-metadata.md)). Emitted verbatim, these read as noise or as broken links outside track.

Emacs `org-export` (ox.el) is the reference. Its core shape is: parse to an element tree, run a backend whose transcoders turn each element into output, thread an options channel through the run, and allow backends to be derived. But `org-element` models every paragraph, list, and inline span. track's near-term goal is Markdown → Markdown, and the engine already has line-range-aware extractors (`link.Refs`, `link.Headings`, `babel.ParseBlocks`). A full AST would be overhead for re-emitting the same format.

## Decision

Adopt a lightweight transform pipeline with a `Renderer` interface, taking org-export's *ideas* (element-keyed transcoding, an options channel, a swappable backend) without its full AST.

- **Partial extraction, not a tree.** Scan the body for track-specific spans with the existing parsers and pass everything else through unchanged. Only constructs that need rewriting are modeled.
- **Five stages:** Load (body + sidecar metadata, split legacy footmatter) → Scan (extract spans) → Transform (replace each span via the renderer) → Assemble (optional frontmatter + body) → Emit (stdout or file).
- **`Renderer` interface** is the org backend analogue; the first implementation is Markdown. Output format stays swappable (e.g. a future HTML renderer) without touching the pipeline. `ExportOptions` is the communication channel.
- **Scope is a single note first.** Batch export (vault / tag / query) is future work and slots in behind the same renderer and options.

Element handling:

- **Wiki links → plain text.** `[[Go]]` → `Go`, `[[Go|ゴー]]` → `ゴー`, `[[note#heading]]` → `note`. Display text wins; the anchor is dropped. No dictionary resolution is needed, so export does not depend on the index.
- **Markdown action links → removed.** The template-backed `[label](<note?...>)` / `<journal?...>` cannot be evaluated outside track, so the label is flattened to plain text; a label-less action link is dropped.
- **Babel blocks follow `:exports`** (`code` default, plus `results`, `both`, `none`). track-specific header arguments (`:name`, `:results`, `:visible-lines`, `:session`, …) are stripped to a plain language-tagged fence. Results are pulled from sidecar v2 `last_run`. `:visible-lines` is an editor-only hint, so export emits the full body.
- **Legacy footmatter → removed.**
- **Metadata → off by default.** `--frontmatter` prepends a YAML block.
- **Headings pass through unchanged.** Body H1 headings are content; frontmatter title, when requested, comes from sidecar metadata (ADR 0013).

Engine code lives in `internal/track/export`; `track export` is a thin CLI command over it.

## Consequences

Reusing the existing parsers keeps the implementation small, and Markdown → Markdown needs no new index state at export time. This also fixes the semantics of `:exports`, which until now was parsed as metadata only (`docs/spec/babel.md`).

Flattening wiki links to plain text discards link structure; round-tripping back into track is lossy. Preserving links as relative Markdown links is a deliberate later option, enabled by knowing the export set — which is why batch export is the natural place to add it, behind a different renderer.

Because there is no full AST, a future backend that must reason about paragraph or list structure (e.g. rich HTML) cannot be expressed by a renderer alone; it would require extending the Scan stage. That cost is accepted in exchange for a simpler first implementation.
