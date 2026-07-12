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
  scanned.
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
