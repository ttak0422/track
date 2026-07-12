# 0037. Semantic related-notes via an external embedder command

Status: Accepted

## Context

Explicit `[[...]]` links (ADR 0008) connect notes a person thought to connect. They miss notes that
are *about the same thing* but were never linked — the value a semantic "related notes" surface adds.
Computing that surface needs an embedding model: text in, a float vector out, ranked by cosine
similarity.

A model does not belong in the engine. It is large, its licence and size are the user's choice, and
pinning one would bloat the CLI and force a network or GPU dependency on everyone. The vault already has
prior art for pushing heavy, swappable work to a separate process: the `track-fetch-*` tools are ordinary
executables the engine invokes, and their output files are the authoritative artifact.

## Decision

- **The embedder is a user-configured command, not engine code.** `embedder` in `config.yml` (or the
  `TRACK_EMBEDDER` env, which wins) names a command. The engine feeds a note's text on stdin and reads a
  JSON array of floats on stdout — the same stdin/stdout split as the fetch tools. The model, its size,
  and its licence are entirely the user's choice, and no note text leaves the machine unless their
  command sends it.
- **`embedder` accepts a scalar or a YAML sequence.** The scalar form is split on whitespace with no
  shell quoting, so no argument can contain a space; the sequence form is used verbatim as argv and is
  the way to pass space-containing arguments. `TRACK_EMBEDDER` stays a whitespace-split string (an env
  var cannot carry an array) and overrides the config value entirely.
- **Vectors are a cache in the index DB, keyed by note + content hash.** A new `embeddings` table stores
  one vector per note alongside the hash of the text it was computed from. The hash folds in the embedder
  command signature, so a note is embedded once and only re-embedded when its text or the embedder
  changes; swapping embedders re-embeds the whole vault rather than mixing vector dimensions. Like the
  rest of the index it is rebuildable — dropping it only forces a re-embed.
- **`track similar --id N [--limit K]` ranks by cosine similarity.** It self-heals the index, embeds any
  changed notes, then scans. Only notes (`kind == note`) are embedded; journals are date buckets whose
  content is whatever was touched that day, so ranking them as "related" is noise.
- **Brute-force scan is the deliberate ceiling.** Nearest-neighbour is an O(n·d) cosine loop over every
  cached vector. Fine to a few thousand notes; the upgrade path (an approximate-nearest-neighbour index
  such as an hnsw sidecar or sqlite-vec) is named in a code comment, not built now.
- **No embedder configured is a clean no-op, never a failure.** `track similar` returns a short message
  explaining how to configure one and exits 0. Every other command is untouched: the feature adds an
  inert table and one opt-in command, nothing on the hot path.

## Consequences

- The engine stays model-free and dependency-light; anyone can wire sentence-transformers, `llama.cpp`,
  or a hosted API behind the same three-line contract.
- The cache makes repeat lookups cheap, but `track similar` still re-parses each note to compute its
  content hash — acceptable for an opt-in command at the current scale, and independent of the scan
  ceiling above.
- Cosine on raw model output assumes the embedder emits comparable vectors; normalisation and model
  choice are the user's responsibility, matching the "heavy lifting outside the engine" split.
- An embedder switch is not cache-atomic — a mid-run failure leaves rows whose hashes no longer match —
  but ranking only runs after a fully successful Ensure, cosine scores any dimension mismatch 0, and
  the stale rows self-heal on the next successful run, so no transactional staging is needed for a
  rebuildable cache.
