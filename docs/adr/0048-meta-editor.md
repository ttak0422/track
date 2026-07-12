# 0048. Metadata editor: one canonical YAML document, one validated apply path

Status: Accepted

## Context

A note's user-editable sidecar metadata grew piecewise: tags via `track new/append/update`,
description and image via `track meta` (ADR-era OGP dialog in the web UI, a two-line popup in
Neovim), and typed props via `track meta --set/--unset` (ADR 0032). Editing "the metadata of this
note" therefore meant three different surfaces with three different subsets, and the Neovim popup
even said tags must be changed elsewhere. Both frontends are supposed to be thin shells over the
engine.

## Decision

- **One canonical editable document.** A note's user-editable metadata — `title`, `tags`,
  `description`, `image`, `props` — is one YAML document (`note.MetaDoc`). Non-editable sidecar
  fields (created, days, blocks) never appear in the document and carry over untouched; the
  document is a projection of the sidecar for editing, not a second store.
- **A changed title is a rename, through the existing rename path.** The title is a link keyword,
  so applying a document whose title differs routes through `rename.Do` — the engine rename
  extracted from `track rename` (uniqueness against the index, backlink rewrite in referencing
  notes, rename history, full reindex) — never a bare sidecar write. An empty `title:` in the
  document means "leave the title unchanged", so older documents stay valid.
- **Apply order and atomicity.** Everything validates before anything writes: document syntax and
  unknown keys, sidecar-field rules, and the title-uniqueness pre-check. Then the sidecar fields
  are written (one write), then the rename runs (which rewrites backlinks and reindexes; the
  non-title path reindexes the one note instead). A validation failure therefore changes nothing;
  the only window for a partial apply — sidecar fields updated, title not — is an I/O failure
  between the two writes, which the pre-checks cannot cause.
- **One validated apply path.** `note.MetaDocYAML` renders the document — empty keys stay present
  so editors always show every field, as bare `key:` lines rather than flow-style `[]`/`{}`/`""`,
  which are hostile to hand-editing (the parser accepts both forms). `note.ApplyMetaDoc` parses it
  strictly (unknown top-level keys are rejected, not dropped) and hands the parsed document to
  `note.ApplyMetaDocValue`, which validates everything — tag normalization (`note.DedupTags`, the
  same rule as the CLI tag flags), the vault-asset/raster image check, prop keys and prop values
  typed against the configured `properties:` schema — and only then performs the single sidecar
  write. A rejected document changes nothing. The web dialog, which sends structured fields rather
  than a serialized document, composes a `MetaDoc` and calls `ApplyMetaDocValue` directly, so both
  transports share the one validated apply.
- **CLI is the transport.** `track meta` prints the document under `doc` alongside the existing
  JSON fields; `track meta --edit (FILE|-)` applies a document from a file or stdin. The field
  flags (`--description`, `--image`, `--set`, `--unset`) remain for scripted point edits.
- **Both frontends are thin shells.** The Neovim popup is a floating acwrite YAML buffer seeded
  with the document; `:w` pipes the buffer verbatim to `track meta --edit -` — no client-side
  parsing. The web meta dialog gives each built-in field (title, tags, description, cover image) a
  typed control and keeps props as the one free-form YAML block; it sends those fields as structured
  JSON to `/api/note/meta`, and the engine composes the document and runs the same
  pre-check/apply/rename sequence — the frontend never assembles YAML. A cover image can be uploaded
  from the browser: `POST /api/asset` (multipart) imports the file into the vault assets via the
  engine asset store, gates it with the same cover-image check (`note.ValidateImageRef`), and returns
  its `assets/<name>` reference for the image field. Validation errors surface as the engine's
  message and keep the editor open; the static export stays read-only.

## Consequences

- Adding a future editable metadata field means extending `MetaDoc` once; both frontends pick it
  up without UI-schema work.
- The web dialog's metadata contract is structured JSON — the typed built-in fields plus a free-form
  props block — not a serialized document; the engine composes and validates the `MetaDoc` from
  them, so all validation still lives in one place. The Neovim popup and CLI still speak the YAML
  document.
- The document is whole-state: applying it replaces tags/description/image/props entirely (the
  title only when non-empty and different), which is what an editor wants but means concurrent
  point edits between GET and apply are overwritten. Good enough for a single-user vault; an etag
  like the body editor's can join later if needed.
- `track rename`, the meta editors, and (still separately, for buffer-awareness) the LSP rename
  all end in the same backlink-rewrite semantics; `rename.Do` is now the shared engine home for
  the file-level rename.
- ADR 0032's "the web view is read-only for properties" is superseded: props are now editable from
  both frontends, still through the same engine validation.
