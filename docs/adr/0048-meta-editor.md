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

- **One canonical editable document.** A note's user-editable metadata — `tags`, `description`,
  `image`, `props` — is one YAML document (`note.MetaDoc`). The title is excluded: it is a link
  keyword owned by `track rename` (backlink-rewrite semantics). Non-editable sidecar fields
  (created, days, blocks) never appear in the document and carry over untouched.
- **One validated apply path.** `note.MetaDocYAML` renders the document (empty keys stay present so
  editors always show every field); `note.ApplyMetaDoc` parses it strictly (unknown top-level keys
  are rejected, not dropped), validates everything — tag normalization (`note.DedupTags`, the same
  rule as the CLI tag flags), the vault-asset/raster image check, prop keys and prop values typed
  against the configured `properties:` schema — and only then performs the single sidecar write.
  A rejected document changes nothing.
- **CLI is the transport.** `track meta` prints the document under `doc` alongside the existing
  JSON fields; `track meta --edit (FILE|-)` applies a document from a file or stdin. The field
  flags (`--description`, `--image`, `--set`, `--unset`) remain for scripted point edits.
- **Both frontends are thin shells.** The Neovim popup is a floating acwrite YAML buffer seeded
  with the document; `:w` pipes the buffer verbatim to `track meta --edit -` — no client-side
  parsing. The web meta dialog is the same document in a modal textarea, round-tripped through
  `/api/note/meta` (`{doc}` in, `{doc}` out), which calls `ApplyMetaDoc` directly. Validation
  errors surface as the engine's message and keep the editor open; the static export stays
  read-only.

## Consequences

- Adding a future editable metadata field means extending `MetaDoc` once; both frontends pick it
  up without UI-schema work.
- The web dialog's old `{description,image}` JSON shape is gone (pre-1.0 break); the endpoint and
  the dialog speak only the document.
- The document is whole-state: applying it replaces tags/description/image/props entirely, which
  is what an editor wants but means concurrent point edits between GET and apply are overwritten.
  Good enough for a single-user vault; an etag like the body editor's can join later if needed.
- ADR 0032's "the web view is read-only for properties" is superseded: props are now editable from
  both frontends, still through the same engine validation.
