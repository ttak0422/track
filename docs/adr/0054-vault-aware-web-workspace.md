# 0054. One web workspace serves a set of vaults

Status: Accepted

## Context

`track web` was built when a process had exactly one vault. `webui.Server` held
one `*config.Config` and one `*store.Store`, and every handler derived its file
paths from them. Requests named a note by a bare id: `?id=1783173182000`.

That is unsound once a machine has a vault registry (ADR 0051). Note ids are
vault-local — `note.FreeID` only looks inside one vault — and journal ids are
*guaranteed* to collide, because a journal's id is its date: `20260725` names
today's journal in every vault simultaneously. A bare id is therefore not an
identity, which is exactly why cross-vault search labels every hit with its
vault (ADR 0052) and cross-vault links are written `[[vault:title]]` (ADR 0053).

The web workspace had not made that move, and it is where the consequences are
worst, because it is the one surface that *writes*. A request carrying an id
from another vault would read, save, retitle, or delete the same-numbered note
in the launch vault instead. `DELETE` is the sharpest case: no etag, no server
confirmation, and the note file, its sidecar, and its index row all go. The
`PUT` etag check does not save you either — it compares content, not identity,
so two vaults' identical stub notes (an empty note, a freshly templated journal)
pass it and one overwrites the other.

The Neovim follow protocol had the same hole from the other direction: the
plugin posted a bare id, so with a buffer open in another vault the workspace
scrolled to whichever note in the launch vault shared that number.

## Decision

**One workspace serves a set of vaults, and every request that names a note
names its vault.**

- The server holds a `vaultView` per vault — registry name, config, index handle,
  and its own reindex throttle. The launch vault is the default; the others open
  lazily on first use.
- Every note-addressing endpoint is registered through `withVault`, which
  resolves `?vault=<registry name>` (absent = launch vault) *before* the handler
  runs. There is one resolution point, so no handler can forget.
- An unknown or unreachable vault fails the request. It never falls back to the
  launch vault — the rule ADR 0051 set for `--vault`, for the same reason: a typo
  must not land a write somewhere else.
- Responses label ids with the vault they belong to, and the launch vault has no
  label. An unlabelled id means "the vault you are already in" — the same
  asymmetry as `[[title]]` versus `[[vault:title]]` — so a workspace serving one
  vault answers exactly as it did before, registered or not. The federated
  search relabels its own launch-vault rows to keep that rule uniform across
  endpoints.
- `GET /api/search` spans every served vault by default (ADR 0052's federated
  connection) and reports vaults it could not read under `unavailable`.
  `?vault=` narrows it. Every other endpoint stays single-vault.
- Follow carries the editor's vault. The plugin knows its buffer by directory,
  so it sends `vault_path`; the server maps it to a served vault, refuses one it
  does not serve, and broadcasts the position labelled with the registry name.
- In the frontend an id from a named vault is the string `<vault>~<id>`. That
  keeps `NoteID` a single opaque string — routes, tabs, the query cache, and the
  graph's node map all keep working unchanged — while making collisions
  impossible. The launch vault keeps bare ids.
- A note's body also makes requests that name no note — its attachments, its
  `[[links]]`, the notes its includes and query blocks resolve, its viewspec data
  sources, and an uploaded cover. Those carry the vault of the body being
  rendered, provided as React context beside the note kind that was already
  threaded that way.
- A vault that cannot join a cross-vault read is reported, never silently
  dropped: an unreadable index, or one past SQLite's limit of ten attached
  databases, drops out of the federated query and is listed as a gap.

## Why the frontend composes rather than carrying a pair

Every frontend surface keys notes by one string: `/notes/$noteId`, the tab
strip's `NoteTab.id`, the react-query keys, `GraphCanvas`'s node map, the
floating-preview dedupe key. Threading a `(vault, id)` pair through all of them
would touch every one; qualifying the string touches the two functions that
compose and split it. The Go wire keeps the pair (`vault` + `note_id`), matching
what Phases 2 and 3 already emit — the composition happens once, where responses
are parsed.

The unqualified form meaning "the vault you are already in" is the same
asymmetry as `[[title]]` versus `[[vault:title]]`, and it keeps a single-vault
workspace's URLs and stored tabs byte-identical to before.

The separator is `~` rather than the `:` link syntax uses, because this id
travels through the URL. `:` is URI-reserved: a route param interpolates it to
`%3A`, and a router decodes a pathname with `decodeURI` (which keeps `%3A`) but
a param with `decodeURIComponent` (which restores `:`) — so the tab strip, which
reads the pathname, and the reader, which reads the param, would disagree on
every qualified id. `~` is unreserved and can appear in neither half: vault names
are `[a-z0-9-]` and ids are digits or a base62 slug.

## Consequences

- A workspace serving one unregistered vault behaves exactly as it did: no
  `vault` in responses, bare ids, unchanged URLs and restored tabs.
- Writes land in the vault the request names, so the whole class of
  wrong-vault writes is closed at the seam rather than per endpoint.
- Only the launch vault is watched by fsnotify; other vaults reconcile on read
  (`Server.refresh`), which is the same freshness path an external cloud sync
  already depends on. Sub-second liveness for a second vault would need its own
  watcher.
- The federated search attaches every reachable vault's index per request;
  SQLite's attach limit (10) bounds how large a registry this serves, the same
  bound ADR 0052 accepted.
- The static export is unaffected: it publishes one vault, so its ids stay bare.
- Cross-vault *graph* edges are not drawn. `ext_links` are (vault, title) string
  edges (ADR 0053) with no id on the far side, and the graph is a per-vault view;
  drawing them would need a federated graph query and an edge kind. Deferred
  until the need is real.
