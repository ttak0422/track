# 0056. Drop semantic related-notes and its embedder command

Status: Accepted

Supersedes ADR 0037.

> Numbered 0056 rather than 0050 on purpose: 0050–0055 are taken by the multi-vault
> work in flight. This ADR is independent of it, so it takes a number past the
> reservation instead of forcing three open branches to renumber.

## Context

ADR 0037 added `track similar`: notes ranked by cosine similarity of embedding
vectors, to surface notes that are about the same thing but were never linked —
the gap explicit `[[...]]` links (ADR 0008) leave by design.

The model was deliberately kept out of the engine. `embedder` in `config.yml`
named a command; the engine fed a note's text on stdin and read a JSON array of
floats on stdout.

That design is sound and the feature went unused anyway, because it needs a local
embedding model. A user who reaches models through a subscription has no such
command to point at, and the feature has no fallback: with no embedder
configured, `track similar` only explains how to configure one.

What it cost while sitting idle:

- **An exec path.** `similar.CommandEmbedder` passed the configured argv to
  `exec.CommandContext` with the note's text on stdin. It was the only command
  named by a config *file* — Babel, the other exec path, is environment-only.
  Anything that could write a user's config could therefore choose a program to
  run, with note text on its stdin, the next time that user asked for related
  notes.
- **A config key with its own YAML grammar.** `embedder` accepted a scalar or a
  sequence, which needed a custom unmarshaller and its own error messages.
- **A schema table.** `embeddings`, plus the store methods reading and writing it.
- **A package**, its tests, and a CLI command.

## Decision

Remove the feature: the `similar` package and command, the `embedder` config key
and `TRACK_EMBEDDER`, the `embeddings` table and its store methods, and the
custom argv YAML grammar that existed only for this key.

The schema version bumps to 6. The index is rebuilt rather than migrated, so the
bump is what drops the table from an existing database.

## Consequences

- No config file names a command any more. The one exec path left is Babel,
  configured by `TRACK_BABEL_<LANG>` and run only by an explicit
  `track babel run`.
- That matters for the configuration split the multi-vault work introduces, where
  a vault carries its own config file and vaults are cloned and synced: the rule
  that a config file must not be able to name an executable now describes a
  property the code has, rather than a hazard it contains.
- Notes that are about the same thing but unlinked are once again only findable
  by searching for them. `track search` (title plus the FTS5 trigram body index,
  ADR 0045) is unaffected — it never used embeddings.
- Bringing the feature back means restoring an exec path, so it should come with
  a reason the previous one lacked: a way to use it without running a local
  model.
