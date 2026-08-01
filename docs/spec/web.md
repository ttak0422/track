# Web Workspace Specification

`track web` serves a local-only HTTP workspace over the same SQLite indexes and
vaults as the CLI. It is for interactive exploration, not publication; public
output belongs to `track export` plus a static-site generator.

```sh
track web --addr 127.0.0.1:8765
```

The server is intended for a single local user. It performs no authentication
and binds to a loopback address by default.

## Vaults

One workspace serves the vault it was launched in — the **launch vault**, the
default target of every request — plus every vault in the machine config's
`vaults:` registry (ADR 0051). Note ids are vault-local and journal ids are the
date, so `20260725` names a note in every vault at once: an id alone cannot say
which note it means.

Every endpoint that addresses a note therefore takes `?vault=<registry name>`.
Omitting it means the launch vault; naming a vault that is not served is an
error, never a silent fallback to the launch vault.

Responses label ids with the vault they belong to, and **the launch vault has no
label**: an unlabelled id means "the vault you are already in", exactly as an
unqualified `[[title]]` does. A workspace serving one vault therefore answers
exactly as it did before it could serve several, whether or not that vault is
registered.

- `GET /api/search` searches **every** served vault by default and labels each
  hit with its vault: each vault answers the ordinary single-vault query and
  its pages merge in Go on the rank its own SQL assigned (ADR 0062), so no
  attach limit bounds how many vaults one search covers. `?vault=<name>`
  narrows it to one. A vault is either searched or reported under
  `unavailable` — `[{"name","path","error"}]` — so "no matches there" stays
  distinguishable from "could not read that vault".
- `GET /api/note` also carries `external`: the inbound `[[vault:title]]`
  references other vaults make to this note (ADR 0053), with `unavailable` for
  vaults that could not be consulted. Those edges live in the referring vaults'
  indexes, so they are a separate list from the same-vault `backlinks`.
- Every other note endpoint is single-vault: it acts on `?vault=` or the launch
  vault.

Only the launch vault is watched for filesystem changes; every other served
vault is reconciled on read, the same freshness path a cloud sync already
relies on. Change events carry no vault: a client refreshes everything it holds,
so there is nothing to narrow.

In the frontend an id from a named vault is written `<vault>~<id>` — in routes,
in the tab strip, and in the query cache — so two vaults' notes never collide.
The launch vault keeps bare ids, the same asymmetry as `[[title]]` versus
`[[vault:title]]`. The separator is `~`, not the `:` link syntax uses, because
the id travels through the URL: `:` is URI-reserved, so a route param would
interpolate to `%3A` and the pathname and the param would decode differently.

A vault has exactly one registry name — registering a second name for the same
directory is refused when the config loads (ADR 0051), so the name a hit is
labelled with, the name a qualified id carries, and the name a cross-vault link
is written with are always the same one.

## HTTP API

All `/api/*` responses are JSON. Read endpoints:

The calendar's day cells open that day's **journal** when one exists — the day is
what the journal is about — and the day page otherwise. Navigation never creates a
note, so a day with no journal (a purely planned future day, a vault with journals
off, a published site) still opens the day page and its notes-and-tasks listing.

- `GET /api/search?q=<query>&limit=<n>[&vault=<name>]`: search notes across every
  served vault; an empty `q` lists recent notes. `#tag` terms filter by sidecar tags.
  Results are the shared engine composition (ADR 0066): title matches first, then
  full-text matches for notes the titles did not already name, each tagged
  `match: "title" | "body"`. A body hit also carries the `line` it matched on and
  a `snippet` of it — though a match whose terms straddle lines has neither, which
  is why `match` and not the snippet is the discriminator. Across vaults the two
  phases merge separately — every served vault's title hits first, then the body
  hits — since a title rank and a bm25 score are not on one scale (ADR 0062).
- `GET /api/notes`: list indexed notes; each entry carries its activity `days` (the local days the note
  was created or updated), which the calendar view derives its per-day note lists from.
- `GET /api/activity?days=<n>`: return local-day update counts for the recent
  `n` days. The sidebar activity grid uses this instead of fetching every note.
- `GET /api/resolve?term=<title>[&vault=<name>]`: resolve a title to a note within
  one vault, matching how an unqualified `[[title]]` resolves.
- `GET /api/note?id=<id>[&vault=<name>]`: the note's body, tags, paths, backlinks, and an `etag`
  (a content hash of the file as read). It returns two paths: `path`, the canonical
  (symlink-resolved) location, and `copy_path`, the same note in the configured,
  symlink-intact form used for the copy-path button.
- `GET /api/graph/local?id=<id>[&vault=<name>]`: the one-hop local graph around a note.
- `GET /api/graph`: the whole-vault graph — every indexed note as a node and every
  link between two known notes as an edge, with no center.
- `GET /api/ogp?url=<url>`: Open Graph metadata for an embedded link, used to render
  link cards. Only `http(s)` URLs are accepted and the fetch is SSRF-guarded; see
  "Markdown embeds" below.

- `GET /api/tasks[?vault=<name>]`: every task in the vault carrying a scheduled or
  due date, for the calendar and day pages; `?open=1` asks for the open ones
  instead — any state that is not terminal, dated or not, worst first — for the
  tasks page; `?id=<id>` narrows it to one note's task set. The published site
  carries the dated listing as `data/tasks.json`.

Write endpoint:

- `PUT /api/note?id=<id>[&vault=<name>]`: save the body of an existing note. The request
  body is `{"body": "...", "etag": "<etag-from-GET>"}`.
- `DELETE /api/note?id=<id>[&vault=<name>]`: permanently delete a note — its Markdown file, its
  sidecar metadata, and its index row (tags and links cascade). Other notes keep
  their now-dangling `[[links]]`. The destructive title-retype confirmation is
  enforced in the web UI; the endpoint deletes by id.

## Frontend implementation

Visual styling follows the design master in [design.md](design.md): a fixed set
of control variants (text control, quiet chip, floating layer, filled action,
underline input, section label) over the shared color tokens. Consult it
before adding UI.

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
- a text-file **attachment** (`assets/<file>`) is fetched and rendered inline: a
  diagram source (`.mmd`/`.mermaid`, `.dot`/`.gv`, `.d2`, `.drawio`) renders with its
  diagram engine, any other text extension (`.txt`, `.json`, `.yaml`, `.csv`, shell
  scripts, …) as a code block. This is asset-only — a remote text URL is left to the
  OGP/link path — and degrades to a plain link while loading fails;
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
block. The same renderer backs an embedded `.mmd`/`.mermaid` attachment (see
"Markdown embeds"), so a diagram kept as a separate file renders identically to a
fenced block.

Graphviz (` ```dot `), D2 (` ```d2 `), and draw.io (` ```drawio `) follow the same
contract: lazy-loaded engine, error falls back to the source. draw.io is not a text
DSL — the block (or a `.drawio` attachment) holds the `<mxfile>` XML the editor
saves, compressed or not, and renders through drawio's vendored static viewer
(ADR 0065); the first page of a multi-page file is shown.

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

`POST /api/task` also takes optional `sched`/`due` date patches beside `state`
(a JSON `null`/absent field leaves the token alone, `""` clears it), so the task
table's date cells write through the same endpoint the state cell uses.

It carries the save's conflict model one level down too: the optional `expect`
field is the state the client is looking at, and a line that has since become
something else is refused with `409 Conflict`, leaving the file untouched. The
board and the rendered task rows both send it, since both already know the state
they drew. Omitting it writes unconditionally, as before.

## Copy path

The note view has a **Copy path** button that copies the note's absolute file path
to the clipboard. The copied path keeps the configured, symlink-intact form (e.g.
`~/track/note/100.md`) rather than the resolved target (`~/OneDrive/track/...`),
since that is the path the user recognizes and is usually shorter. This is the
`copy_path` field from `GET /api/note`.

## Graph view

A note page always shows the one-hop graph around the open note in its aside,
next to the backlinks (`GET /api/graph/local`); whenever the viewport holds
both, the aside sits beside the reading column as a sticky right rail, the
column giving up width before the rail does. Views without a
graph of their own (day, tags, search) keep the floating graph panel behind the
corner launcher, showing the whole vault (`GET /api/graph`). All graph surfaces
share the same force-directed layout, pan, and zoom.

For large graphs the rendering borrows Obsidian's approach rather than dropping
nodes:

- node size scales with link degree, so hubs stand out;
- labels are thinned by zoom — when zoomed out, only the center and high-degree
  hubs keep their labels, and the rest appear as you zoom in.

## Calendar view

`/calendar` shows a month calendar of note activity, reached from the sidebar rail like the full graph.
Each day cell lists the top notes active that day (as many as fit, freshest first, with a `+N` count for
the rest) and links to that day's `/day/YYYY-MM-DD` page; days without activity are inert. The month
title (`YYYY / MM`) links to the `yyyyMM` summary journal when that exists. Journals carry no activity
days by design, so cells list only real notes.

`/day/YYYY-MM-DD` is the page a day cell opens: the notes active that day — the same set the reader's
"On this day" aside shows — as a plain list of links. Its header offers the day's journal: the live
workspace opens (creating if needed) it like the activity heatmap does; the static site links it only
when the journal is published.

Both pages derive from the notes listing, which carries each note's activity `days` (`/api/notes` live,
`notes.json` in the static export), so neither needs an endpoint — or any journals — of its own. On the
static site `getAgenda` derives day lists from `notes.json`, which also makes the reader's "On this day"
work on published sites.

Every note-list surface shares one ordering: most recently updated first (id ascending on ties). The
notes listing, the calendar's cell titles, the day pages, the reader's "On this day" aside, and
backlinks all sort this way, live and published alike, so a cell's visible titles read identically on
the page they open.

A published site opts into the calendar with `track export-site --calendar`. The flag is carried as `calendar` in `site.json`: the
frontend shows the rail button and the prerender emits `calendar/index.html` plus a real
`day/<date>/index.html` for every active day. Without the flag — the default, suiting reference sites
like the repo help docs — the calendar and day pages are absent from the output.

## Tasks view

`/tasks` lists the vault's open work — every task whose state is not a terminal one, dated or not —
reached from the sidebar rail. The order is the engine's: `[#A]` before `[#B]` before unprioritized,
ties broken by deadline. A row marks what put it where it is (its priority, else `!` for a deadline
and `▷` for neither), names the note it lives in, and navigates there.

The listing is `GET /api/tasks?open=1`. Without the flag the same endpoint answers the calendar's
question instead — every task carrying a date, whatever its state — because a planned day should show
what was already finished on it. The two share a React Query prefix so a task write refreshes both.

The page reads and never writes. A task's identity is its line number in a note file, which is stable
only against that file, so changing a state belongs on the note page where the line is in view. The
published bundle carries the dated listing alone (`tasks.json`), so both the page and its rail button
are live-only.

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
