# 0057. Drop the rename history file

Status: Accepted

## Context

A rename — `track rename`, LSP `textDocument/rename`, or a title change through
`track meta --edit` — goes through one engine path: check the new title is
unique, rewrite every `[[old title]]` in the vault, write the sidecar title,
append to `.track/renames.yaml`, reindex.

The append is the odd one out. The rewrite already fixed every link the vault
holds, so the history exists only for links the rewrite could not reach: one
written by hand after the fact, or one in another vault. Exactly one thing read
it — an LSP code action offering to rewrite an unresolved `[[old title]]` to the
note's current title. Everything else about the file was writing it, snapshotting
it, or documenting it.

The cost is out of proportion to that. `renames.yaml` is an **append-only record
of every title the vault has ever used**, kept forever, inside the vault. A vault
checked into a repository or published therefore ships the titles its author
renamed away from — which is often exactly why they renamed. It is the same class
of leak as the journal tree recording which days someone worked and `.track/gen/`
keeping every past state (ADR 0055), except those are useful enough to earn a
switch. A quickfix suggestion is not.

It also has to be carried: generations snapshot it as a special-cased single file
alongside the directories they copy.

## Decision

Remove the rename history: the `.track/renames.yaml` file, the `rename` package's
history type and its `Append`/`LatestReachable`, `Config.RenamesPath`, the
generation snapshot's single-file special case, and the LSP code action that read
it.

Renames keep doing the part that matters — the in-vault backlink rewrite — which
was always where the guarantee lived.

## Consequences

- A link a rename could not reach surfaces as an ordinary unresolved-link
  diagnostic. The suggestion that used to name the new title is gone; the
  diagnostic still points at the link, and `track search` finds the note.
- Nothing records a past title, so an old title is free for a new note the moment
  it is released — which was already true (the history was never a keyword
  source), just now with nothing to contradict it.
- A vault checked into a repository no longer carries the titles its author
  renamed away from.
- Generations snapshot only directories (`note/`, `journal/`, `.track/notes/`);
  the single-file copy path is gone.
- Cross-vault `[[vault:title]]` references were never repaired by the history
  (it is vault-local, and ADR 0053 accepts that a rename breaks inbound qualified
  references). That is unchanged.
