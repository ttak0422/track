# 0046. Home dashboard widgets and note icons

Status: Accepted

## Context

The web workspace opens on a search screen and lists notes as bare titles. Two entry-point
improvements were wanted: a configurable landing note that can show a small dashboard (recent notes,
today's journal, pinned links), and a light visual identity where an emoji sits beside a note's title
in lists and navigation. Both must render the same in the live `track web` workspace and in the
`track export-site` static bundle, since they share one React frontend.

Prior art: Obsidian's Dataview/Home-tab plugins embed query-driven blocks in a note; Org-mode's agenda
and org-superstar's per-headline bullets. The vault already has fenced blocks that the engine resolves
per deployment (`viewspec` → `echarts` at export, live via `/api/viewspec`), a per-note sidecar
metadata store, and a start-page mechanism the static export uses to land on a root note.

## Decision

- **Dashboard widgets resolve to Markdown, not a bespoke component.** A `dashboard` fence carries a
  tiny YAML config (`recent: N`, `journal: true`, `pinned: [titles]`). The engine
  (`internal/track/dashboard`) rewrites it to ordinary Markdown — a heading and a bulleted list of
  `[[wiki links]]` per widget. The live server resolves it inside `/api/render`; the static export
  resolves it in `site.writeBundle`. Because the output is plain links, it reuses the existing
  Markdown, link-resolution, hover-preview, and navigation paths in both deployments — no new
  frontend renderer, API endpoint, or static data file. The engine stays the single source of truth
  for widget data.
- **The home note reuses the start-page mechanism.** `web.home` (a title or id) is resolved to a note
  id and injected as the same `__TRACK_START_PAGE__` the static export already uses; the live
  `HomeRoute` renders that note instead of the search hero. One code path lands both deployments on a
  note.
- **Icons are resolved on the engine, shipped as a field.** `config.NoteIcon(kind, tags, override)`
  resolves an icon with fixed precedence: a per-note sidecar override (`Metadata.Icon`, a new v5
  field, editable via `track meta --icon` and the same validated `MetaEdit` write path) beats the
  first matching tag mapping, which beats the kind mapping. The override is indexed as a `notes.icon`
  column so lists resolve it without per-note sidecar reads; the serving layer (webui / the static
  export) applies `NoteIcon` and emits a resolved `icon` string, so the frontend only displays it and
  never owns the mapping.

## Consequences

- A dashboard is a live, always-current list on the workspace and a build-time snapshot on the static
  site, with zero extra rendering machinery. The tradeoff is that widgets are limited to what Markdown
  links express (no interactive filtering) — acceptable for an entry point; a richer widget can later
  adopt the `viewspec` pattern of a resolved fence + component.
- Icons appear wherever a note list carries the resolved field (search results, the notes listing,
  the sidebar/search navigation, and vault-backed static sites). Surfaces built from the lighter
  `NoteRef` (backlinks, agenda) and the directory-mode help export do not carry icons yet, since they
  have no tags/override to resolve from; they can adopt the field when needed.
- The index schema gains one additive column (`notes.icon`), bumping the rebuildable cache to v3.
