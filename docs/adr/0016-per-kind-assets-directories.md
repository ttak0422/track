# ADR 0016: Per-Kind Assets Directories

## Status

Accepted

## Context

Notes increasingly reference local media — pasted images, downloaded diagrams,
and (planned) resources pulled in by a future "web fetch" that saves a remote
file into the vault. Without an agreed location these files scatter next to
notes or under ad-hoc folders, becoming hard to find, move, and reason about.

The links-and-attachments roadmap weighed two storage shapes: a single
vault-root `attachment/` tree, or a per-kind `assets/` directory. A vault-level
store centralizes files but divorces them from the note kind they belong to and
needs an extra path policy for references. Notes already live under `note/` and
journals under `journal/`, and the note scanner skips subdirectories, so a
sibling `assets/` directory inside each kind is naturally ignored by indexing.

## Decision

Store a kind's media under a reserved `assets/` subdirectory of that kind:
`note/assets/` and `journal/assets/`. A note references a stored file with the
relative path `assets/<file>`, which resolves the same whether the note is a
note or a journal entry (both sit one level above their `assets/` directory).

`config` is the single source of truth for the location
(`Config.AssetsDirForKind`, `AssetsDirName`). The storage primitive lives in the
engine as `internal/track/asset`, independent of the CLI so the editor, the web
UI, and a future web-fetch can all reuse it:

- `asset.Store(cfg, kind, name, data)` writes bytes under a sanitized,
  collision-free filename and returns the absolute path plus the `assets/<file>`
  reference.
- `asset.Import(cfg, kind, srcPath)` copies an existing local file.

Filenames are sanitized so the reference is valid inside a Markdown link: path
components are dropped and link-breaking characters (whitespace, brackets,
parentheses, `#`, `?`, …) become `-`; non-ASCII letters are kept. Collisions add
a numeric suffix rather than overwriting.

The CLI exposes `track asset import` and `track asset dir`. Assets are not
indexed notes: the note/journal scanners skip subdirectories, so files under
`assets/` are never treated as notes, and `track doctor` does not flag them.

## Consequences

There is now a stable home for attachments that a web-fetch feature can write
into without further design: it downloads a resource and calls `asset.Store`,
then embeds the returned `assets/<file>` reference.

Rendering an `assets/<file>` reference still depends on the surface. The Neovim
frontend opens it as a normal relative path. The web UI does not yet serve files
from `assets/` (the embed there resolves relative URLs against the page route),
so showing vault-local assets in the web preview is a deliberate follow-up that
needs its own asset-serving route and note-kind-aware reference resolution.

Assets are part of the authoritative vault, not the rebuildable cache, so they
follow the same backup/sync durability rules as note bodies and sidecars.
Moving a note between kinds would change the directory its `assets/` reference
resolves against; track does not move notes between kinds today, so this is not
yet a concern.
