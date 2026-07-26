# Web workspace

`track web` serves a local browser workspace for reading and navigating the vault. It is a thin
frontend over the [[CLI]]: it renders note bodies, resolves `[[...]]` links into navigable,
hover-previewable anchors, and draws the note graph.

## What it offers

- Rendered Markdown reading with GFM tables, task lists, and Mermaid diagrams.
- Hover previews and persistent floating windows for linked notes and media.
- A local link graph you can open per-note or full-screen.
- Follow mode, so the web view tracks the note you are editing in Neovim.
- A configurable [[Home dashboard]] landing note, with recent-notes, journal, and pinned widgets.
- A metadata editor (the Meta dialog in the note actions menu): dedicated fields for the note's
  title, tags, description, cover image, and icon, plus a free-form block for [[Properties]]. Built-in
  fields get typed controls — the cover image can be uploaded straight from the browser into the
  vault assets — while props stays free-form YAML. The engine composes and validates the whole edit
  (the same rules as `track meta --edit`); the frontend never assembles YAML. Changing the title
  renames the note and rewrites its backlinks. The published static site has no editor.

## Media embeds and diagrams

A Markdown image link on its own line becomes a rich block embed — YouTube players, Twitter/X posts,
PDFs, image URLs, Open Graph link cards, and text-file attachments (including Mermaid diagrams). Fenced
code blocks are syntax highlighted (with a copy button) and a `mermaid` fence renders inline. See
[[Embeds]] for the full list and syntax, and [[Charts]] for `.viewspec.json` chart embeds.

## Relationship to the static export

The static site produced by `track export-site` is the *published* counterpart of this workspace:
rendered content only, with no editor, search index, or heatmap top page. It reuses the same Markdown
and Mermaid rendering so a published note reads the way it does here, while [[Linking notes]] explains
how cross-note links are resolved against the published set.

## Searching from the workspace

The workspace search box matches note titles and `#tags` through the [[CLI]] as you type. Full-text
search across note *bodies* — ranked by relevance, with code-block and CJK matches — is a CLI
capability: run `track search --scope body`. See [[Searching notes]] for the query rules it shares.
