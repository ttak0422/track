# Web workspace

`track web` serves a local browser workspace for reading and navigating the vault. It is a thin
frontend over the [[CLI]]: it renders note bodies, resolves `[[...]]` links into navigable,
hover-previewable anchors, and draws the note graph.

Back to [[track]].

## What it offers

- Rendered Markdown reading with GFM tables, task lists, and Mermaid diagrams.
- Hover previews and persistent floating windows for linked notes and media.
- A local link graph you can open per-note or full-screen.
- Follow mode, so the web view tracks the note you are editing in Neovim.
- A configurable [[Home dashboard]] landing note, with recent-notes, journal, and pinned widgets.

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
