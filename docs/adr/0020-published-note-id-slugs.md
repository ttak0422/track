# 0020. Opaque note id slugs in the published site

Status: Accepted

## Context

Note ids are timestamp-based: `note.NewID` returns `unixSeconds * 1000 + sequence`, and journals are
`yyyyMMdd` (ADR 0005). They are the source file base names (`note/<id>.md`).

The static site export (ADR 0019) used these ids verbatim as the published note's URL
(`#/notes/1781359469000`), its data file name (`data/note/1781359469000.json`), every `note_id` /
`source_id` / `target_id` / `root` field in the bundle, and the informational `path` / `copy_path`
fields. A published, public site therefore leaked each note's creation second (and each journal's date),
let visitors enumerate notes by guessing timestamps, and exposed source file names.

## Decision

**The published static site addresses notes by an opaque, stable slug instead of the internal id, and
the frontend treats note ids as opaque strings end to end.**

- **Slug = base62(UUIDv5(namespace, decimal id)).** `site.PublishID` (in `internal/track/site/slug.go`)
  hashes the decimal id under a fixed namespace and base62-encodes the 128 bits to a fixed 22-character,
  URL/filename-safe string.
  - **UUIDv5 (deterministic), not v4 (random):** the same note must map to the same slug on every
    rebuild so published URLs (bookmarks, external links, Pages history) stay valid.
  - The namespace constant **must never change** — altering it shifts every published URL.
  - Full 128 bits (no truncation), so collisions are negligible and no collision handling is needed.
- **The slug replaces the id everywhere in the bundle**: data file names, all id fields, `resolve.json`
  values, graph nodes/edges, and `site.json` root. It is applied uniformly, **including journals**, so
  no URL reveals a date.
- **`path` / `copy_path` are dropped from the bundle** (emitted empty). They held the timestamp-based
  source file name, which would re-expose what the slug hides; they were only informational.
- **Referenced assets are published under slug names too.** `assets/<rel>` would otherwise leak the
  source file name (and any directory structure) in the published HTML. `publishAssetName(rel)` =
  `publishSlug("asset:"+rel)` keeping the lowercased extension (the frontend detects media kind and the
  host sets the content type from it); the copied file is renamed and every reference in the note body
  is rewritten to match. The `"asset:"` prefix keeps the asset and note-id slug spaces disjoint.
- **The frontend's `NoteID` is a `string`.** It never does arithmetic on ids — only equality and URL
  building — so a string serves both modes. The live `track web` server still marshals numeric ids;
  `web/src/api.ts` stringifies every id field at the fetch boundary (`stringifyIDs`), so the rest of the
  app sees opaque string ids in both modes. The live server and its JSON are unchanged.

## Consequences

- Published URLs are stable but opaque (`#/notes/<22-char slug>`); the timestamp/date is no longer
  recoverable from a URL, file name, or bundle field.
- The vault's on-disk id scheme is unchanged: slugging happens only at export. (A broader vault-wide
  uuid5+base62 id migration remains future work; this is the export-only first step.)
- A published **journal** no longer exposes its date, so the static site cannot derive a journal's day
  from its id and omits the "On this day" agenda for journals. The live app (numeric `yyyyMMdd` ids) is
  unaffected.
- `internal/track/site/PublishID` is exported because the CLI/site tests resolve a note's published file
  by slug.
