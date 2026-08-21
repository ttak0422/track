# 0072. Reading milestones live in the sidecar

Status: Accepted

## Context

The workspace marks notes NEW until they are opened, and read once viewing time crosses half the
note's estimated reading time (floored at 20s). That state began life entirely in per-browser
localStorage: the accumulated seconds, and with them the NEW/read badges. The comment in
`web/src/reading.ts` called this per-browser state deliberate — reading also happens in Neovim,
which reports nothing, so the workspace only ever claimed "read here".

The consequence is that the flags do not travel. The vault syncs through cloud storage across
machines; localStorage does not. A report filed by an agent stays NEW on every device until each
browser opens it, a note read at the desk shows NEW again on the laptop, and there is no way for any
two devices to agree on what has been read.

## Decision

Reading milestones move into the note's sidecar as two monotonic firsts:

- `seen_at` — the first time any device opened the note in the web workspace;
- `read_at` — the first time viewing time crossed the read threshold there (implying seen).

The web workspace is their only writer, through `POST /api/note/read`. Milestones never move back:
whichever device reaches one first wins, and re-reporting is a changeless no-op that skips both the
sidecar write and the reindex — no etag is needed because the fields only ever advance. A sidecar
carrying either stamp is metadata version 9.

The index carries them as `notes.seen_at`/`notes.read_at` (unix seconds), and every search/listing
row rides them out over the wire. The frontend keeps its localStorage cache but demotes it: listing
and note responses are adopted into it monotonically, and the locally reached milestones are POSTed
once each. Components keep drawing badges from the same `isNew`/`isRead` they always read — server
truth now flows in underneath without any list learning about the wire.

The published bundle carries neither milestone. A public site's NEW/read badges describe the visitor,
not the author, so they stay per-visitor localStorage exactly as before.

## Consequences

- NEW and read agree across devices through the same OneDrive sync every other sidecar field rides,
  converging when listings refetch rather than requiring a reload contract.
- Opening a note now writes its sidecar once per browser lifetime per milestone instead of touching
  nothing; the monotonic rule keeps that to at most two writes per note.
- The accumulated viewing seconds stay local on purpose: they are the estimate that fires `read`,
  not something worth syncing, and merging them would manufacture precision no reader gets back.
- Vault-qualified ids (federated cross-vault results) stay local: the endpoint marks within one
  vault, and guessing which could stamp the wrong one.
- A note opened only in Neovim still reads as unseen — the terminal has no way to say otherwise,
  which is the honest answer rather than a wrong shared one.
