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
  place to put note-level metadata (a title, a tag, an icon); that belongs outside the body, and the
  body renders whole. See the correction below — we broke this rule once, and it cost a rendering
  regression.
- **A published directory's pages keep the split too — in the site config.** `track export-site --src
  <dir>` publishes plain Markdown files that belong to no vault. The body/metadata split holds: the
  page body stays pure Markdown at `<dir>/<name>.md`, and what the page says about itself (its icon,
  its tags) goes outside it, into the directory's `site.yml` under `pages`, keyed by file base name
  (ADR 0049). *Where* outside the body differs from a vault on purpose. A vault note's sidecar is
  tool-created — `track new` and `track open` write it, `track meta` and `track rename` maintain it —
  so it costs a human nothing; a published directory has no such tool between the author and the
  files, so a per-page sidecar there would be hand-written boilerplate, one file per page and one
  rename per rename. The rule that must not bend is that the metadata is not in the body; the file it
  lands in is an engineering choice, and for a directory the config it already has wins. A page's
  *title* stays the first `# H1` (or the file name), as it always was. Frontmatter is rejected here
  for the same reason it is rejected in a vault.
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
nowhere else to read one from. That was note-level metadata put back inside the body file — the exact
thing this ADR and ADR 0002 forbid, arrived at by the back door rather than by frontmatter. It then
forced a second wrong: to keep those lines out of the published prose, the web render blanked *every*
whole-line inline field, so a journal's own `weight:: 68.2` line rendered as a blank line in the live
workspace. One misplaced fact cost every user their prose.

The fix is the site config's `pages` map above: metadata out of the body, and the body renders whole
again — a whole-line field is published as the line the author wrote, and indexed from that same line. The
rule this ADR now states plainly — inline fields are for data that belongs in the prose, never for
note-level metadata — is what would have caught it.

Follow-up (done with PR #15, `feat/query`): that branch first drove directory-mode tag pages with `tags::`
inline fields in a dozen `docs/help` pages. A page's tags are note-level metadata, so a `tags::` field in a
body is the same mistake `icon::` was, in the same place. The branch now records them in the site config
beside the icons — a `pages` entry grown from `cli: ⌨️` to `cli: {icon: ⌨️, tags: [help/reference]}` — and
that is also the change that brought an `icons.tags` map into the site config, which until then took no
such key precisely because no directory page had tags to match (ADR 0049).
