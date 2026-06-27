# 0019. Static site export

Status: Accepted

## Context

`track export` writes a single note out as Markdown. The next requested capability is publishing a
chosen set of notes as a browsable, self-contained static site that can be mounted on GitHub Pages
(e.g. to ship usage/help docs alongside the repository). Requirements:

- Rendered HTML content only — no live index, heatmap top page, or preview/edit toggles.
- Links between selected notes are navigable; links to notes outside the selection are inert.
- Referenced assets are published.
- A machine-friendly CLI interface (an explicit set of note ids), since selection is driven by
  automation rather than interactive picking.

The roadmap originally framed batch export as a Markdown bundle first, but the concrete need here is a
ready-to-host HTML site, so we target HTML directly.

## Decision

Add `track export-site`, backed by the `internal/track/site` package. **The published site is the React
web frontend running in a static mode against a pre-generated JSON bundle** — not bespoke HTML. This
keeps track's real reading experience (sidebar, graph, hover previews, mermaid, media) in the output
instead of a second, lower-fidelity renderer.

> An earlier iteration generated plain HTML in Go via goldmark. It was replaced because it lost the
> features that make track worth reading; reusing the frontend is the only way to keep them without a
> parallel UI to maintain. The goldmark dependency was removed.

- **The frontend has a static mode** (build flag `VITE_TRACK_STATIC=1`). In static mode `web/src/api.ts`
  reads `./data/*.json` instead of the `/api/*` server, runs read-only (no editing, follow, live
  updates), uses hash routing (GitHub Pages has no SPA fallback), redirects the home route to the entry
  note, and is built with a relative base so it works under any Pages subpath. The live `track web`
  build is unchanged (flag off).
- **The exporter emits a JSON bundle mirroring the server's `/api/*` shapes** under `<out>/data`:
  `notes.json`, `note/<id>.json` (sanitized body + backlinks), `graph.json`, `resolve.json`, and
  `site.json` (the entry note). It then copies the static frontend build (passed as `--frontend <dir>`)
  and referenced assets into the output.
- **One bundle writer, two input front-ends.** `Build` publishes a vault selection (`--root <id>
  [--id ...]`) read through the index/store; `BuildDir` publishes a directory of plain Markdown files
  (`--src <dir> [--root <name>]`) for repo-mounted help/docs outside any vault, assigning ids in name
  order and resolving wiki links by file base name or first H1 title. Both reduce to the same
  `doc`/`edge` model.
- **Note bodies are sanitized with `export.NewWebRenderer`** — the same transform the live `/api/render`
  applies — so wiki links stay for the frontend to resolve and a published note reads identically.
- **Vault export does a full reindex first**, so the published link graph includes edges to notes that
  were created after their linker (which a partial index would miss).

## Consequences

- The static build is a second Vite build of the same app; CI builds it (`VITE_TRACK_STATIC=1`) and
  passes it to `track export-site --frontend`. The bundle is heavier than plain HTML (it ships the SPA),
  but fidelity matches the live app.
- The goldmark dependency was removed, reverting the Nix `vendorHash` to its pre-goldmark value.
- Links to notes outside the published set are not in `resolve.json`/`graph.json`, so the frontend
  leaves them unresolved (inert) — the site has no dangling navigation.
- OGP link cards degrade to bare cards (no network at view time); editing, follow, and the heatmap home
  are intentionally absent in the published site.
