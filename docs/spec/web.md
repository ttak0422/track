# Web Workspace Specification

`track web` serves a local-only HTTP workspace over the same SQLite index and
vault as the CLI. It is for interactive exploration, not publication; public
output belongs to `track export` plus a static-site generator.

```sh
track web --addr 127.0.0.1:8765
```

The server is intended for a single local user. It performs no authentication
and binds to a loopback address by default.

## HTTP API

All `/api/*` responses are JSON. Read endpoints:

- `GET /api/search?q=<query>&limit=<n>`: search notes; an empty `q` lists recent
  notes. `#tag` terms filter by sidecar tags.
- `GET /api/notes`: list indexed notes.
- `GET /api/activity?days=<n>`: return local-day update counts for the recent
  `n` days. The sidebar activity grid uses this instead of fetching every note.
- `GET /api/resolve?term=<title>`: resolve a title to a note.
- `GET /api/note?id=<id>`: the note's body, tags, paths, backlinks, and an `etag`
  (a content hash of the file as read). It returns two paths: `path`, the canonical
  (symlink-resolved) location, and `copy_path`, the same note in the configured,
  symlink-intact form used for the copy-path button.
- `GET /api/graph/local?id=<id>`: the one-hop local graph around a note.
- `GET /api/graph`: the whole-vault graph — every indexed note as a node and every
  link between two known notes as an edge, with no center.
- `GET /api/ogp?url=<url>`: Open Graph metadata for an embedded link, used to render
  link cards. Only `http(s)` URLs are accepted and the fetch is SSRF-guarded; see
  "Markdown embeds" below.

Write endpoint:

- `PUT /api/note?id=<id>`: save the body of an existing note. The request body is
  `{"body": "...", "etag": "<etag-from-GET>"}`.
- `DELETE /api/note?id=<id>`: permanently delete a note — its Markdown file, its
  sidecar metadata, and its index row (tags and links cascade). Other notes keep
  their now-dangling `[[links]]`. The destructive title-retype confirmation is
  enforced in the web UI; the endpoint deletes by id.

## Frontend implementation

The current production UI is still served by the Go `internal/track/webui`
package. The React migration lives under `web/` and is built with Vite,
TypeScript, TanStack Query, and TanStack Router while it is brought up to parity.

During migration:

- keep the existing `/api/*` contract stable;
- run `npm install` and `npm run build` from `web/` for frontend changes;
- run `track web --addr 127.0.0.1:8765` and `npm run dev` from `web/` to use the
  Vite dev server against the local Go API;
- only switch Go's served assets to the Vite build once the React workspace has
  reached feature parity with the existing raw-string UI.

### Markdown embeds

A line that is exactly a Markdown image link (`![alt](url)`) renders as a block
embed instead of a link:

- YouTube URLs (`youtu.be/<id>`, `youtube.com/watch?v=<id>`, `/shorts/<id>`,
  `/live/<id>`, `/embed/<id>`) become a privacy-enhanced `youtube-nocookie.com`
  iframe player,
  carrying a `t=`/`start=` timestamp (plain seconds or the `1h2m3s` form) as the
  player's `start`;
- Twitter/X status URLs (`twitter.com`/`x.com/<user>/status/<id>`) embed the actual
  post through Twitter's official `widgets.js`, loaded once on demand; if the widget
  cannot render the tweet (deleted, blocked, offline) it falls back to the OGP card;
- `.pdf` URLs become an inline iframe viewer with an "open" link fallback;
- image URLs (`.png`, `.jpg`, `.gif`, `.webp`, `.avif`, `.svg`, …) render as an `<img>`;
- any other `http(s)` URL renders as an Open Graph card.

Only `http(s)` and same-origin relative URLs feed an iframe, so a note cannot
smuggle a `javascript:`/`data:` document into the frame. A plain `[label](url)`
stays a link — embedding is opt-in via `![…]()` so ordinary links are never turned
into cards — and inline `![…](…)` inside a paragraph is left untouched so block
embeds never nest inside a `<p>`.

The Open Graph card is fetched server-side via `GET /api/ogp?url=<url>`, which
returns `{url, title?, description?, image?, site_name?}`. The fetch is guarded:
only `http(s)` URLs are accepted, the dialer refuses loopback/private/link-local
addresses (SSRF), redirects and body size are capped, and results are cached
(positive and negative) so repeated renders do not refetch. The client renders
the card as a link and falls back to a plain link when the fetch fails or the
page exposes no metadata.

### Mermaid diagrams

Fenced code blocks tagged `mermaid` render as Mermaid diagrams in the web
preview. The frontend initializes Mermaid with `securityLevel: "strict"` and the
current track theme colors. If a diagram fails to parse or render, the preview
shows the error and falls back to the original fenced source as a normal code
block.

### Save conflict detection

`PUT /api/note` is guarded by an optimistic-concurrency `etag`, the content hash
the matching `GET` returned. On save the server recomputes the hash of the file
on disk:

- match: the body is written, the single note is reindexed, and a new `etag` is
  returned;
- mismatch: the save is refused with `409 Conflict` and the file is left
  untouched, so a copy written underneath (e.g. by a cloud sync) since the client
  loaded the note is never clobbered. The client should reload before retrying.

A missing `etag` is a `400`. Titles stay sidecar-authoritative (ADR 0013), so a
save only writes the markdown body, never the title.

## Copy path

The note view has a **Copy path** button that copies the note's absolute file path
to the clipboard. The copied path keeps the configured, symlink-intact form (e.g.
`~/track/note/100.md`) rather than the resolved target (`~/OneDrive/track/...`),
since that is the path the user recognizes and is usually shorter. This is the
`copy_path` field from `GET /api/note`.

## Graph view

The graph panel has a **Local / Global** toggle. Local shows the one-hop graph
around the open note; Global shows the whole vault (`GET /api/graph`). Both share
the same force-directed layout, pan, and zoom.

For large graphs the rendering borrows Obsidian's approach rather than dropping
nodes:

- node size scales with link degree, so hubs stand out;
- labels are thinned by zoom — when zoomed out, only the center and high-degree
  hubs keep their labels, and the rest appear as you zoom in.

## Theme and colors

The workspace theme and colors are configured under `web:` in `config.yml`:

```yaml
web:
  theme: dark
  colors_path: ~/.config/track/colors.yml
```

`web.theme` (`system` / `light` / `dark`, default `system`; unknown values fall
back to `system`) sets the boot default theme. A user's in-browser theme choice
is stored client-side and overrides this default.

`web.colors_path` points at an optional palette file that overrides the built-in
colors. It has optional `light:` and `dark:` sections, each mapping a themeable
variable to a CSS color:

```yaml
light:
  accent: "#2f6f5e"
  text: "#20231f"
dark:
  accent: "#62b39b"
```

Themeable variables: `bg`, `panel`, `panel-soft`, `text`, `muted`, `line`,
`accent`, `accent-strong`, `graph-active`, `graph-active-strong`, `generated`,
`danger`. Unknown keys are ignored and values are validated against safe color
syntax, so a palette can never inject arbitrary CSS. A malformed value is an
error; a missing or unreadable palette file is logged and the server falls back
to the built-in palette rather than failing to start. The overrides follow the
same light/dark/system cascade as the default stylesheet.
