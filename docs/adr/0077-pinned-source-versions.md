# 0077: Preserve source versions outside vault generations

## Decision

Freeze the exact UTF-8 body of an existing note with `track source save`. The existing
note ID identifies the working document; callers reuse it when fetching it again.
The note stays editable and searchable. Its preserved source versions live under
`.track/sources/<note-id>/<version>/`, separately from the working body and sidecar.

A record contains the original location, media type, SHA-256 body hash, explicitly
supplied retrieval/generation timestamp, and exact body. An explicitly supplied
original file is copied byte for byte into that same version directory and hashed.
Other linked assets and remote resources are not recursively captured. No PDF
extraction, remote fetching, model inference, or financial timestamp policy is added.

Source identities use the source location, media type, body hash and optional original
hash. Derived identities use pinned input references, method/model, exact settings
identifier, media type and an optional explicit regeneration key. Deduplication spans
all note IDs in the vault. Retrying preserves the saved owner, recorded time and output;
changing a recipe or regeneration key keeps a separate result. The returned record
contains the canonical note ID and version: use both for citations and derived inputs.
A duplicate working note is neither changed nor deleted and owns no new version.
Inputs remain exact references; track does not guess a different owner for an input.

A process file lock serializes source saves while scanning existing version directories
and publishing a new record. Write and verify a temporary directory before publishing
it with one rename. Readers ignore unfinished stages and verify hashes on every read.
Concurrent identical writes
converge on one version. A crash can leave an ignored temporary directory, but cannot
publish a record pointing at an original file that has not been written. Existing
corrupt versions are reported instead of being silently overwritten. Legacy duplicates
keep their existing paths and citation references; saves choose the smallest owner ID
among matching records after validating every match. A missing owner or missing/corrupt
record or original in any matching directory fails the save, even if another copy is
healthy. No automatic retargeting or repair occurs. A completely removed version
directory cannot be detected by this scan; old citations still fail explicitly.

## Why the existing features are insufficient

Properties and links describe provenance, but both properties and note bodies are
mutable. They neither deduplicate a processing recipe nor pin its evidence. `export`
renders the working note. Vault generations are bounded, reversible snapshots of
many notes and exclude attachments; their pruning and undo semantics do not identify
external document versions. The existing asset importer adds a suffix on collisions
and therefore does not identify immutable original files either.

The new directory is authoritative vault data, backed up alongside note bodies and
sidecars. SQLite schema and note metadata schema stay unchanged; no migration is
required. Reindexing, generation pruning and undo do not alter preserved versions.
There is no automatic expiry or garbage collection. Deleting or moving the owning
note makes ID-based evidence resolution fail explicitly; it does not silently resolve
a different document. Moving source records between vaults is outside this initial
contract; keep the source vault when retaining citations.

## Citation positions

`track cite` uses a stable note ID and an optional saved version. Missing, deleted,
corrupt or ambiguous references fail; a missing version never falls back to the
working text. Heading and block selection reuse the existing Markdown parser.
Ranges are 1-based inclusive lines. Physical pages require form-feed boundaries
already present in the saved text; printed page labels are never used as offsets.
No new wikilink syntax is introduced. Persist the returned note ID, version and
position in citation metadata; title links alone still refer to the working note.
