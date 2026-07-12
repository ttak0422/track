# 0032. Typed note properties: sidecar props, inline fields, one flattening path

Status: Accepted

## Context

Notes need structured, queryable metadata beyond title and tags — a status, a rating, a due date,
an owner — so the vault can eventually answer "every draft note rated above 7". track already keeps
note metadata out of body prose: the versioned YAML sidecar (`.track/notes/<id>.yaml`, ADR 0002)
holds title, tags, description, and image, and the body stays plain Markdown.

Prior art: Obsidian stores properties as YAML frontmatter in the body file; Dataview adds inline
`key:: value` fields in prose; org-mode uses property drawers plus per-key completion.

## Decision

- **Extend the sidecar, not YAML frontmatter.** Properties are a `props` map in the sidecar
  (metadata version 5). Frontmatter would put a second metadata store *inside* the body file,
  contradicting ADR 0002's premise (bodies are pure Markdown; metadata is versioned and lives in
  the sidecar) and forcing every body consumer — render, export, LSP, transclusion line numbers —
  to learn to skip a header block. The sidecar already round-trips YAML, so typed scalars and
  lists come for free.
- **Inline `key:: value` fields for data that belongs in prose.** A whole line (list items
  included) or a bracketed `[key:: value]` mid-sentence is scanned at index time with its 1-based
  body line. The text keeps rendering as ordinary Markdown; fenced code and inline code are never
  scanned. *Belongs in prose* is the whole of it: a `weight:: 68.2` line in a journal is a line of
  the journal — indexed as a property **and** read as the sentence it is. An inline field is never a
  place to put note-level metadata (a title, a tag, an icon); that is the sidecar's job, and the body
  renders whole. See the correction below — we broke this rule once, and it cost a rendering
  regression.
- **A published directory's pages get a sidecar too.** `track export-site --src <dir>` publishes
  plain Markdown files that belong to no vault, and the split holds there: the page body stays at
  `<dir>/<name>.md`, its metadata goes to `<dir>/.track/<name>.yml`. It is the same decision as the
  vault's `.track/notes/<id>.yaml`, keyed by file base name only because a directory has no note ids
  (`BuildDir` assigns them at build time, in name order — the vault differs only in having other
  things under `.track/`). It carries the same top-level keys — `title`, `tags`, `description`,
  `image`, `icon`, `props` — is decoded strictly (`yaml.Decoder` + `KnownFields(true)`, this file's
  own idiom), and is optional per page: a page without one is a plain Markdown file. The vault's
  runtime-only fields (`version`, `created`, `days`, `blocks`) are not page metadata and are rejected
  as unknown keys. Every failure is loud, because each silent one publishes a page missing the
  metadata its author wrote: a sidecar naming no page, an unknown key, and one page spelled both
  `.yml` and `.yaml` are all build errors. A sidecar `title` beats the first-H1 convention (explicit
  over convention) and feeds the same link keys. Frontmatter is rejected here for the same reason it
  is rejected in a vault. The published *site's* config stays at `<dir>/site.yml` (ADR 0049):
  site-level and page-level are separated by location, so nothing collides.
- **Types are detected from value text, one rule set everywhere.** `true`/`false` → boolean, plain
  decimal → number, real `YYYY-MM-DD` → date, `[[...]]` → link (stored as its resolution key),
  top-level commas → list, else string. Sidecar values, inline fields, and CLI `--set` input all
  classify identically (`note.ValueType`).
- **One flattening path.** `note.CollectProps` (sidecar props sorted by key, then inline fields in
  body order) is the single source for the indexer's `props` table (note id, key, value, type,
  line), the web note API, the static-site bundle, and doctor — the surfaces can never disagree.
- **Schema is opt-in per key, enforced at the edges.** `config.yml` `properties:` declares
  `type` and/or enum `values` per key. CLI writes reject violations; files edited by hand surface
  them as `track doctor` `property_violation` diagnostics (not auto-fixable — the intended value
  is unknown); the LSP completes declared keys and enum values. Undeclared keys stay free-form.
- **The web view is read-only.** Properties are edited through `track meta --set/--unset` (the
  validated `ApplyMetaEdit` path) or by editing inline fields as text; the frontends only display.

## Consequences

- The index can filter/sort on `props(key, value, type)` for future query features; line
  provenance lets diagnostics and future tooling point at the exact inline field.
- Inline-field typing is per item, so a mixed list (`go, 2`) is legal; the schema checks each item
  against the declared type.
- A quoted YAML string that looks like a date or `[[link]]` is re-typed on read — deliberate, so
  the CLI and hand-edited sidecars behave the same.
- Renaming a note does not rewrite link-valued properties in sidecars yet; inline `[[...]]` values
  are body text and follow the existing rename rewriting. Sidecar rewrite can join `track rename`
  later if link-valued props see real use.

## Correction (the `icon::` field)

Directory mode briefly read a page's icon from an `icon::` **inline field** in the body, because it had
no sidecar to read one from. That was note-level metadata put back inside the body file — the exact
thing this ADR and ADR 0002 forbid, arrived at by the back door rather than by frontmatter. It then
forced a second wrong: to keep those lines out of the published prose, the web render blanked *every*
whole-line inline field, so a journal's own `weight:: 68.2` line rendered as a blank line in the live
workspace. One misplaced fact cost every user their prose.

The fix is the page sidecar above: metadata out of the body, and the body renders whole again. The rule
this ADR now states plainly — inline fields are for data that belongs in the prose, never for
note-level metadata — is what would have caught it.

Follow-up (not yet done): PR #15 (`feat/query`) adds `tags::` inline fields to a dozen `docs/help`
pages to drive directory-mode tag pages. Under this decision a `tags::` field is an ordinary prop, not
the page's tags — those come from the sidecar's `tags` key. Those fields must move into the page
sidecars when that branch lands.
