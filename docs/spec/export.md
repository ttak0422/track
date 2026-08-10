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

ATX headings pass through unchanged. Body headings are content; the exported title, when requested as frontmatter, comes from sidecar metadata.

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
track export (--id <n> | --title <s> | --path <p>) [--out <file>] [--frontmatter] [--exports-default <mode>]
```

The target note is given by `--id`, by `--title` (resolved through the keyword dictionary like other commands), or by `--path`. The rendered Markdown is written to stdout. With `--out`, it is written to the file instead and the command prints `{"path": <file>}` as JSON, matching the other commands. Warnings (such as a missing stored result) go to stderr and do not change the exit code.

## Static site export

`track export-site` publishes a selected set of notes as a self-contained static site for GitHub Pages or any plain file server. The site is **the React web frontend in a static mode running against a pre-generated JSON bundle**, so it keeps track's real reading experience — sidebar, graph, hover previews, mermaid, media — without a server. The design is [ADR 0019](../adr/0019-static-site-export.md).

The input is a vault (`--frontend <dir>`, the static-mode frontend build, and `--out <dir>` are required):

```
track export-site (--all | --id <id> ...) [--root <id>] [--calendar] [--share]
                  [--base-url <url>] --frontend <dist> --out <dir>
```

`--all` publishes every note in the vault; `--id` selects instead. Journals are excluded from `--all`: they are day hubs indexing creates as a side effect, and the set of them records which days their author worked, so publishing them stays something a caller asks for by id. `--root` is the landing note's id and defaults to the vault config's `web.home` — the same landing note the workspace opens, so the front door travels with the content instead of sitting in a build config. A full reindex runs first so the published graph is complete.

`--base-url` supplies the absolute public site URL used by canonical and social metadata. `--share` is
opt-in: it adds X and copy-link actions below each static note, and requires `--base-url` so both actions
have an absolute published URL. It is off by default, which keeps the documentation site free of sharing
controls.

When `--base-url` is present, the export also writes `sitemap.xml` with every HTML page it publishes:
the root, selected note pages (using each note's resolved or pinned `slug:`), graph and empty pages,
used tag pages and their ancestors, and—when `--calendar` is set—the calendar and activity/task-date
day pages. Assets, encrypted bundle files, `/tasks/`, and disabled calendar routes are not listed. Note
URLs carry `lastmod` from the note body's indexed file mtime; generated view URLs omit it. The export
also writes `robots.txt` pointing at that absolute sitemap URL. Without `--base-url`, both files are
omitted because a sitemap cannot contain relative locators.

There used to be a second input mode: `--src <dir>` published a directory of plain Markdown outside any vault, with a `site.yml` standing in for the sidecars it did not have. It is gone (ADR 0059). A vault does everything it did — and a directory can become one: pin each page's current address in its sidecar (`slug:`, see [storage.md](storage.md)) so no published URL moves, since the slug is otherwise derived from the note id. This repository's own help site made exactly that move.

Two things the directory mode did that a vault does not. It resolved `[[links]]` by file base name as well as by title; a vault resolves by title, as everywhere else in track. And its link graph kept self-edges, which the index drops (`ReplaceLinks`) — so a page that links to itself does not appear in its own backlinks, matching the live workspace.

`--calendar` opts the published site into the calendar view and its per-day pages (see the web spec's
"Calendar view"): off suits reference sites like help docs, on suits activity-shaped ones like a blog
over a vault.

**OGP.** The prerender writes per-page `og:` tags into each page's head: `og:title` (the note title,
also the page `<title>`), `og:description` (the note's sidecar description — `track meta
--description` — falling back to a flattened excerpt of the body), and `og:type`/`og:site_name`.
`og:url` and `og:image` require an absolute origin, so they are emitted only when the export ran with
`--base-url <https://origin>` (carried to the prerender via `site.json`'s `base_url`). The image is
the note's cover (`track meta --image assets/<file>`, published under its content-hiding slug like
every asset); a note without one falls back to the site-wide default `ogp-default.png` shipped with
the frontend build.

**The data bundle is locked** ([ADR 0069](../adr/0069-the-published-data-bundle-is-locked.md)) **and
content-addressed** ([ADR 0070](../adr/0070-published-data-lives-at-a-content-addressed-path.md)). Every
file is gzipped, encrypted with AES-256-GCM, and published as `<out>/data/<generation>/<name>.bin`, where
the generation is a fingerprint of the data the bundle holds — the shapes below are what comes *out* of
it, not what a fetch returns. The generation is baked into every page as `window.__trackData`, so a page
reads the data of its own deploy: an update is visible immediately instead of behind the host's
ten-minute cache, and a page the CDN still serves from an earlier deploy never mixes with a later
bundle. A missing generation (that deploy is gone) tells the client its page is stale, and it reloads
once. The key is derived from the site's address
(`sha256("track-site-lock\0" + base URL + "\0" + root note slug)`) and baked into every page as
`window.__trackLock`, so the app opens its own data while a bulk consumer has to unlock deliberately.
Deriving it from the address rather than from content is what lets a page the CDN still serves from an
earlier deploy keep reading the current bundle. The
dehydrated cache the prerender inlines into each page is locked the same way. `Unlock` in
`internal/track/site/lock.go` and `web/src/lock.ts` are the two halves of that conversion. Because
`crypto.subtle` needs a secure context, a published site must be served over HTTPS or from localhost.

The exporter writes a JSON bundle (named by what each file holds; published as above) mirroring the server's `/api/*` shapes — `notes.json`, `note/<id>.json` (web-sanitized body + backlinks), `graph.json`, `resolve.json`, `site.json` — plus `search.json`, the published bodies (the same text `note/<id>.json` carries, in body-search order), which the site's full-text search scans in the browser ([ADR 0067](../adr/0067-published-search-runs-the-scan-path-in-the-browser.md)); it is fetched on the first search rather than at first paint. `hierarchy.json` is the other deferred file: the published `up` forest, prebuilt so the rail's hierarchy menu never walks the tree in the browser, and fetched when that menu is first opened. Then it copies the static frontend build and referenced `assets/<path>` media into `<out>`; each attachment publishes as `assets/<content slug><ext>`, so the source file name never appears and a replaced file lands at a new URL instead of behind the host's cache ([ADR 0070](../adr/0070-published-data-lives-at-a-content-addressed-path.md)). Wiki links to notes outside the published set are absent from `resolve.json`/`graph.json`, so the frontend leaves them inert. The live heatmap home is not published; the root note is the entry point. The `docs/help` vault is a working example — this repository publishes it with `make site`.

## Future

- **Batch export** of a whole vault, a tag, or a search result. With the export set known, wiki links can be rewritten as relative Markdown links (`[[a]]` → `[a](a.md)`) instead of being flattened — implemented as an alternative renderer rather than a change to the pipeline. The selection vocabulary already exists on the site exporter (`--all`, `--id`) and should be the same one here rather than a second spelling of it.
- **Additional renderers** (e.g. HTML). Formats that need paragraph or list structure would require extending the Scan stage, not just the renderer (see [ADR 0011](../adr/0011-markdown-export-pipeline.md)).
- **Per-note export exclusion** (an `org`-style `:noexport:` equivalent) so a note can opt out of batch export. This is no longer speculative: `export-site --all` already ships one exclusion, and it is hardcoded — journals, because indexing writes them without anyone asking and the set of them records which days their author worked. That is the right default and the wrong shape for a rule; a sidecar opt-out would state it once, per note, and let `--all` mean "every note that has not opted out" instead of "every note, except the kind we decided about in code".
- **Directory import.** Publishing a directory of plain Markdown was directory mode's job, and removing it (ADR 0059) removed that path with no command in its place: turning a directory into a vault today means writing `note/<id>.md` files and sidecars by hand, which is how `docs/help` was migrated. A `track import-dir` would assign ids, take each file's first H1 as the title, and pin `slug:` so an already-published directory keeps its URLs — the three things that migration actually did.
