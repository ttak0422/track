# ADR 0023: Single Top-Level Assets Directory

## Status

Accepted. Supersedes ADR 0016 (per-kind assets directories).

## Context

ADR 0016 stored media under a per-kind `assets/` subdirectory (`note/assets/`,
`journal/assets/`), so a note's `assets/<file>` reference resolved against its
own kind's directory. The web UI later served these files through
`/api/asset?kind=<kind>&name=<file>`, threading a `kind` value from the note
being rendered down through the markdown renderer purely to pick the directory.

In practice the split adds friction without paying for itself: users dropping a
file into the vault have to know which kind's `assets/` to use, the same file
referenced from a note and a journal has to be stored twice, and the `kind`
parameter propagates through both the CLI and the web frontend only to select
between two directories that hold the same sort of content. A single, obvious
place to put attachments is more intuitive.

## Decision

Store media for every note kind in one top-level `assets/` directory,
`<vault>/assets/`, a sibling of `note/`, `journal/`, and `data/`. A note
references a stored file with the same relative path `assets/<file>` as before.

- `config` remains the single source of truth: `Config.AssetsDir()` replaces
  `Config.AssetsDirForKind(kind)`; `AssetsDirName` is unchanged.
- `asset.Store(cfg, name, data)` and `asset.Import(cfg, srcPath)` drop the
  `kind` argument. Filename sanitization and collision handling are unchanged.
- `track asset import <file>` and `track asset dir [--ensure]` drop the
  `--kind` flag.
- The web UI serves attachments from `/api/asset?name=<file>`; a legacy `kind`
  query parameter, if present, is ignored, so already-rendered links keep
  working.

The note scanner walks only `note/` and `journal/`, so the sibling `assets/`
directory is never indexed or flagged by `track doctor` — the same reason `data/`
is safe as a top-level directory.

## Consequences

Attachments now live in one predictable location, and moving a note between
kinds no longer changes where its `assets/<file>` reference resolves — the
concern ADR 0016 left open is gone.

This is a breaking change to on-disk layout: media already stored under
`note/assets/` or `journal/assets/` must be moved into `<vault>/assets/` for its
references to resolve. Since the reference form (`assets/<file>`) is unchanged,
moving the files is sufficient; no note bodies need rewriting.

The web frontend still passes a `kind` value into its markdown renderer for the
`/api/asset` request. Because the server ignores it, that plumbing is now dead
weight and can be removed in a follow-up frontend cleanup; it was left in place
here to keep the change server-side and low-risk.
